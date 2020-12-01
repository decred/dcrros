// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package types

import (
	"errors"
	"fmt"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/blockchain/stake/v3"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/wire"
)

var (
	// ErrNeedsPreviousBlock indicates the previous block is needed but was
	// not provided.
	ErrNeedsPreviousBlock = errors.New("previous block required")
)

// VoteBitsApprovesParent returns true if the provided voteBits as included in
// some block header flags the parent block as approved according to current
// consensus rules.
func VoteBitsApprovesParent(voteBits uint16) bool {
	return voteBits&0x01 == 0x01
}

// PrevInput is the output information needed for a given transaction input.
type PrevInput struct {
	PkScript []byte
	Version  uint16
	Amount   dcrutil.Amount
}

// PrevInputsFetcher is used to fetch previous inputs from transactions.
type PrevInputsFetcher func(...*wire.OutPoint) (map[wire.OutPoint]*PrevInput, error)

type Op struct {
	Tree           int8
	Status         OpStatus
	Tx             *wire.MsgTx
	TxHash         chainhash.Hash
	TxIndex        int
	IOIndex        int
	Account        string
	AccountVersion uint16
	Type           OpType
	OpIndex        int64
	Amount         dcrutil.Amount
	In             *wire.TxIn
	Out            *wire.TxOut
	PrevInput      *PrevInput
}

func (op *Op) ROp() *rtypes.Operation {
	account := &rtypes.AccountIdentifier{
		Address: op.Account,
		Metadata: map[string]interface{}{
			"script_version": op.AccountVersion,
		},
	}
	var meta map[string]interface{}
	var coinChange *rtypes.CoinChange
	if op.Type == OpTypeDebit {
		meta = map[string]interface{}{
			"input_index":      op.IOIndex,
			"prev_hash":        op.In.PreviousOutPoint.Hash.String(),
			"prev_index":       op.In.PreviousOutPoint.Index,
			"prev_tree":        op.In.PreviousOutPoint.Tree,
			"sequence":         op.In.Sequence,
			"block_height":     op.In.BlockHeight,
			"block_index":      op.In.BlockIndex,
			"signature_script": op.In.SignatureScript,
		}

		coinAction := rtypes.CoinSpent
		if op.Status == OpStatusReversed {
			coinAction = rtypes.CoinCreated
		}

		if op.Account != TreasuryAccountAdddress {
			coinChange = &rtypes.CoinChange{
				CoinAction: coinAction,
				CoinIdentifier: &rtypes.CoinIdentifier{
					Identifier: op.In.PreviousOutPoint.String(),
				},
			}
		}
	} else {
		meta = map[string]interface{}{
			"output_index": op.IOIndex,
		}

		// TODO: support tadd[0] output (doesn't create coins).
		outp := wire.OutPoint{
			Hash:  op.TxHash,
			Index: uint32(op.IOIndex),
			Tree:  op.Tree,
		}

		coinAction := rtypes.CoinCreated
		if op.Status == OpStatusReversed {
			coinAction = rtypes.CoinSpent
		}

		if op.Account != TreasuryAccountAdddress {
			coinChange = &rtypes.CoinChange{
				CoinAction: coinAction,
				CoinIdentifier: &rtypes.CoinIdentifier{
					Identifier: outp.String(),
				},
			}
		}
	}

	return &rtypes.Operation{
		OperationIdentifier: &rtypes.OperationIdentifier{
			Index: op.OpIndex,
		},
		Type:       op.Type.RType(),
		Status:     op.Status.Status(),
		Account:    account,
		Amount:     DcrAmountToRosetta(op.Amount),
		Metadata:   meta,
		CoinChange: coinChange,
	}

}

type BlockOpCb = func(op *Op) error

// iterateBlockOpsInTx iterates over all Rosetta-reprenstable operations in the
// given transaction.
//
// The provided op must have been previously filled with the following
// information:
// - Tree
// - Tx
// - TxHash
// - TxIndex
// - OpIndex
// - Status
//
// The same op is reused across all calls to the callback.
func iterateBlockOpsInTx(op *Op, fetchInputs PrevInputsFetcher, applyOp BlockOpCb,
	chainParams *chaincfg.Params) error {

	tx := op.Tx
	isStake := op.Tree == wire.TxTreeStake
	txType := stake.DetermineTxType(tx, true)
	isVote := txType == stake.TxTypeSSGen
	isCoinbase := !isStake && txType == stake.TxTypeRegular && op.TxIndex == 0

	isTBase := isStake && txType == stake.TxTypeTreasuryBase && op.TxIndex == 0
	isTAdd := isStake && txType == stake.TxTypeTAdd
	isTSpend := isStake && txType == stake.TxTypeTSpend

	// Fetch the relevant data for the inputs.
	prevOutpoints := make([]*wire.OutPoint, 0, len(tx.TxIn))
	for i, in := range tx.TxIn {
		if i == 0 && (isVote || isCoinbase || isTBase || isTSpend) {
			// Coinbases don't have an input with i > 0 so this is
			// safe.
			continue
		}
		prevOutpoints = append(prevOutpoints, &in.PreviousOutPoint)
	}
	prevInputs, err := fetchInputs(prevOutpoints...)
	if err != nil {
		return err
	}

	var ok bool

	// Helper to process the inputs.
	addTxIns := func() error {
		// Reset op's output attributes.
		op.Out = nil

		for i, in := range tx.TxIn {
			if i == 0 && (isVote || isCoinbase || isTBase) {
				// Coinbases don't have an input with i > 0.
				continue
			}

			if isTSpend {
				// For tspends, the first input debits from the
				// treasury account and there's no PrevInput.
				op.PrevInput = nil
				op.AccountVersion = 0
				op.Account = TreasuryAccountAdddress
				op.Amount = -dcrutil.Amount(in.ValueIn)
			} else {
				// Handle standard inputs.
				op.PrevInput, ok = prevInputs[in.PreviousOutPoint]
				if !ok {
					return fmt.Errorf("missing prev outpoint %s", in.PreviousOutPoint)
				}

				op.AccountVersion = op.PrevInput.Version
				op.Account, err = dcrPkScriptToAccountAddr(op.PrevInput.Version,
					op.PrevInput.PkScript, chainParams)
				if err != nil {
					return err
				}
				if op.Account == "" {
					// Might happen for OP_RETURNs, ticket
					// commitments, etc.
					continue
				}

				op.Amount = -op.PrevInput.Amount
			}

			// Fill in op input data.
			op.IOIndex = i
			op.In = in
			op.Type = OpTypeDebit
			if op.Status == OpStatusReversed {
				op.Amount *= -1
			}

			if err := applyOp(op); err != nil {
				return err
			}

			// Track cumulative OpIndex.
			op.OpIndex += 1
		}

		return nil
	}

	// Helper to process the outputs.
	addTxOuts := func() error {
		// Reset op's input attributes.
		op.In = nil
		op.PrevInput = nil

		for i, out := range tx.TxOut {
			if out.Value == 0 {
				// Ignore OP_RETURNs and other zero-valued
				// outputs.
				//
				// TODO: decode ticket commitments?
				continue
			}

			op.AccountVersion = out.Version

			if (isTAdd || isTBase) && i == 0 {
				// The first output in TAdds and treasury bases
				// credit the treasury account.
				op.Account = TreasuryAccountAdddress
			} else {
				// Handle standard outputs.
				op.Account, err = dcrPkScriptToAccountAddr(out.Version,
					out.PkScript, chainParams)
				if err != nil {
					return err
				}
				if op.Account == "" {
					continue
				}
			}

			// Fill in op output data.
			op.IOIndex = i
			op.Out = out
			op.Type = OpTypeCredit
			op.Amount = dcrutil.Amount(out.Value)
			if op.Status == OpStatusReversed {
				op.Amount *= -1
			}

			if err := applyOp(op); err != nil {
				return err
			}

			// Track cumulative OpIndex.
			op.OpIndex += 1
		}

		return nil
	}

	if op.Status == OpStatusSuccess {
		if err := addTxIns(); err != nil {
			return err
		}
		if err := addTxOuts(); err != nil {
			return err
		}
	} else {
		// When reversing a tx we apply the update in the opposite
		// order: first roll back outputs (which were crediting an
		// amount) then inputs (which were debiting the amount + fee).
		if err := addTxOuts(); err != nil {
			return err
		}
		if err := addTxIns(); err != nil {
			return err
		}
	}

	return nil
}

// IterateBlockOps generates all Rosetta-understandable operations for the
// given block. It does so by iterating over every transaction and calling the
// applyOp callback on every transaction input and output.
//
// prev is only needed if block disapproves its parent block, in which case
// transactions in prev are reversed.
//
// fetchInputs must be able to fetch any previous output from the blocks.
//
// The operation passed to applyOp may be reused, so callers are expected to
// copy its contents if they will ne needed.
func IterateBlockOps(b, prev *wire.MsgBlock, fetchInputs PrevInputsFetcher,
	applyOp BlockOpCb, chainParams *chaincfg.Params) error {

	approvesParent := VoteBitsApprovesParent(b.Header.VoteBits) || b.Header.Height == 0
	if !approvesParent && prev == nil {
		return ErrNeedsPreviousBlock
	}

	// Use a single op var.
	var op Op

	// Helper to apply a set of transactions.
	applyTxs := func(tree int8, status OpStatus, txs []*wire.MsgTx) error {
		op = Op{
			Tree:   tree,
			Status: status,
		}
		for i, tx := range txs {
			op.Tx = tx
			op.TxHash = tx.TxHash()
			op.TxIndex = i
			op.OpIndex = 0
			err := iterateBlockOpsInTx(&op, fetchInputs, applyOp,
				chainParams)
			if err != nil {
				return err
			}
		}

		return nil
	}

	if !approvesParent {
		// Reverse regular transactions of the previous block.
		if err := applyTxs(wire.TxTreeRegular, OpStatusReversed, prev.Transactions); err != nil {
			return err
		}
	}
	if err := applyTxs(wire.TxTreeRegular, OpStatusSuccess, b.Transactions); err != nil {
		return err
	}
	if err := applyTxs(wire.TxTreeStake, OpStatusSuccess, b.STransactions); err != nil {
		return err
	}

	return nil
}

// txMetaToRosetta generates the Rosetta transaction metadata for a given tx.
//
// If blockHash is specified, then the transaction identified constructed is
// blockhash:txhash.
func txMetaToRosetta(tx *wire.MsgTx, txHash, blockHash *chainhash.Hash) *rtypes.Transaction {
	var txId string

	meta := map[string]interface{}{
		"version":  tx.Version,
		"expiry":   tx.Expiry,
		"locktime": tx.LockTime,
	}

	switch {
	case blockHash != nil:
		txId = fmt.Sprintf("%s:%s", blockHash.String(), txHash.String())

		// Include the original tx hash as metadata in case of reversed
		// txs that use blockhash:txhash identifier.
		meta["original_tx_hash"] = txHash.String()
	default:
		txId = txHash.String()
	}

	return &rtypes.Transaction{
		TransactionIdentifier: &rtypes.TransactionIdentifier{
			Hash: txId,
		},
		Operations: []*rtypes.Operation{},
		Metadata:   meta,
	}

}

// WireBlockToRosetta converts the given block in wire representation to the
// block in rosetta representation. The previous block is needed when the
// current block disapproved the regular transactions of the previous one, in
// which case it must be specified or this function errors.
func WireBlockToRosetta(b, prev *wire.MsgBlock, fetchInputs PrevInputsFetcher,
	chainParams *chaincfg.Params) (*rtypes.Block, error) {

	approvesParent := VoteBitsApprovesParent(b.Header.VoteBits) || b.Header.Height == 0
	if !approvesParent && prev == nil {
		return nil, ErrNeedsPreviousBlock
	}

	var prevBlockHash chainhash.Hash
	if prev != nil {
		prevBlockHash = prev.BlockHash()
	}

	var txs []*rtypes.Transaction
	nbTxs := len(b.Transactions) + len(b.STransactions)
	if !approvesParent {
		nbTxs += len(prev.Transactions) + len(prev.STransactions)
	}
	txs = make([]*rtypes.Transaction, 0, nbTxs)

	// Closure that builds the list of transactions/ops by iterating over
	// the block's transactions.
	var tx *rtypes.Transaction
	applyOp := func(op *Op) error {
		if op.OpIndex == 0 {
			// Include the block hash in the tx identifier if this
			// is a reversed tx.
			var blockHash *chainhash.Hash
			if op.Status == OpStatusReversed {
				blockHash = &prevBlockHash
			}

			// Starting a new transaction.
			tx = txMetaToRosetta(op.Tx, &op.TxHash, blockHash)
			txs = append(txs, tx)
		}
		tx.Operations = append(tx.Operations, op.ROp())
		return nil
	}

	// Build the list of transactions.
	err := IterateBlockOps(b, prev, fetchInputs, applyOp, chainParams)
	if err != nil {
		return nil, err
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
func MempoolTxToRosetta(tx *wire.MsgTx, fetchInputs PrevInputsFetcher,
	chainParams *chaincfg.Params) (*rtypes.Transaction, error) {

	txh := tx.TxHash()
	rtx := txMetaToRosetta(tx, &txh, nil)
	applyOp := func(op *Op) error {
		rtx.Operations = append(rtx.Operations, op.ROp())
		return nil
	}

	txType := stake.DetermineTxType(tx, true)
	tree := wire.TxTreeRegular
	if txType != stake.TxTypeRegular {
		tree = wire.TxTreeStake
	}

	op := Op{
		Tx:     tx,
		TxHash: txh,
		Tree:   tree,
		Status: OpStatusSuccess,

		// Coinbase txs are never seen on the mempool so it's safe to
		// use a negative txidx.
		TxIndex: -1,
	}
	err := iterateBlockOpsInTx(&op, fetchInputs, applyOp,
		chainParams)
	if err != nil {
		return nil, err
	}

	return rtx, nil
}
