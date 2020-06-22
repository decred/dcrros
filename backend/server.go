package backend

import (
	"context"

	"github.com/coinbase/rosetta-sdk-go/asserter"
	rserver "github.com/coinbase/rosetta-sdk-go/server"
	"github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/v3"
)

type ServerConfig struct {
	ChainParams *chaincfg.Params
}

type Server struct {
	chainParams *chaincfg.Params
	asserter    *asserter.Asserter
	network     *types.NetworkIdentifier
}

func NewServer(ctx context.Context, cfg *ServerConfig) (*Server, error) {
	network := &types.NetworkIdentifier{
		Blockchain: "decred",
		Network:    cfg.ChainParams.Name,
	}

	astr, err := asserter.NewServer([]*types.NetworkIdentifier{network})
	if err != nil {
		return nil, err
	}
	return &Server{
		chainParams: cfg.ChainParams,
		asserter:    astr,
		network:     network,
	}, nil
}

func (s *Server) Routers() []rserver.Router {
	return []rserver.Router{
		rserver.NewNetworkAPIController(s, s.asserter),
	}
}

func (s *Server) Run(ctx context.Context) error {
	return nil
}
