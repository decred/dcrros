package types

import (
	"errors"
	"fmt"
	"strconv"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/blockchain/stake/v3"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
)

const (
	// outputIndexFlag is the starting index of outputs when specified in
	// an OperationIdentifier structure. This allows inputs and outputs to
	// carry their original dcr transaction index while still maintaining
	// the uniquiness invariant required by rosetta for that field.
	OutputIndexFlag = int64(1 << 32)
)

var (
	ErrNeedsPreviousBlock = errors.New("previous block required")

	CurrencySymbol = &rtypes.Currency{
		Symbol:   "DCR",
		Decimals: 8,
	}
)

func dcrAmountToRosetta(amt dcrutil.Amount) *rtypes.Amount {
	return &rtypes.Amount{
		Value:    strconv.FormatInt(int64(amt), 10),
		Currency: CurrencySymbol,
	}
}

func dcrPkScriptToAccount(version uint16, pkScript []byte, chainParams *chaincfg.Params) (*rtypes.AccountIdentifier, error) {
	_, addrs, _, err := txscript.ExtractPkScriptAddrs(version, pkScript, chainParams)
	if err != nil {
		return nil, err
	}

	if len(addrs) != 1 {
		// TODO: support 'bare' (non-p2sh) multisig?
		return nil, nil
	}

	return &rtypes.AccountIdentifier{
		Address: addrs[0].Address(),
	}, nil
}

type PrevInput struct {
	PkScript []byte
	Version  uint16
	Amount   dcrutil.Amount
}

type PrevInputsFetcher func(...*wire.OutPoint) (map[wire.OutPoint]*PrevInput, error)

func wireBlockTxToRosetta(txidx int, tx *wire.MsgTx, reversed bool, fetchInputs PrevInputsFetcher, chainParams *chaincfg.Params) (*rtypes.Transaction, error) {
	txType := stake.DetermineTxType(tx)
	isVote := txType == stake.TxTypeSSGen
	isCoinbase := txType == stake.TxTypeRegular && txidx == 0

	// Fetch the relevant data for the inputs.
	prevOutpoints := make([]*wire.OutPoint, 0, len(tx.TxIn))
	for i, in := range tx.TxIn {
		if i == 0 && (isVote || isCoinbase) {
			// Coinbases don't have an input with i > 0.
			continue
		}

		prevOutpoints = append(prevOutpoints, &in.PreviousOutPoint)
	}
	prevInputs, err := fetchInputs(prevOutpoints...)
	if err != nil {
		return nil, err
	}

	ops := make([]*rtypes.Operation, 0, len(tx.TxIn)+len(tx.TxOut))
	addTxIns := func() error {
		for i, in := range tx.TxIn {
			if i == 0 && (isVote || isCoinbase) {
				// Coinbases don't have an input with i > 0.
				continue
			}

			prevInput, ok := prevInputs[in.PreviousOutPoint]
			if !ok {
				return fmt.Errorf("missing prev outpoint %s", in.PreviousOutPoint)
			}

			account, err := dcrPkScriptToAccount(prevInput.Version,
				prevInput.PkScript, chainParams)
			if err != nil {
				return err
			}
			if account == nil {
				// Might happen for OP_RETURNs, ticket
				// commitments, etc.
				continue
			}

			amt := -prevInput.Amount
			status := OpStatusSuccess
			if reversed {
				amt *= -1
				status = OpStatusReversed
			}

			op := &rtypes.Operation{
				OperationIdentifier: &rtypes.OperationIdentifier{
					Index: int64(i),
				},
				Type:    OpTypeDebit.String(),
				Status:  status.String(),
				Account: account,
				Amount:  dcrAmountToRosetta(amt),
				Metadata: map[string]interface{}{
					"prev_hash":        in.PreviousOutPoint.Hash.String(),
					"prev_index":       in.PreviousOutPoint.Index,
					"prev_tree":        in.PreviousOutPoint.Tree,
					"sequence":         in.Sequence,
					"block_height":     in.BlockHeight,
					"block_index":      in.BlockIndex,
					"signature_script": in.SignatureScript,
					"script_version":   prevInput.Version,
				},
			}

			ops = append(ops, op)
		}

		return nil
	}

	addTxOuts := func() error {
		for i, out := range tx.TxOut {
			if out.Value == 0 {
				// Ignore OP_RETURNs and other zero-valued
				// outputs.
				//
				// TODO: decode ticket commitments?
				continue
			}

			account, err := dcrPkScriptToAccount(out.Version,
				out.PkScript, chainParams)
			if err != nil {
				return err
			}
			if account == nil {
				continue
			}

			amt := dcrutil.Amount(out.Value)
			status := OpStatusSuccess
			if reversed {
				amt *= -1
				status = OpStatusReversed
			}

			opIdx := int64(i) | OutputIndexFlag
			op := &rtypes.Operation{
				OperationIdentifier: &rtypes.OperationIdentifier{
					Index: opIdx,
				},
				Type:    OpTypeCredit.String(),
				Status:  status.String(),
				Account: account,
				Amount:  dcrAmountToRosetta(amt),
				Metadata: map[string]interface{}{
					"script_version": out.Version,
				},
			}

			ops = append(ops, op)
		}

		return nil
	}

	if !reversed {
		if err := addTxIns(); err != nil {
			return nil, err
		}
		if err := addTxOuts(); err != nil {
			return nil, err
		}
	} else {
		// When reversing a tx we apply the update in the opposite
		// order: first roll back outputs (which were crediting an
		// amount) then inputs (which were debiting the amount + fee).
		if err := addTxOuts(); err != nil {
			return nil, err
		}
		if err := addTxIns(); err != nil {
			return nil, err
		}
	}

	r := &rtypes.Transaction{
		TransactionIdentifier: &rtypes.TransactionIdentifier{
			Hash: tx.TxHash().String(),
		},
		Operations: ops,
		Metadata: map[string]interface{}{
			"version":  tx.Version,
			"expiry":   tx.Expiry,
			"locktime": tx.LockTime,
		},
	}
	return r, nil
}

// WireBlockToRosetta converts the given block in wire representation to the
// block in rosetta representation. The previous block is needed when the
// current block disapproved the regular transactions of the previous one, in
// which case it must be specified or this function errors.
func WireBlockToRosetta(b, prev *wire.MsgBlock, fetchInputs PrevInputsFetcher, chainParams *chaincfg.Params) (*rtypes.Block, error) {

	approvesParent := b.Header.VoteBits&0x01 == 0x01 || b.Header.Height == 0
	if !approvesParent && prev == nil {
		return nil, ErrNeedsPreviousBlock
	}

	var txs []*rtypes.Transaction
	nbTxs := len(b.Transactions) + len(b.STransactions)
	if !approvesParent {
		nbTxs += len(prev.Transactions) + len(prev.STransactions)
	}
	txs = make([]*rtypes.Transaction, 0, nbTxs)

	if !approvesParent {
		// Reverse regular transactions of the previous block.
		for i, tx := range prev.Transactions {
			rtx, err := wireBlockTxToRosetta(i, tx, true, fetchInputs, chainParams)
			if err != nil {
				return nil, err
			}

			txs = append(txs, rtx)
		}
	}

	for i, tx := range b.Transactions {
		rtx, err := wireBlockTxToRosetta(i, tx, false, fetchInputs, chainParams)
		if err != nil {
			return nil, err
		}

		txs = append(txs, rtx)
	}
	for i, tx := range b.STransactions {
		rtx, err := wireBlockTxToRosetta(i, tx, false, fetchInputs, chainParams)
		if err != nil {
			return nil, err
		}

		txs = append(txs, rtx)
	}

	blockHash := b.Header.BlockHash()
	prevHeight := b.Header.Height - 1
	prevHash := b.Header.PrevBlock
	if b.Header.Height == 0 {
		// https://www.rosetta-api.org/docs/common_mistakes.html#malformed-genesis-block
		// currently (2020-05-24) recommends returning the same
		// identifier on both BlockIdentifier and ParentBlockIdentifier
		// on the genesis block.
		prevHeight = 0
		prevHash = blockHash
	}

	r := &rtypes.Block{
		BlockIdentifier: &rtypes.BlockIdentifier{
			Index: int64(b.Header.Height),
			Hash:  blockHash.String(),
		},
		ParentBlockIdentifier: &rtypes.BlockIdentifier{
			Index: int64(prevHeight),
			Hash:  prevHash.String(),
		},
		Timestamp:    b.Header.Timestamp.Unix() * 1000,
		Transactions: txs,
		Metadata: map[string]interface{}{
			"block_version":   b.Header.Version,
			"merkle_root":     b.Header.MerkleRoot.String(),
			"stake_root":      b.Header.StakeRoot.String(),
			"approves_parent": approvesParent,
			"vote_bits":       b.Header.VoteBits,
			"bits":            b.Header.Bits,
			"sbits":           b.Header.SBits,
		},
	}
	return r, nil
}

// MempoolTxToRosetta converts a wire tx that is known to be on the mempool to
// a rosetta tx.
func MempoolTxToRosetta(tx *wire.MsgTx, fetchInputs PrevInputsFetcher, chainParams *chaincfg.Params) (*rtypes.Transaction, error) {
	// Coinbase txs are never seen on the mempool so it's safe to use a
	// negative txidx.
	return wireBlockTxToRosetta(-1, tx, false, fetchInputs, chainParams)
}
