package backend

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	rserver "github.com/coinbase/rosetta-sdk-go/server"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/blockchain/stake/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrros/types"
	"golang.org/x/sync/errgroup"
)

var _ rserver.AccountAPIServicer = (*Server)(nil)

// calcBalance calculates the final balance of a given address given all
// transactions found by a searchRawTransactions rpc call. It assumes prevOut
// is true (that is, the results include previous output information).
//
// Due to including transactions mined in blocks that were eventually
// disapproved, we need to take care when processing to skip such txs and not
// include their balance-changing operations in the final balance.
//
// However, the semantics for the underlying blockchain is that any
// non-disapproved tx is automatically approved, thus if we're calculating a
// historical balance and the tx is included in the target block we also
// special-case any attempts to detect future disapproved blocks.
//
// The initial balance can be specified so that calls to searchrawtransactions
// can be batched.
func (s *Server) calcFinalBalance(ctx context.Context, addr dcrutil.Address, startBalance dcrutil.Amount, stopHeight int64, txs []*chainjson.SearchRawTransactionsResult) (dcrutil.Amount, error) {
	balance := startBalance

	saddr := addr.Address()

	// Helper.
	includes := func(haystack []string, needle string) bool {
		for _, s := range haystack {
			if s == needle {
				return true
			}
		}
		return false
	}

	// The slowest part of processing a batch of txs is fetching previous
	// blocks from the dcrd server, so we'll dedupe the list of blocks to
	// fetch and grab them concurrently to improve throughput.
	blockApprovalByHeight := make(map[int64]bool)
	g, gctx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	for _, txInfo := range txs {
		blockApprovalByHeight[txInfo.BlockHeight+1] = false
	}
	for height := range blockApprovalByHeight {
		height := height
		g.Go(func() error {
			// We can ignore the ErrBlockIndexAfterTip since it
			// just means we're attempting to calculate the balance
			// up to the current tip.
			_, bl, err := s.getBlockByHeight(gctx, height)
			mu.Lock()
			defer mu.Unlock()

			switch {
			case err == nil || errors.Is(err, types.ErrBlockIndexAfterTip):
				// Consider approved by default at tip.
				blockApprovalByHeight[height] = true
				return nil
			default:
				return err
			}

			blockApprovalByHeight[height] = types.VoteBitsApprovesParent(bl.Header.VoteBits)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return 0, err
	}

	svrLog.Tracef("%s balance modified by %d txs", saddr, len(txs))

	for _, txInfo := range txs {
		// Decode the tx to determine its type. We could probably skip
		// this if SearchRawTransactionsResult ever returned the tx
		// type itself and input/output amounts in ints instead of
		// floats.
		txBytes, err := hex.DecodeString(txInfo.Hex)
		if err != nil {
			return 0, types.ErrInvalidHexString
		}
		tx := new(wire.MsgTx)
		if err := tx.FromBytes(txBytes); err != nil {
			return 0, types.ErrInvalidTransaction
		}
		txType := stake.DetermineTxType(tx)

		// We'll use the corresponding inputs and outputs from the
		// decoded tx, so ensure they're sane.
		if len(tx.TxIn) != len(txInfo.Vin) {
			return 0, fmt.Errorf("incongruent number of TxIn and Vin")
		}
		if len(tx.TxOut) != len(txInfo.Vout) {
			return 0, fmt.Errorf("incongruent number of TxOut and Vout")
		}

		// Figure out whether this tx was disapproved because the block
		// after where this tx was mined disapproved its parent. We
		// only need to do this for regular txs since stake txs can't
		// be disapproved. We also skip this test when the tx is mined
		// in the last block we're interested in so that historical
		// balance queries reflect the correct amount at that height
		// independently of whether a future block would reverse the
		// tx.
		if txType == stake.TxTypeRegular && txInfo.BlockHeight < stopHeight {
			disapproved := !blockApprovalByHeight[txInfo.BlockHeight+1]
			if disapproved {
				// Skip this tx since it's known to be in a
				// disapproved block.
				svrLog.Tracef("%s skipping disapproved tx %s at block %d",
					saddr, txInfo.Txid, txInfo.BlockHeight)
				continue
			}
		}

		svrLog.Tracef("%s accounting for tx %s at block %d", saddr,
			txInfo.Txid, txInfo.BlockHeight)

		// Account for the effects of this tx in the balance.
		//
		// First, decrease the balance if there's an input spending
		// from this tx.
		for i, in := range txInfo.Vin {
			if in.Coinbase != "" || in.Stakebase != "" {
				// Skip coinbase/stakebase inputs since those
				// don't spend from an account(=address).
				continue
			}
			if in.PrevOut == nil {
				svrLog.Errorf("Prevout not included in vin for "+
					"tx %s at block %d", txInfo.Txid,
					txInfo.BlockHeight)
				return 0, fmt.Errorf("prevout not included in vin")
			}

			if !includes(in.PrevOut.Addresses, saddr) {
				continue
			}

			// Definitely spends the given address, so subtract the
			// amount spent.
			//
			// Since we're dealing with mined txs, it's safe to use
			// AmountIn.
			delta := dcrutil.Amount(tx.TxIn[i].ValueIn)
			balance -= delta
			svrLog.Tracef("%s debit input %d %d (curr=%s)", saddr,
				i, delta, balance)
		}

		// Now, increase the balance if there are outputs paying to
		// this address.
		for i, out := range txInfo.Vout {
			if !includes(out.ScriptPubKey.Addresses, saddr) {
				continue
			}

			delta := dcrutil.Amount(tx.TxOut[i].Value)
			balance += delta
			svrLog.Tracef("%s credit output %d %d (curr=%s)", saddr,
				i, delta, balance)
		}

	}
	return balance, nil
}

func (s *Server) AccountBalance(ctx context.Context, req *rtypes.AccountBalanceRequest) (*rtypes.AccountBalanceResponse, *rtypes.Error) {
	if req.AccountIdentifier == nil {
		// It doesn't make sense to return "all balances" of the
		// network.
		return nil, types.ErrInvalidArgument.RError()
	}

	// Decode the relevant account(=address).
	saddr := req.AccountIdentifier.Address
	addr, err := dcrutil.DecodeAddress(saddr, s.chainParams)
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

	// Process all txs affecting the given addr. Assumes addrindex is on.
	var skip int
	count := 100
	for {
		// Fetch a batch of transactions.
		txs, err := s.c.SearchRawTransactionsVerbose(ctx, addr, skip,
			count, true, false, nil)
		if err != nil && !strings.Contains(err.Error(), "No Txns available") {
			return nil, types.DcrdError(err)
		}

		// If we're performing a historical balance check, stop
		// processing txs after the target stopHeight. We also don't
		// process mempool txs.
		for i, tx := range txs {
			if tx.BlockHeight > stopHeight || tx.BlockHash == "" {
				txs = txs[:i]
				break
			}
		}

		// If there are no transactions, we're done.
		if len(txs) == 0 {
			break
		}

		// Account for these transactions.
		balance, err = s.calcFinalBalance(ctx, addr, balance, stopHeight, txs)
		if err != nil {
			return nil, types.DcrdError(err)
		}

		// Fetch the next batch of txs.
		skip += count
		if skip%2000 == 0 {
			svrLog.Infof("%s processed %d txs so far (height=%d stopHeight=%d)",
				saddr, skip, txs[len(txs)-1].BlockHeight,
				stopHeight)
		}
	}

	return &rtypes.AccountBalanceResponse{
		Balances: []*rtypes.Amount{
			types.DcrAmountToRosetta(balance),
		},
		BlockIdentifier: &rtypes.BlockIdentifier{
			Hash:  stopHash.String(),
			Index: stopHeight,
		},
	}, nil

}
