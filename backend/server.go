package backend

import (
	"context"
	"sync"

	"github.com/coinbase/rosetta-sdk-go/asserter"
	rserver "github.com/coinbase/rosetta-sdk-go/server"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/rpcclient/v6"
)

const (
	// rosettaVersion is the version of the rosetta spec this backend
	// currently implements.
	rosettaVersion = "1.3.1"
)

type ServerConfig struct {
	ChainParams *chaincfg.Params
	DcrdCfg     *rpcclient.ConnConfig
}

type Server struct {
	c           *rpcclient.Client
	ctx         context.Context
	chainParams *chaincfg.Params
	asserter    *asserter.Asserter
	network     *rtypes.NetworkIdentifier

	// The given mtx mutex protects the following fields.
	mtx         sync.Mutex
	active      bool
	dcrdVersion string
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
	s := &Server{
		chainParams: cfg.ChainParams,
		asserter:    astr,
		network:     network,
		ctx:         ctx,
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

func (s *Server) ntfnHandlers() *rpcclient.NotificationHandlers {
	return &rpcclient.NotificationHandlers{
		OnClientConnected: s.onDcrdConnected,
	}
}

func (s *Server) Routers() []rserver.Router {
	return []rserver.Router{
		rserver.NewNetworkAPIController(s, s.asserter),
		rserver.NewBlockAPIController(s, s.asserter),
		rserver.NewMempoolAPIController(s, s.asserter),
	}
}

// Run starts all service goroutines and blocks until the passed context is
// canceled.
//
// NOTE: the passed context MUST be the same one passed for New() otherwise the
// server's behavior is undefined.
func (s *Server) Run(ctx context.Context) error {
	go s.c.Connect(ctx, true)

	select {
	case <-ctx.Done():
	}
	return ctx.Err()
}
