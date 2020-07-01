package backend

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrros/backend/backenddb"
	"decred.org/dcrros/backend/internal/badgerdb"
	"decred.org/dcrros/backend/internal/memdb"
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
	rosettaVersion = "1.3.1"
)

type DBType string

const (
	DBTypeMem       DBType = "mem"
	DBTypeBadger    DBType = "badger"
	DBTypeBadgerMem DBType = "badgermem"
)

func SupportedDBTypes() []DBType {
	return []DBType{DBTypeMem, DBTypeBadger, DBTypeBadgerMem}
}

type blockNtfnType int

const (
	blockConnected blockNtfnType = iota
	blockDisconnected
)

type blockNtfn struct {
	ntfnType blockNtfnType
	header   *wire.BlockHeader
}

type ServerConfig struct {
	ChainParams *chaincfg.Params
	DcrdCfg     *rpcclient.ConnConfig
	DBType      DBType
	DBDir       string
}

type Server struct {
	c           *rpcclient.Client
	ctx         context.Context
	chainParams *chaincfg.Params
	asserter    *asserter.Asserter
	network     *rtypes.NetworkIdentifier
	db          backenddb.DB

	blocks      *lru.KVCache
	blockHashes *lru.KVCache
	accountTxs  *lru.KVCache
	rawTxs      *lru.KVCache

	// The given mtx mutex protects the following fields.
	mtx            sync.Mutex
	active         bool
	dcrdVersion    string
	blockNtfns     []*blockNtfn
	blockNtfnsChan chan struct{}
}

func NewServer(ctx context.Context, cfg *ServerConfig) (*Server, error) {
	network := &rtypes.NetworkIdentifier{
		Blockchain: "decred",
		Network:    cfg.ChainParams.Name,
	}

	astr, err := asserter.NewServer([]*rtypes.NetworkIdentifier{network})
	if err != nil {
		return nil, err
	}

	blockCache := lru.NewKVCache(0)
	blockHashCache := lru.NewKVCache(0)
	accountTxsCache := lru.NewKVCache(0)
	txsCache := lru.NewKVCache(0)

	var db backenddb.DB
	switch cfg.DBType {
	case DBTypeMem:
		db, err = memdb.NewMemDB()
	case DBTypeBadger:
		db, err = badgerdb.NewBadgerDB(cfg.DBDir)
	case DBTypeBadgerMem:
		db, err = badgerdb.NewBadgerDB("")
	default:
		err = errors.New("unknown db type")
	}
	if err != nil {
		return nil, err
	}

	s := &Server{
		chainParams:    cfg.ChainParams,
		asserter:       astr,
		network:        network,
		ctx:            ctx,
		blocks:         &blockCache,
		blockHashes:    &blockHashCache,
		accountTxs:     &accountTxsCache,
		rawTxs:         &txsCache,
		db:             db,
		blockNtfns:     make([]*blockNtfn, 0),
		blockNtfnsChan: make(chan struct{}),
	}

	// We make a copy of the passed config because we change some of the
	// parameters locally to ensure they are configured as needed by the
	// Server struct.
	connCfg := *cfg.DcrdCfg
	connCfg.DisableConnectOnNew = true
	connCfg.DisableAutoReconnect = false
	connCfg.HTTPPostMode = false
	s.c, err = rpcclient.New(&connCfg, s.ntfnHandlers())
	if err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Server) Active() bool {
	s.mtx.Lock()
	active := s.active
	s.mtx.Unlock()
	return active
}

func (s *Server) onDcrdConnected() {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// Ideally these would be done on a onDcrdDisconnected() callback but
	// rpcclient doesn't currently offer that.
	s.active = false
	s.dcrdVersion = ""

	svrLog.Debugf("Reconnected to the dcrd instance")
	version, err := checkDcrd(s.ctx, s.c, s.chainParams)
	if err != nil {
		svrLog.Error(err)
		svrLog.Infof("Disabling server operations")
		return
	}

	s.active = true
	s.dcrdVersion = version
}

func (s *Server) notifyNewBlockEvent() {
	go func() {
		select {
		case <-s.ctx.Done():
		case s.blockNtfnsChan <- struct{}{}:
		}
	}()
}

func (s *Server) onDcrdBlockConnected(blockHeader []byte, transactions [][]byte) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	var header wire.BlockHeader
	if err := header.FromBytes(blockHeader); err != nil {
		svrLog.Errorf("Unable to decode blockheader on block connected: %v", err)
		return
	}

	ntfn := &blockNtfn{header: &header, ntfnType: blockConnected}
	s.blockNtfns = append(s.blockNtfns, ntfn)
	s.notifyNewBlockEvent()
}

func (s *Server) handleBlockConnected(ctx context.Context, header *wire.BlockHeader) error {

	blockHash := header.BlockHash()

	// Ensure our current tip matches the chain extended by the new block.
	err := s.db.View(s.ctx, func(dbtx backenddb.ReadTx) error {
		tipHash, tipHeight, err := s.db.LastProcessedBlock(dbtx)
		if err != nil {
			return err
		}

		if tipHash != header.PrevBlock || tipHeight != int64(header.Height-1) {
			return fmt.Errorf("Current tip %d (%s) does not match "+
				"prev block of connected block %s",
				tipHeight, tipHash, blockHash)
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Fetch the full previous block.
	prev, err := s.getBlock(s.ctx, &header.PrevBlock)
	if err != nil {
		return fmt.Errorf("Unable to fetch previous block %s of connected "+
			"block %s: %v", header.PrevBlock, blockHash, err)
	}

	// Fetch the full block as a wire.MsgBlock.
	//
	// TODO: avoid having to get this block again (build based on the
	// header and transactions).
	b, err := s.getBlock(s.ctx, &blockHash)
	if err != nil {
		return fmt.Errorf("Unable to fetch new connected block %s: %v",
			blockHash, err)
	}

	// Process the accounts modified by the block.
	err = s.preProcessAccountBlock(s.ctx, &blockHash, b, prev, nil)
	if err != nil {
		return fmt.Errorf("Unable to process accounts of connected block "+
			"%s: %v", blockHash, err)
	}

	svrLog.Infof("Connected block %s at height %d", blockHash, header.Height)
	return nil
}

func (s *Server) onDcrdBlockDisconnected(blockHeader []byte) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	var header wire.BlockHeader
	if err := header.FromBytes(blockHeader); err != nil {
		svrLog.Errorf("Unable to decode blockheader on block disconnected: %v", err)
		return
	}

	ntfn := &blockNtfn{header: &header, ntfnType: blockDisconnected}
	s.blockNtfns = append(s.blockNtfns, ntfn)
	s.notifyNewBlockEvent()
}

func (s *Server) handleBlockDisconnected(ctx context.Context, header *wire.BlockHeader) error {
	blockHash := header.BlockHash()
	err := s.db.Update(s.ctx, func(dbtx backenddb.WriteTx) error {
		// Ensure our current tip matches the chain rolled back by the
		// disconnected block.
		tipHash, tipHeight, err := s.db.LastProcessedBlock(dbtx)
		if err != nil {
			return err
		}

		if tipHash != blockHash || tipHeight != int64(header.Height) {
			return fmt.Errorf("Current tip %d (%s) does not match "+
				"disconnected block %s",
				tipHeight, tipHash, blockHash)
		}

		// Rollback this block.
		return s.db.RollbackTip(dbtx, int64(header.Height), tipHash)
	})
	if err != nil {
		return err
	}

	svrLog.Infof("Disconnected block %s at height %d", blockHash, header.Height)
	return nil
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
	go s.c.Connect(ctx, true)
	time.Sleep(time.Millisecond * 100)

	if err := s.waitForBlockchainSync(ctx); err != nil {
		s.db.Close()
		return err
	}

	err := s.preProcessAccounts(ctx)
	if err != nil {
		s.db.Close()
		return err
	}

	// Now that we've processed the accounts, we can register for block
	// notifications.
	if err := s.c.NotifyBlocks(ctx); err != nil {
		return err
	}
	svrLog.Infof("Waiting for block notifications")

	// Handle server events.
nextevent:
	for {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			break nextevent

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
				break nextevent
			}
		}
	}
	s.db.Close()
	return err
}
