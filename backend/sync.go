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
// It returns the chainhash and height for the new tip. It also returns a list
// of block hashes between the new (rolled back) tip and the targetHash block
// (including targetHash itself) in reverse block order (newer blocks first).
func (s *Server) rollbackDbChain(dbtx backenddb.WriteTx,
	targetHash *chainhash.Hash, targetHeight int64) (
	*chainhash.Hash, int64, []*chainhash.Hash, error) {

	// Fetch the current DB (i.e. processed) tip.
	tipHash, tipHeight, err := s.db.LastProcessedBlock(dbtx)
	if err != nil {
		return nil, 0, nil, err
	}

	// Special case when we don't have anything to do.
	if targetHeight == tipHeight && targetHash.IsEqual(&tipHash) {
		return &tipHash, tipHeight, nil, nil
	}

	// If the target is lower than the current tip, it's likely we're in
	// the middle of a reorg. Roll back until we find the block in our
	// chain that is at the same height as the one in the target chain.
	for tipHeight > 0 && tipHeight > targetHeight {
		var err error

		svrLog.Debugf("Rolling back rewinded tip %d %s", tipHeight, tipHash)
		if err = s.db.RollbackTip(dbtx, tipHash, tipHeight); err != nil {
			return nil, 0, nil, err
		}
		if tipHash, tipHeight, err = s.db.LastProcessedBlock(dbtx); err != nil {
			return nil, 0, nil, err
		}
	}

	// Keep track of missing blocks that will need to be processed.
	var missingBlocks []*chainhash.Hash

	// If the target is higher than the current tip, we missed some blocks.
	// Follow back the chain that ends in targetHash until we reach the
	// same height as the current db tip.
	for targetHeight > 0 && targetHeight > tipHeight {
		missingBlocks = append(missingBlocks, targetHash)

		// Fetch the targetHash block so we can discover the hash of
		// its parent.
		b, err := s.getBlock(dbtx.Context(), targetHash)
		if err != nil {
			return nil, 0, nil, err
		}

		targetHash = &b.Header.PrevBlock
		targetHeight--
	}

	// Now roll back until we find a block shared by both our db and the
	// chain which tip is the targetHash at this moment.
	for tipHeight > 0 && !targetHash.IsEqual(&tipHash) {
		missingBlocks = append(missingBlocks, targetHash)

		var err error
		svrLog.Debugf("Rolling back reorged tip %d %s", tipHeight, tipHash)

		// Rollback the DB tip and find out the new tip.
		if err = s.db.RollbackTip(dbtx, tipHash, tipHeight); err != nil {
			return nil, 0, nil, err
		}
		if tipHash, tipHeight, err = s.db.LastProcessedBlock(dbtx); err != nil {
			return nil, 0, nil, err
		}

		// Find out the parent block hash.
		b, err := s.getBlock(dbtx.Context(), targetHash)
		if err != nil {
			return nil, 0, nil, err
		}
		targetHash = &b.Header.PrevBlock
	}

	if len(missingBlocks) > 0 {
		svrLog.Infof("Rolled back db chain to block %d %s", tipHeight, tipHash)
	}
	return &tipHash, tipHeight, missingBlocks, nil
}

// reorgToParentOf performs a chain rollback (if necessary) and processes
// blocks until the last processed block matches the parent of the specified
// block.
//
// It returns the last processed block.
func (s *Server) reorgToParentOf(ctx context.Context, bh *chainhash.Hash,
	block *wire.MsgBlock) (*wire.MsgBlock, error) {

	var tipHeight int64
	var tipHash *chainhash.Hash
	var missingBlocks []*chainhash.Hash

	// Rollback until we find a common point between the specified block's
	// parent and the db chain.
	err := s.db.Update(ctx, func(dbtx backenddb.WriteTx) error {
		var err error
		targetHash := &block.Header.PrevBlock
		targetHeight := int64(block.Header.Height - 1)
		tipHash, tipHeight, missingBlocks, err = s.rollbackDbChain(dbtx, targetHash, targetHeight)
		return err
	})
	if err != nil {
		return nil, err
	}

	var prev *wire.MsgBlock

	// Fetch the current tip block.
	if tipHeight > 0 {
		prev, err = s.getBlock(ctx, tipHash)
		if err != nil {
			return nil, fmt.Errorf("unable to fetch tip block %s at height "+
				"%d", tipHash, tipHeight)
		}
	}

	// Process any missing blocks between the current tip and the parent of
	// the passed block.
	//
	// missingBlocks is in reverse block order (newer blocks first).
	for i := len(missingBlocks) - 1; i >= 0; i-- {
		nextTipHash := missingBlocks[i]
		b, err := s.getBlock(ctx, nextTipHash)
		if err != nil {
			return nil, fmt.Errorf("unable to fetch missing block %s: %v",
				nextTipHash, err)
		}

		// Process the accounts modified by the block.
		err = s.preProcessAccountBlock(ctx, nextTipHash, b, prev, nil)
		if err != nil {
			return nil, fmt.Errorf("unable to process accounts of connected block "+
				"%s: %v", nextTipHash, err)
		}

		// Advance to next block.
		prev = b
		tipHeight++
		svrLog.Infof("Connected missing block %s at height %d", nextTipHash, tipHeight)
	}

	return prev, nil
}

// switchChainTo switches the DB chain to the specified block, handling reorgs
// as necessary.
func (s *Server) switchChainTo(ctx context.Context, bh *chainhash.Hash,
	b *wire.MsgBlock, utxoSet map[wire.OutPoint]*types.PrevInput) error {

	// Reorg (if needed) to the parent of the passed block.
	prev, err := s.reorgToParentOf(ctx, bh, b)
	if err != nil {
		return fmt.Errorf("unable to reorg to parent of %s (height %d): "+
			"%v", bh, b.Header.Height, err)
	}

	// Process the accounts modified by the block.
	err = s.preProcessAccountBlock(ctx, bh, b, prev, utxoSet)
	if err != nil {
		return fmt.Errorf("unable to process accounts of connected block "+
			"%s (height %d): %v", bh, b.Header.Height, err)
	}

	svrLog.Debugf("Connected block %s at height %d", bh, b.Header.Height)

	return nil
}

// handleBlockConnected is called when new blocks are connected to the
// blockchain.
//
// This is called from the main Run goroutine.
func (s *Server) handleBlockConnected(ctx context.Context, header *wire.BlockHeader) error {
	chainHeight := int64(header.Height)
	chainHash := header.BlockHash()

	svrLog.Debugf("Received connected block %s at height %d", chainHash, chainHeight)

	// If we received a block connected notification for a height lower
	// than we currently are, ignore it. It's likely we're in the middle of
	// a reorg, so we'll switch to the new chain once a longer one is
	// received.
	_, tipHeight, err := s.lastProcessedBlock(ctx)
	if err != nil {
		return err
	}
	if chainHeight < tipHeight {
		svrLog.Debugf("Ignoring block connected %s at height lower than "+
			"current tip (%d < %d)", chainHash, chainHeight, tipHeight)
		return nil
	}

	// Fetch the full block.
	b, err := s.getBlock(ctx, &chainHash)
	if err != nil {
		return fmt.Errorf("unable to fetch dcrd connected block %s: %v",
			chainHash, err)
	}

	return s.switchChainTo(ctx, &chainHash, b, nil)
}

func (s *Server) handleBlockDisconnected(ctx context.Context, header *wire.BlockHeader) error {
	blockHash := header.BlockHash()
	err := s.db.Update(ctx, func(dbtx backenddb.WriteTx) error {
		// Ensure our current tip matches the chain rolled back by the
		// disconnected block.
		tipHash, tipHeight, err := s.db.LastProcessedBlock(dbtx)
		if err != nil {
			return err
		}

		if tipHash != blockHash || tipHeight != int64(header.Height) {
			// In this case we'll just warn that we received an
			// invalid disconnection and wait for the next
			// connected block to handle any potential reorg.
			svrLog.Warnf("Current tip %d (%s) does not match "+
				"disconnected block %s",
				tipHeight, tipHash, blockHash)
			return nil
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
