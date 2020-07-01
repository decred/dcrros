package backend

import (
	"context"
	"encoding/hex"
	"strings"

	"decred.org/dcrros/types"
	rserver "github.com/coinbase/rosetta-sdk-go/server"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/dcrjson/v3"
	"github.com/decred/dcrd/wire"
)

var _ rserver.ConstructionAPIServicer = (*Server)(nil)

// ConstructionMetadata returns metadata required to build a valid Decred
// transaction.
//
// Decred doesn't currently need any metadata.
//
// NOTE: This is part of the ConstructionAPIServicer interface.
func (s *Server) ConstructionMetadata(context.Context, *rtypes.ConstructionMetadataRequest) (*rtypes.ConstructionMetadataResponse, *rtypes.Error) {
	return &rtypes.ConstructionMetadataResponse{
		Metadata: map[string]interface{}{},
	}, nil
}

// ConstructionSubmit submits the provided transaction to the Decred network.
//
// NOTE: This is part of the ConstructionAPIServicer interface.
func (s *Server) ConstructionSubmit(ctx context.Context, req *rtypes.ConstructionSubmitRequest) (*rtypes.ConstructionSubmitResponse, *rtypes.Error) {
	txBytes, err := hex.DecodeString(req.SignedTransaction)
	if err != nil {
		return nil, types.ErrInvalidHexString.RError()
	}

	tx := new(wire.MsgTx)
	err = tx.FromBytes(txBytes)
	if err != nil {
		return nil, types.ErrInvalidTransaction.RError()
	}

	txh, err := s.c.SendRawTransaction(ctx, tx, false)
	if err != nil {
		// Handle some special cases from dcrd into different error
		// codes.
		if rpcerr, ok := err.(*dcrjson.RPCError); ok {
			if rpcerr.Code == dcrjson.ErrRPCDuplicateTx {
				return nil, types.ErrAlreadyHaveTx.Msg(err.Error()).RError()

			} else if rpcerr.Code == dcrjson.ErrRPCMisc && strings.Index(rpcerr.Error(), "transaction already exists") > 0 {
				return nil, types.ErrTxAlreadyMined.Msg(err.Error()).RError()
			} else if rpcerr.Code == dcrjson.ErrRPCMisc {
				// Generic rule error.
				return nil, types.ErrProcessingTx.Msg(err.Error()).RError()
			}

			return nil, types.DcrdError(err)
		}
	}

	return &rtypes.ConstructionSubmitResponse{
		TransactionIdentifier: &rtypes.TransactionIdentifier{
			Hash: txh.String(),
		},
	}, nil
}
