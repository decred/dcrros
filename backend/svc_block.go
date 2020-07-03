// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"
	"fmt"
	"sync"

	"decred.org/dcrros/types"
	rserver "github.com/coinbase/rosetta-sdk-go/server"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/wire"
	"golang.org/x/sync/errgroup"
)

var _ rserver.BlockAPIServicer = (*Server)(nil)

func (s *Server) inputsFetcher(ctx context.Context, utxoSet map[wire.OutPoint]*types.PrevInput, inputList ...*wire.OutPoint) (map[wire.OutPoint]*types.PrevInput, error) {
	res := make(map[wire.OutPoint]*types.PrevInput, len(inputList))

	// First, dedupe the needed txs.
	txs := make(map[chainhash.Hash]*wire.MsgTx)
	for _, in := range inputList {
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
	for _, in := range inputList {
		tx := txs[in.Hash]
		if tx == nil {
			// Already got it through the utxoSet.
			continue
		}
		if len(tx.TxOut) <= int(in.Index) {
			return nil, fmt.Errorf("non-existant output index %s", in.String())
		}
		out := tx.TxOut[in.Index]
		res[*in] = &types.PrevInput{
			PkScript: out.PkScript,
			Version:  out.Version,
			Amount:   dcrutil.Amount(out.Value),
		}
	}

	return res, nil
}

func (s *Server) makeInputsFetcher(ctx context.Context, utxoSet map[wire.OutPoint]*types.PrevInput) types.PrevInputsFetcher {
	return func(inputList ...*wire.OutPoint) (map[wire.OutPoint]*types.PrevInput, error) {
		return s.inputsFetcher(ctx, utxoSet, inputList...)
	}
}

// Block returns the block identified on the request as a rosetta encoded
// block.
//
// NOTE: this is part of the BlockAPIServicer interface.
func (s *Server) Block(ctx context.Context, req *rtypes.BlockRequest) (*rtypes.BlockResponse, *rtypes.Error) {
	_, _, b, err := s.getBlockByPartialId(ctx, req.BlockIdentifier)
	if err != nil {
		return nil, types.DcrdError(err)
	}
	var prev *wire.MsgBlock

	// Fetch the previous block when the current block disapproves of its
	// parent, since we'll need to reverse the transactions in the parent.
	// We include a special check for the genesis block because it has
	// VoteBits == 0.
	approvesParent := b.Header.VoteBits&0x01 == 0x01
	if !approvesParent && b.Header.Height > 0 {
		prev, err = s.c.GetBlock(ctx, &b.Header.PrevBlock)
		if err != nil {
			return nil, types.DcrdError(err, types.MapRpcErrCode(-5, types.ErrBlockNotFound))
		}
	}

	fetchInputs := s.makeInputsFetcher(ctx, nil)
	rblock, err := types.WireBlockToRosetta(b, prev, fetchInputs, s.chainParams)
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
