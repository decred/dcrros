// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"
	"fmt"

	"decred.org/dcrros/backend/backenddb"
	"decred.org/dcrros/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
)

// rollbackDbChain rolls back the db chain until we find a common block between
// the db and the blockchain, assuming the chain is at the specified target
// hash and height.
//
// It returns the chainhash and height for the new tip.
func (s *Server) rollbackDbChain(dbtx backenddb.WriteTx,
	targetHash *chainhash.Hash, targetHeight int64) (*chainhash.Hash, int64, error) {

	tipHash, tipHeight, err := s.db.LastProcessedBlock(dbtx)
	if err != nil {
		return nil, 0, err
	}

	// Special case when we don't have anything to do.
	if targetHeight == tipHeight && targetHash.IsEqual(&tipHash) {
		return &tipHash, tipHeight, nil
	}

	// If the target is lower than the current tip, it's likely we're in
	// the middle of a reorg. Roll back until we find the target height.
	for tipHeight > 0 && tipHeight > targetHeight {
		var err error

		svrLog.Debugf("Rolling back rewinded tip %d %s", tipHeight, tipHash)
		if err = s.db.RollbackTip(dbtx, tipHash, tipHeight); err != nil {
			return nil, 0, err
		}
		if tipHash, tipHeight, err = s.db.LastProcessedBlock(dbtx); err != nil {
			return nil, 0, err
		}
	}

	// If the target is higher than the current tip, we missed some blocks.
	// Fetch the block hash of the chain at the current height.
	chainHash := targetHash
	if targetHeight > tipHeight {
		var err error
		chainHash, err = s.c.GetBlockHash(dbtx.Context(), tipHeight)
		if err != nil {
			return nil, 0, err
		}
	}

	// Now roll back until we find a block shared by both our db and the
	// chain which tip is the targetHash at this moment.
	rolledBack := false
	for tipHeight > 0 && !chainHash.IsEqual(&tipHash) {
		var err error
		svrLog.Debugf("Rolling back reorged tip %d %s", tipHeight, tipHash)

		if err = s.db.RollbackTip(dbtx, tipHash, tipHeight); err != nil {
			return nil, 0, err
		}
		if tipHash, tipHeight, err = s.db.LastProcessedBlock(dbtx); err != nil {
			return nil, 0, err
		}
		if chainHash, err = s.c.GetBlockHash(dbtx.Context(), tipHeight); err != nil {
			return nil, 0, err
		}
		rolledBack = true
	}

	if rolledBack {
		svrLog.Infof("Rolled back db chain to block %d %s", tipHeight, tipHash)
	}
	return &tipHash, tipHeight, nil
}

func (s *Server) handleBlockConnected(ctx context.Context, header *wire.BlockHeader) error {

	chainHeight := int64(header.Height)
	chainHash := header.BlockHash()

	svrLog.Debugf("Received connected block %s at height %d", chainHash, chainHeight)

	var tipHeight int64
	var tipHash *chainhash.Hash

	// Ensure our current tip matches the chain extended by the new block.
	err := s.db.Update(s.ctx, func(dbtx backenddb.WriteTx) error {
		var err error
		targetHash := &header.PrevBlock
		targetHeight := int64(header.Height - 1)
		tipHash, tipHeight, err = s.rollbackDbChain(dbtx, targetHash, targetHeight)
		return err

	})
	if err != nil {
		return err
	}

	// Fetch the full previous block.
	prev, err := s.getBlock(s.ctx, tipHash)
	if err != nil {
		return fmt.Errorf("Unable to fetch previous block %s of connected "+
			"block %s: %v", header.PrevBlock, chainHash, err)
	}

	// Now fetch all missing blocks from our tip
	for tipHeight < chainHeight {
		// Fetch the next missing block as a wire.MsgBlock. We special
		// case when the next block is the received connected block to
		// avoid another roundtrip to dcrd.
		var err error
		var nextTipHash *chainhash.Hash
		if tipHeight+1 == chainHeight {
			nextTipHash = &chainHash
		} else {
			if nextTipHash, err = s.c.GetBlockHash(ctx, tipHeight+1); err != nil {
				return err
			}
		}

		b, err := s.c.GetBlock(ctx, nextTipHash)
		if err != nil {
			return fmt.Errorf("Unable to fetch new connected block %s: %v",
				nextTipHash, err)
		}

		// Process the accounts modified by the block.
		err = s.preProcessAccountBlock(s.ctx, nextTipHash, b, prev, nil)
		if err != nil {
			return fmt.Errorf("Unable to process accounts of connected block "+
				"%s: %v", nextTipHash, err)
		}

		// Advance to next block.
		prev = b
		tipHeight++
		svrLog.Infof("Connected block %s at height %d", nextTipHash, tipHeight)
	}
	return nil
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
		return s.db.RollbackTip(dbtx, tipHash, int64(header.Height))
	})
	if err != nil {
		return err
	}

	svrLog.Infof("Disconnected block %s at height %d", blockHash, header.Height)
	return nil
}

func (s *Server) processSequentialBlocks(ctx context.Context, startHeight int64, f func(*chainhash.Hash, *wire.MsgBlock) error) error {
	concurrency := int64(s.concurrency)
	type gbbhReply struct {
		block *wire.MsgBlock
		hash  *chainhash.Hash
		err   error
	}
	chans := make([]chan gbbhReply, concurrency)
	gctx, cancel := context.WithCancel(ctx)
	for i := startHeight; i < startHeight+concurrency; i++ {
		c := make(chan gbbhReply)
		chans[i%concurrency] = c
		start := startHeight + ((i - startHeight) % concurrency)
		go func() {
			i := int64(0)
			for {
				var bl *wire.MsgBlock
				bh, err := s.c.GetBlockHash(gctx, start+i)
				if isErrRPCOutOfRange(err) {
					err = types.ErrBlockIndexAfterTip
				}
				if err == nil {
					bl, err = s.getBlock(gctx, bh)
				}
				select {
				case c <- gbbhReply{block: bl, hash: bh, err: err}:
				case <-gctx.Done():
					return
				}
				i += concurrency
			}
		}()
	}

	var err error
	for i := startHeight; err == nil; i++ {
		var next gbbhReply
		select {
		case next = <-chans[i%concurrency]:
			err = next.err
		case <-gctx.Done():
			err = gctx.Err()
		}
		if err == nil {
			err = f(next.hash, next.block)
		}
	}
	cancel()
	if err == types.ErrBlockIndexAfterTip {
		return nil
	}

	return err
}
