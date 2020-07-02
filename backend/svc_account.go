package backend

import (
	"context"
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

func (s *Server) preProcessAccountBlock(ctx context.Context, bh *chainhash.Hash, b, prev *wire.MsgBlock, utxoSet map[wire.OutPoint]*types.PrevInput) error {
	fetchInputs := s.makeInputsFetcher(ctx, utxoSet)

	height := int64(b.Header.Height)
	newBalances := make(map[string]dcrutil.Amount)

	return s.db.Update(ctx, func(dbtx backenddb.WriteTx) error {
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

		err := types.IterateBlockOps(b, prev, fetchInputs, applyOp, s.chainParams)
		if err != nil {
			return err
		}

		// Update the db with the new balances.
		return s.db.StoreBalances(dbtx, *bh, height, newBalances)
	})
}

// preProcessAccounts pre-processes the blockchain to setup the account
// balances index in the server's badger db.
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

	// Verify if the last processed block matches the block in the
	// mainchain at startHeight. If it doesn't, we'll have to roll back due
	// to a reorg that happened while we were offline.
	hash, err := s.c.GetBlockHash(ctx, startHeight)
	if err != nil {
		return err
	}
	if *hash != startHash && startHeight > 0 {
		svrLog.Warnf("Last processed block %s does not match current "+
			"mainchain block %s at height %d. Rolling back.",
			startHash, hash, startHeight)

		err := s.db.Update(s.ctx, func(dbtx backenddb.WriteTx) error {
			var err error
			hash, startHeight, err = s.rollbackDbChain(dbtx, hash, startHeight)
			return err
		})
		if err != nil {
			return err
		}
	}

	// Fetch the block prior to starting to process the chain.
	prev, err := s.getBlock(ctx, hash)
	if err != nil {
		return err
	}

	// Use a utxoSet map to speed up sequential processing when traversing
	// many blocks (useful during initial startup).
	utxoSet := make(map[wire.OutPoint]*types.PrevInput)

	// Sequentially process the chain.
	svrLog.Infof("Pre-processing accounts in blocks starting at %d", startHeight)
	var lastHeight int64
	err = s.processSequentialBlocks(ctx, startHeight, func(bh *chainhash.Hash, b *wire.MsgBlock) error {
		err := s.preProcessAccountBlock(ctx, bh, b, prev, utxoSet)
		if err != nil {
			return err
		}

		lastHeight = int64(b.Header.Height)
		if lastHeight%2000 == 0 {
			svrLog.Infof("Processed up to height %d", lastHeight)
		}

		prev = b
		return err
	})
	if err != nil {
		svrLog.Warnf("Errored processing at height %d: %v", lastHeight, err)
		return err
	}

	totalTime := time.Now().Sub(start)
	svrLog.Infof("Processed all blocks in %s. Last one was %d", totalTime, lastHeight)
	svrLog.Debugf("Final utxo set len %d", len(utxoSet))
	return nil
}

func (s *Server) AccountBalance(ctx context.Context, req *rtypes.AccountBalanceRequest) (*rtypes.AccountBalanceResponse, *rtypes.Error) {
	if req.AccountIdentifier == nil {
		// It doesn't make sense to return "all balances" of the
		// network.
		return nil, types.ErrInvalidArgument.RError()
	}

	// Decode the relevant account(=address).
	saddr := req.AccountIdentifier.Address
	_, err := dcrutil.DecodeAddress(saddr, s.chainParams)
	if err != nil {
		return nil, types.ErrInvalidAccountIdAddr.RError()
	}

	// Figure out when to stop considering blocks (what the target height
	// for balance was requested for by the client). By default it's the
	// current block height.
	stopHash, stopHeight, _, err := s.getBlockByPartialId(ctx, req.BlockIdentifier)
	if err != nil {
		return nil, types.DcrdError(err)
	}

	// Track the balance across batches of txs.
	var balance dcrutil.Amount

	err = s.db.View(ctx, func(dbtx backenddb.ReadTx) error {
		var err error
		balance, err = s.db.Balance(dbtx, saddr, stopHeight)
		return err
	})
	if err != nil {
		return nil, types.DcrdError(err)
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
