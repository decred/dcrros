// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"

	"decred.org/dcrros/types"
	rserver "github.com/coinbase/rosetta-sdk-go/server"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
)

var _ rserver.MempoolAPIServicer = (*Server)(nil)

func (s *Server) Mempool(ctx context.Context, req *rtypes.NetworkRequest) (*rtypes.MempoolResponse, *rtypes.Error) {
	mempool, err := s.c.GetRawMempool(ctx, chainjson.GRMAll)
	if err != nil {
		return nil, types.RError(err)
	}
	txs := make([]*rtypes.TransactionIdentifier, len(mempool))
	for i, txh := range mempool {
		txs[i] = &rtypes.TransactionIdentifier{
			Hash: txh.String(),
		}
	}
	return &rtypes.MempoolResponse{
		TransactionIdentifiers: txs,
	}, nil
}

func (s *Server) MempoolTransaction(ctx context.Context, req *rtypes.MempoolTransactionRequest) (*rtypes.MempoolTransactionResponse, *rtypes.Error) {

	if err := s.isDcrdActive(); err != nil {
		return nil, types.ErrChainUnavailable.Msg(err.Error()).RError()
	}

	var txh chainhash.Hash
	err := chainhash.Decode(&txh, req.TransactionIdentifier.Hash)
	if err != nil {
		return nil, types.ErrInvalidChainHash.RError()
	}

	tx, err := s.c.GetRawTransaction(ctx, &txh)
	if rpcerr, ok := err.(*dcrjson.RPCError); ok && rpcerr.Code == dcrjson.ErrRPCNoTxInfo {
		return nil, types.ErrTxNotFound.RError()
	}
	if err != nil {
		return nil, types.RError(err)
	}

	// TODO: What if the returned tx has already been mined?
	fetchInputs := s.makeInputsFetcher(ctx, nil)
	rtx, err := types.MempoolTxToRosetta(tx.MsgTx(), fetchInputs, s.chainParams)
	if err != nil {
		return nil, types.RError(err)
	}
	return &rtypes.MempoolTransactionResponse{
		Transaction: rtx,
	}, nil
}
