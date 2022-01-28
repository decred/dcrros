// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"
	"sync"

	"decred.org/dcrros/types"
	rserver "github.com/coinbase/rosetta-sdk-go/server"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
	"golang.org/x/sync/errgroup"
)

var _ rserver.BlockAPIServicer = (*Server)(nil)

// prevOutsFetcher fetches information about a set of target utxos specified by
// their outpoints.
//
// Data is fetched from either the passed utxoSet map (if it exists) or from
// the underlying dcrd instance.
func (s *Server) prevOutsFetcher(ctx context.Context, utxoSet map[wire.OutPoint]*types.PrevOutput, targets ...*wire.OutPoint) (map[wire.OutPoint]*types.PrevOutput, error) {
	res := make(map[wire.OutPoint]*types.PrevOutput, len(targets))

	// First, dedupe the needed txs.
	txs := make(map[chainhash.Hash]*wire.MsgTx)
	for _, in := range targets {
		if prev, ok := utxoSet[*in]; ok {
			res[*in] = prev
			continue
		}
		txs[in.Hash] = nil
	}
	txhs := make([]chainhash.Hash, 0, len(txs))
	for txh := range txs {
		txhs = append(txhs, txh)
	}
	if len(txhs) == 0 {
		// The utxoSet already had all entries.
		return res, nil
	}

	// Now, request the txs concurrently from dcrd (assumes txindex is on).
	g, gctx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	for _, txh := range txhs {
		txh := txh
		g.Go(func() error {
			tx, err := s.getRawTx(gctx, &txh)
			if isErrNoTxInfo(err) {
				return types.ErrPrevOutTxNotFound.Msgf("tx %s", txh)
			}
			if err != nil {
				return err
			}
			mu.Lock()
			txs[txh] = tx
			mu.Unlock()
			return nil
		})
	}

	err := g.Wait()
	if err != nil {
		return nil, err
	}

	// Now build the resulting map.
	for _, in := range targets {
		tx := txs[in.Hash]
		if tx == nil {
			// Already got it through the utxoSet.
			continue
		}
		if len(tx.TxOut) <= int(in.Index) {
			return nil, types.ErrPrevOutIndexNotFound.Msgf(
				"tx %s index %d", in.Hash, in.Index)
		}
		out := tx.TxOut[in.Index]
		res[*in] = &types.PrevOutput{
			PkScript: out.PkScript,
			Version:  out.Version,
			Amount:   dcrutil.Amount(out.Value),
		}
	}

	return res, nil
}

// makePrevOutsFetcher wraps the given context to produce a cancelable function
// to fetch previous outputs.
func (s *Server) makePrevOutsFetcher(ctx context.Context, utxoSet map[wire.OutPoint]*types.PrevOutput) types.PrevOutputsFetcher {
	return func(inputList ...*wire.OutPoint) (map[wire.OutPoint]*types.PrevOutput, error) {
		return s.prevOutsFetcher(ctx, utxoSet, inputList...)
	}
}

// Block returns the block identified on the request as a rosetta encoded
// block.
//
// NOTE: this is part of the BlockAPIServicer interface.
func (s *Server) Block(ctx context.Context, req *rtypes.BlockRequest) (*rtypes.BlockResponse, *rtypes.Error) {
	_, _, b, err := s.getBlockByPartialId(ctx, req.BlockIdentifier)
	if err != nil {
		return nil, types.RError(err)
	}
	var prev *wire.MsgBlock

	// Fetch the previous block when the current block disapproves of its
	// parent, since we'll need to reverse the transactions in the parent.
	// We include a special check for the genesis block because it has
	// VoteBits == 0.
	approvesParent := b.Header.VoteBits&0x01 == 0x01
	if !approvesParent && b.Header.Height > 0 {
		prev, err = s.getBlock(ctx, &b.Header.PrevBlock)
		if rpcerr, ok := err.(*dcrjson.RPCError); ok && rpcerr.Code == dcrjson.ErrRPCBlockNotFound {
			return nil, types.ErrBlockNotFound.RError()
		}

		if err != nil {
			return nil, types.RError(err)
		}
	}

	fetchPrevOuts := s.makePrevOutsFetcher(ctx, nil)
	rblock, err := types.WireBlockToRosetta(b, prev, fetchPrevOuts, s.chainParams)
	if err != nil {
		return nil, types.RError(err)
	}
	return &rtypes.BlockResponse{
		Block: rblock,
	}, nil
}

// BlockTransaction returns additional transactions related to the specified
// block, not returned by the Block() call.
//
// This is currently unused in Decred given that all relevant transactions are
// returned by Block().
//
// NOTE: this is part of the BlockAPIServicer interface.
func (s *Server) BlockTransaction(context.Context, *rtypes.BlockTransactionRequest,
) (*rtypes.BlockTransactionResponse, *rtypes.Error) {
	return nil, types.ErrUnimplemented.RError()
}
