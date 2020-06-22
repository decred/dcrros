package backend

import (
	"context"
	"runtime"

	rserver "github.com/coinbase/rosetta-sdk-go/server"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrros/internal/version"
)

// Compile time directive to ensure Server implements the NetworkAPIServicer
// interface.
var _ rserver.NetworkAPIServicer = (*Server)(nil)

// NetworkList returns the list of available networks in this server.
//
// This is part of the NetworkAPIServicer inteface.
func (s *Server) NetworkList(context.Context, *rtypes.MetadataRequest) (
	*rtypes.NetworkListResponse, *rtypes.Error) {

	return &rtypes.NetworkListResponse{
		NetworkIdentifiers: []*rtypes.NetworkIdentifier{
			s.network,
		},
	}, nil
}

// NetworkOptions returns the current version and available options of this
// rosetta implementation.
//
// This is part of the NetworkAPIServicer interface.
func (s *Server) NetworkOptions(context.Context, *rtypes.NetworkRequest) (
	*rtypes.NetworkOptionsResponse, *rtypes.Error) {

	dcrrosVersion := version.String()

	s.mtx.Lock()
	dcrdVersion := s.dcrdVersion
	s.mtx.Unlock()

	versionMeta := make(map[string]interface{})
	versionMeta["build_metadata"] = version.BuildMetadata
	versionMeta["runtime"] = runtime.Version()
	return &rtypes.NetworkOptionsResponse{
		Version: &rtypes.Version{
			RosettaVersion:    rosettaVersion,
			NodeVersion:       dcrdVersion,
			MiddlewareVersion: &dcrrosVersion,
			Metadata:          versionMeta,
		},
		Allow: &rtypes.Allow{},
	}, nil
}

func (s *Server) NetworkStatus(context.Context, *rtypes.NetworkRequest) (
	*rtypes.NetworkStatusResponse, *rtypes.Error) {

	return &rtypes.NetworkStatusResponse{
		CurrentBlockIdentifier: nil,
		CurrentBlockTimestamp:  0,
		GenesisBlockIdentifier: nil,
		Peers:                  nil,
	}, nil
}
