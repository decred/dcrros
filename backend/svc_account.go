// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"
	"fmt"
	"time"

	"decred.org/dcrros/backend/backenddb"
	"decred.org/dcrros/types"
	rserver "github.com/coinbase/rosetta-sdk-go/server"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/wire"
)

var _ rserver.AccountAPIServicer = (*Server)(nil)

func updateUtxoSet(op *types.Op, utxoSet map[wire.OutPoint]*types.PrevInput) {
	if utxoSet == nil {
		return
	}

	typ := op.Type
	st := op.Status
	switch {
	case typ == types.OpTypeDebit && st == types.OpStatusSuccess:
		// Successful input removes entry from utxo set.
		delete(utxoSet, op.In.PreviousOutPoint)

	case typ == types.OpTypeCredit && st == types.OpStatusSuccess:
		// Successful output adds entry to utxo set.
		outp := wire.OutPoint{
			Hash:  op.Tx.TxHash(),
			Index: uint32(op.IOIndex),
			Tree:  op.Tree,
		}
		utxoSet[outp] = &types.PrevInput{
			Amount:   op.Amount,
			PkScript: op.Out.PkScript,
			Version:  op.Out.Version,
		}

	case typ == types.OpTypeDebit && st == types.OpStatusReversed:
		// Reversed input returns entry to the utxo set.
		utxoSet[op.In.PreviousOutPoint] = op.PrevInput

	case typ == types.OpTypeCredit && st == types.OpStatusReversed:
		// Reversed output removes entry from the utxo set.
		outp := wire.OutPoint{
			Hash:  op.Tx.TxHash(),
			Index: uint32(op.IOIndex),
			Tree:  op.Tree,
		}
		delete(utxoSet, outp)

	}
}

// lastProcessedBlock returns the current tip of the server database. This is a
// convenience function, since it's usually preferable (and safer) to call the
// db function directly from inside a dbtx if additional operations will be
// performed.
func (s *Server) lastProcessedBlock(ctx context.Context) (*chainhash.Hash, int64, error) {
	var (
		tipHash   chainhash.Hash
		tipHeight int64
	)

	err := s.db.View(ctx, func(dbtx backenddb.ReadTx) error {
		var err error
		tipHash, tipHeight, err = s.db.LastProcessedBlock(dbtx)
		return err
	})
	return &tipHash, tipHeight, err
}

func (s *Server) preProcessAccountBlock(ctx context.Context, bh *chainhash.Hash, b, prev *wire.MsgBlock, utxoSet map[wire.OutPoint]*types.PrevInput) error {
	fetchInputs := s.makeInputsFetcher(ctx, utxoSet)

	height := int64(b.Header.Height)
	newBalances := make(map[string]dcrutil.Amount)

	return s.db.Update(ctx, func(dbtx backenddb.WriteTx) error {
		// Ensure we won't store blocks out of order.
		tipHash, tipHeight, err := s.db.LastProcessedBlock(dbtx)
		if err != nil {
			return err
		}
		prevHash := b.Header.PrevBlock
		prevHeight := int64(b.Header.Height) - 1

		// Special case genesis.
		if (tipHash == chainhash.Hash{}) && (tipHeight == 0) {
			tipHash = s.chainParams.GenesisHash
		}

		if tipHeight != prevHeight || tipHash != b.Header.PrevBlock {
			return fmt.Errorf("attempting to process block that "+
				"does not extend tip. tip hash=%s height=%d, "+
				"prev hash=%s height=%d", tipHash, tipHeight,
				prevHash, prevHeight)
		}

		applyOp := func(op *types.Op) error {
			account := op.Account
			if _, ok := newBalances[account]; !ok {
				// First time on this block we're modifying
				// this account, so fetch the current balance
				// from the db.
				lastBal, err := s.db.Balance(dbtx, account, height-1)
				if err != nil {
					return err
				}
				newBalances[account] = lastBal
			}

			// All dcrros status are currently successful (i.e.
			// affect the balance) so the following is safe without
			// checking for the specific status.
			newBalances[account] += op.Amount

			// Modify the utxo set according to this op so
			// fetchInputs can be implemented without requiring a
			// network call back to dcrd.
			updateUtxoSet(op, utxoSet)

			return nil
		}

		err = types.IterateBlockOps(b, prev, fetchInputs, applyOp, s.chainParams)
		if err != nil {
			return err
		}

		s.bcl.inc(height, bh)

		// Update the db with the new balances.
		return s.db.StoreBalances(dbtx, *bh, height, newBalances)
	})
}

// preProcessAccounts pre-processes the blockchain to setup the account
// balances index in the server's db.
//
// This is called during server startup.
func (s *Server) preProcessAccounts(ctx context.Context) error {
	start := time.Now()

	var startHeight int64
	var startHash chainhash.Hash

	err := s.db.View(ctx, func(dbtx backenddb.ReadTx) error {
		var err error
		startHash, startHeight, err = s.db.LastProcessedBlock(dbtx)
		return err
	})
	if err != nil {
		return err
	}

	// Ensure we're connected to a suitable dcrd instance.
	if err := s.isDcrdActive(); err != nil {
		return err
	}

	// Verify if the last processed block matches the block in the mainchain
	// at startHeight. If it doesn't, we'll have to roll back due to a reorg
	// that happened while we were offline.
	hash, err := s.c.GetBlockHash(ctx, startHeight)
	if err != nil {
		return fmt.Errorf("unable to fetch block hash at height %d: %v",
			startHeight, err)

	}
	if *hash != startHash && startHeight > 0 {
		svrLog.Debugf("Last processed block %s does not match current "+
			"mainchain block %s at height %d. Rolling back.",
			startHash, hash, startHeight)

		b, err := s.getBlock(ctx, hash)
		if err != nil {
			return err
		}
		err = s.switchChainTo(ctx, hash, b, nil)
		if err != nil {
			return err
		}
	}

	// Update the current sync status.
	chainInfo, err := s.c.GetBlockChainInfo(ctx)
	if err != nil {
		return err
	}
	targetIndex := chainInfo.Blocks
	s.mtx.Lock()
	s.syncStatus.Stage = &syncStatusStageProcessingAccounts
	s.syncStatus.CurrentIndex = startHeight
	s.syncStatus.TargetIndex = &targetIndex
	s.mtx.Unlock()

	// We'll start processing at the next block height.
	startHeight++

	// Use a utxoSet map to speed up sequential processing when traversing
	// many blocks (useful during initial startup).
	//
	// Note this isn't a true utxoset because it doesn't handle reorgs, but
	// since outputs aren't malleable and inexistent entries are looked for
	// in the blockchain, this is safe to use as is.
	utxoSet := make(map[wire.OutPoint]*types.PrevInput)

	// Sequentially process the chain.
	svrLog.Infof("Pre-processing accounts in blocks starting at %d", startHeight)
	var lastHeight int64
	err = s.processSequentialBlocks(ctx, startHeight, func(bh *chainhash.Hash, b *wire.MsgBlock) error {
		err := s.switchChainTo(ctx, bh, b, utxoSet)
		if err != nil {
			return err
		}

		// Update the current sync status.
		s.mtx.Lock()
		s.syncStatus.CurrentIndex = int64(b.Header.Height)
		s.mtx.Unlock()

		lastHeight = int64(b.Header.Height)
		return err
	})
	if err != nil {
		svrLog.Warnf("Errored processing at height %d: %v", lastHeight, err)
		return err
	}

	s.bcl.flush()
	totalTime := time.Since(start)
	if lastHeight > 0 {
		svrLog.Infof("Processed all blocks in %s. Last one was %d", totalTime, lastHeight)
		svrLog.Debugf("Final utxo set len %d", len(utxoSet))
	} else {
		svrLog.Info("No blocks processed (already at tip)")
	}

	return nil
}

func (s *Server) AccountBalance(ctx context.Context, req *rtypes.AccountBalanceRequest) (*rtypes.AccountBalanceResponse, *rtypes.Error) {
	if req.AccountIdentifier == nil {
		// It doesn't make sense to return "all balances" of the
		// network.
		return nil, types.ErrInvalidArgument.RError()
	}

	saddr := req.AccountIdentifier.Address
	if saddr != types.TreasuryAccountAdddress {
		// Validate non-treasury addresses.
		err := types.CheckRosettaAccount(req.AccountIdentifier, s.chainParams)
		if err != nil {
			return nil, types.ErrInvalidAccountIdAddr.Msg(err.Error()).RError()
		}
	}

	// Figure out the target block for the balance check requested by the
	// client. By default it's the current tip.
	stopHash, stopHeight, _, err := s.getBlockByPartialId(ctx, req.BlockIdentifier)
	if err != nil {
		return nil, types.RError(err)
	}

	// Fetch the cached balance at that specified height.
	var balance dcrutil.Amount
	err = s.db.View(ctx, func(dbtx backenddb.ReadTx) error {
		var err error
		balance, err = s.db.Balance(dbtx, saddr, stopHeight)
		return err
	})
	if err != nil {
		return nil, types.RError(err)
	}

	res := &rtypes.AccountBalanceResponse{
		Balances: []*rtypes.Amount{
			types.DcrAmountToRosetta(balance),
		},
		BlockIdentifier: &rtypes.BlockIdentifier{
			Hash:  stopHash.String(),
			Index: stopHeight,
		},
	}

	return res, nil
}

// AccountCoins returns the coins (i.e. utxos) belonging to a given account.
func (s *Server) AccountCoins(ctx context.Context, req *rtypes.AccountCoinsRequest) (*rtypes.AccountCoinsResponse, *rtypes.Error) {
	return nil, types.ErrUnimplemented.RError()
}
