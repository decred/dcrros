package backend

import (
	"context"

	rserver "github.com/coinbase/rosetta-sdk-go/server"
	"github.com/coinbase/rosetta-sdk-go/types"
)

// Compile time directive to ensure Server implements the NetworkAPIServicer
// interface.
var _ rserver.NetworkAPIServicer = (*Server)(nil)

// NetworkList returns the list of available networks in this server.
//
// This is part of the NetworkAPIServicer inteface.
func (s *Server) NetworkList(context.Context, *types.MetadataRequest) (
	*types.NetworkListResponse, *types.Error) {

	return &types.NetworkListResponse{
		NetworkIdentifiers: []*types.NetworkIdentifier{
			s.network,
		},
	}, nil
}

func (s *Server) NetworkOptions(context.Context, *types.NetworkRequest) (
	*types.NetworkOptionsResponse, *types.Error) {
	return nil, nil
}

func (s *Server) NetworkStatus(context.Context, *types.NetworkRequest) (
	*types.NetworkStatusResponse, *types.Error) {
	return nil, nil
}
