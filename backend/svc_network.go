package backend

import (
	"context"
	"runtime"

	rserver "github.com/coinbase/rosetta-sdk-go/server"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrros/internal/version"
	"github.com/decred/dcrros/types"
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

// NetworkStatus returns the current status of the chain as seen by dcrros and
// the underlying dcrd.
//
// This is part of the NetworkAPIServicer interface.
func (s *Server) NetworkStatus(ctx context.Context, req *rtypes.NetworkRequest) (
	*rtypes.NetworkStatusResponse, *rtypes.Error) {

	// We need the timestamp of the block, so request the best block hash
	// then the block.
	hash, height, block, err := s.bestBlock(ctx)
	if err != nil {
		return nil, types.DcrdError(err)
	}

	// Rosetta timestamp is in milliseconds.
	timestamp := block.Header.Timestamp.Unix() * 1000
	return &rtypes.NetworkStatusResponse{
		CurrentBlockIdentifier: &rtypes.BlockIdentifier{
			Hash:  hash.String(),
			Index: height,
		},
		CurrentBlockTimestamp: timestamp,
		GenesisBlockIdentifier: &rtypes.BlockIdentifier{
			Hash: s.chainParams.GenesisHash.String(),
		},

		// TODO: syncing peers?
		Peers: nil,
	}, nil
}
