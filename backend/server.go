// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"decred.org/dcrros/backend/backenddb"
	"decred.org/dcrros/backend/internal/badgerdb"
	"decred.org/dcrros/backend/internal/memdb"
	"decred.org/dcrros/types"
	"github.com/coinbase/rosetta-sdk-go/asserter"
	rserver "github.com/coinbase/rosetta-sdk-go/server"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/lru"
	"github.com/decred/dcrd/rpcclient/v6"
	"github.com/decred/dcrd/wire"
)

const (
	// rosettaVersion is the version of the rosetta spec this backend
	// currently implements.
	rosettaVersion = "1.4.8"
)

// DBType defines the available database types.
type DBType string

const (
	// DBTypeMem holds all data in memory structures. This has the best
	// performance but requires reprocessing the chain every time the
	// server runs.
	DBTypeMem DBType = "mem"

	// DBTypeBadger holds all data in a Badger DB instance. This offers a
	// good compromise between not having to reprocess the chain at every
	// startup and performance.
	DBTypeBadger DBType = "badger"

	// DBTypeBadgerMem holds all data in a memory-only Badger DB instance.
	DBTypeBadgerMem DBType = "badgermem"

	// dbTypePreconfigured is only used in tests. It indicates the server
	// should pickup the db instance from the config object.
	dbTypePreconfigured DBType = "preconfigured"
)

// SupportedDBTypes returns all publicly-available DB drivers for the server.
func SupportedDBTypes() []DBType {
	return []DBType{DBTypeMem, DBTypeBadger, DBTypeBadgerMem}
}

var (
	errUnknownDBType  = errors.New("unknown DB type")
	errRunCalledTwice = errors.New("cannot run a server instance twice")
)

type blockNtfnType int

const (
	blockConnected blockNtfnType = iota
	blockDisconnected
)

type blockNtfn struct {
	ntfnType blockNtfnType
	header   *wire.BlockHeader
}

// ServerConfig specifies the config options when initializing a new server
// instance.
type ServerConfig struct {
	ChainParams *chaincfg.Params
	DcrdCfg     *rpcclient.ConnConfig
	DBType      DBType
	DBDir       string

	CacheSizeBlocks uint
	CacheSizeRawTxs uint

	// The following fields are only defined during tests.

	c           chain
	db          backenddb.DB
	peerTimeout time.Duration
}

// Server offers Decred blocks and services while following the Rosetta specs.
//
// Note: the server assumes the underlying chain is correctly following
// consensus rules. In other words, it does _not_ validate the chain itself,
// but rather relies on the fact that the underlying dcrd (or other node
// implementation) _only_ generates valid chain data.
type Server struct {
	c           chain
	chainParams *chaincfg.Params
	asserter    *asserter.Asserter
	network     *rtypes.NetworkIdentifier
	db          backenddb.DB
	dbType      DBType
	peerTimeout time.Duration

	// concurrency is used to define how many goroutines are executed under
	// certain situations.
	concurrency int

	// bcl logs the number of connected blocks during certain intervals.
	bcl *blocksConnectedLogger

	// Caches for speeding up operations.
	cacheBlocks *lru.KVCache
	cacheRawTxs *lru.KVCache

	// The given mtx mutex protects the following fields.
	mtx            sync.Mutex
	dcrdActiveErr  error
	dcrdVersion    string
	blockNtfns     []*blockNtfn
	blockNtfnsChan chan struct{}
	ctx            context.Context
	syncStatus     rtypes.SyncStatus
}

// NewServer creates a new server instance.
func NewServer(cfg *ServerConfig) (*Server, error) {
	network := &rtypes.NetworkIdentifier{
		Blockchain: "decred",
		Network:    cfg.ChainParams.Name,
	}

	allTypes := types.AllOpTypes()
	histBalance := true
	var callMethods []string
	mempoolCoins := false
	astr, err := asserter.NewServer(allTypes, histBalance,
		[]*rtypes.NetworkIdentifier{network}, callMethods, mempoolCoins)
	if err != nil {
		return nil, err
	}

	// Setup in-memory caches.
	cacheBlocks := lru.NewKVCache(cfg.CacheSizeBlocks)
	cacheRawTxs := lru.NewKVCache(cfg.CacheSizeRawTxs)

	var db backenddb.DB
	switch cfg.DBType {
	case DBTypeMem:
		db, err = memdb.NewMemDB()
	case DBTypeBadger:
		db, err = badgerdb.NewBadgerDB(cfg.DBDir)
	case DBTypeBadgerMem:
		db, err = badgerdb.NewBadgerDB("")
	case dbTypePreconfigured:
		db = cfg.db
	default:
		err = errUnknownDBType
	}
	if err != nil {
		return nil, err
	}

	s := &Server{
		c:              cfg.c,
		chainParams:    cfg.ChainParams,
		asserter:       astr,
		network:        network,
		cacheBlocks:    &cacheBlocks,
		cacheRawTxs:    &cacheRawTxs,
		db:             db,
		dbType:         cfg.DBType,
		blockNtfns:     make([]*blockNtfn, 0),
		blockNtfnsChan: make(chan struct{}),
		concurrency:    runtime.NumCPU(),
		bcl:            &blocksConnectedLogger{lastTime: time.Now()},
		dcrdActiveErr:  errDcrdUnconnected,
		peerTimeout:    cfg.peerTimeout,
	}

	// Initialize connection to underlying dcrd node if an existing chain
	// implementation wasn't provided.
	if cfg.c == nil {
		// We make a copy of the passed config because we change some
		// of the parameters locally to ensure they are configured as
		// needed by the Server struct.
		connCfg := *cfg.DcrdCfg
		connCfg.DisableConnectOnNew = true
		connCfg.DisableAutoReconnect = false
		connCfg.HTTPPostMode = false
		s.c, err = rpcclient.New(&connCfg, s.ntfnHandlers())
		if err != nil {
			db.Close()
			return nil, err
		}
	}

	return s, nil
}

// isDcrdActive returns an error if the server is not connected to a suitable
// underlying dcrd node.
func (s *Server) isDcrdActive() error {
	s.mtx.Lock()
	dcrdActiveErr := s.dcrdActiveErr
	s.mtx.Unlock()
	return dcrdActiveErr
}

// onDcrdConnected is called when the underlying chain implementation connected
// (or reconnected) to a dcrd instance.
func (s *Server) onDcrdConnected() {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	svrLog.Debugf("Reconnected to the dcrd instance")
	version, err := checkDcrd(s.ctx, s.c, s.chainParams)
	if err != nil {
		s.dcrdVersion = ""
		s.dcrdActiveErr = errDcrdUnsuitable

		svrLog.Errorf("Reconnected to unsuitable dcrd node: %v", err)
		svrLog.Infof("Disabling server operations")
		return
	}

	s.dcrdVersion = version
	s.dcrdActiveErr = nil
}

// notifyNewBlockEvent asynchronously signals the main Run() goroutine that new
// block notifications have arrived.
//
// This must be called with the server lock held.
func (s *Server) notifyNewBlockEvent() {
	ctx := s.ctx

	go func() {
		select {
		case <-ctx.Done():
		case s.blockNtfnsChan <- struct{}{}:
		}
	}()
}

// onDcrdBlockConnected is called when new blocks are connected to the main
// dcrd chain of the underlying chain implementation.
//
// This may be called by the main rpcclient goroutine so it must not block for
// long periods of time and cannot call chain functions itself.
func (s *Server) onDcrdBlockConnected(blockHeader []byte, transactions [][]byte) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.dcrdActiveErr != nil {
		svrLog.Debugf("Ignoring block connected from unsuitable dcrd node")
		return
	}

	var header wire.BlockHeader
	if err := header.FromBytes(blockHeader); err != nil {
		svrLog.Errorf("Unable to decode blockheader on block connected: %v", err)
		return
	}

	ntfn := &blockNtfn{header: &header, ntfnType: blockConnected}
	s.blockNtfns = append(s.blockNtfns, ntfn)
	s.notifyNewBlockEvent()
}

// onDcrdBlockDisconnected is called when new blocks are disconnected from the
// main dcrd chain of the underlying chain implementation.
//
// This may be called by the main rpcclient goroutine so it must not block for
// long periods of time and cannot call chain functions itself.
func (s *Server) onDcrdBlockDisconnected(blockHeader []byte) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.dcrdActiveErr != nil {
		svrLog.Debugf("Ignoring block disconnected from unsuitable dcrd node")
		return
	}

	var header wire.BlockHeader
	if err := header.FromBytes(blockHeader); err != nil {
		svrLog.Errorf("Unable to decode blockheader on block disconnected: %v", err)
		return
	}

	ntfn := &blockNtfn{header: &header, ntfnType: blockDisconnected}
	s.blockNtfns = append(s.blockNtfns, ntfn)
	s.notifyNewBlockEvent()
}

func (s *Server) ntfnHandlers() *rpcclient.NotificationHandlers {
	return &rpcclient.NotificationHandlers{
		OnClientConnected:   s.onDcrdConnected,
		OnBlockConnected:    s.onDcrdBlockConnected,
		OnBlockDisconnected: s.onDcrdBlockDisconnected,
	}
}

func (s *Server) Routers() []rserver.Router {
	return []rserver.Router{
		rserver.NewNetworkAPIController(s, s.asserter),
		rserver.NewBlockAPIController(s, s.asserter),
		rserver.NewMempoolAPIController(s, s.asserter),
		rserver.NewConstructionAPIController(s, s.asserter),
		rserver.NewAccountAPIController(s, s.asserter),
	}
}

// Run starts all service goroutines and blocks until the passed context is
// canceled.
//
// NOTE: the passed context MUST be the same one passed for New() otherwise the
// server's behavior is undefined.
func (s *Server) Run(ctx context.Context) error {
	// We rely on ctx being non-nil for duplicate Run() execution
	// detection.
	if ctx == nil {
		return errors.New("ctx is required on Run()")
	}

	// Prevent running twice and store the server context needed during
	// notifications.
	s.mtx.Lock()
	if s.ctx != nil {
		s.mtx.Unlock()
		return errRunCalledTwice
	}
	s.ctx = ctx
	s.mtx.Unlock()

	// Run the helper goroutines.
	go s.c.Connect(ctx, true)
	go s.bcl.run(ctx)

	// Defer the cleanup func.
	defer func() {
		if s.dbType != dbTypePreconfigured {
			err := s.db.Close()
			if err != nil {
				svrLog.Errorf("Unable to close DB: %v", err)
			}
		}
	}()

	// Wait until the dcrd and dcrros nodes are ready to function.
	if err := s.waitForDcrdConnection(ctx); err != nil {
		return err
	}
	peerTimeout := 10 * time.Second
	if s.peerTimeout != 0 {
		peerTimeout = s.peerTimeout
	}
	if err := s.waitForPeers(ctx, peerTimeout); err != nil {
		// Skip errNoPeersAfterTimeout since it means no peers were
		// found (we're probably an offline node).
		if !errors.Is(err, errNoPeersAfterTimeout) {
			return err
		}
	}
	if err := s.waitForBlockchainSync(ctx); err != nil {
		return err
	}
	if err := s.preProcessAccounts(ctx); err != nil {
		return err
	}

	// All preprocessing is done, so clear the syncStatus var.
	s.mtx.Lock()
	s.syncStatus = rtypes.SyncStatus{}
	s.mtx.Unlock()

	// Now that we've processed the accounts, we can register for block
	// notifications.
	if err := s.c.NotifyBlocks(ctx); err != nil {
		return err
	}

	svrLog.Infof("Waiting for block notifications")

	// Handle server events.
	var err error
nextevent:
	for {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return err

		case <-s.blockNtfnsChan:
			s.mtx.Lock()
			if len(s.blockNtfns) == 0 {
				// Shouldn't really happen unless there's a
				// racing bug somewhere.
				s.mtx.Unlock()
				continue nextevent
			}
			ntfn := s.blockNtfns[0]
			if len(s.blockNtfns) > 1 {
				copy(s.blockNtfns, s.blockNtfns[1:])
				s.blockNtfns[len(s.blockNtfns)-1] = nil
			}
			s.blockNtfns = s.blockNtfns[:len(s.blockNtfns)-1]
			s.mtx.Unlock()

			switch ntfn.ntfnType {
			case blockConnected:
				err = s.handleBlockConnected(ctx, ntfn.header)
			case blockDisconnected:
				err = s.handleBlockDisconnected(ctx, ntfn.header)
			default:
				err = fmt.Errorf("unknown notification type")
			}

			if err != nil {
				return err
			}
		}
	}
}
