package types

import (
	"bytes"
	"testing"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/wire"
)

type rosToTxTestCase struct {
	op  *rtypes.Operation
	in  *wire.TxIn
	out *wire.TxOut
}

func (tc *rosToTxTestCase) assertMatchesIn(t *testing.T, opIdx int, in *wire.TxIn) {
	if tc.in == nil {
		t.Fatalf("op %d not expecing a txIn", opIdx)
	}

	if tc.in.PreviousOutPoint != in.PreviousOutPoint {
		t.Fatalf("op %d incorect prevout. want=%s got=%s", opIdx,
			tc.in.PreviousOutPoint, in.PreviousOutPoint)
	}

	if tc.in.Sequence != in.Sequence {
		t.Fatalf("op %d incorect sequence. want=%d got=%d", opIdx,
			tc.in.Sequence, in.Sequence)
	}

	if tc.in.ValueIn != in.ValueIn {
		t.Fatalf("op %d incorect valueIn. want=%d got=%d", opIdx,
			tc.in.ValueIn, in.ValueIn)
	}

	if tc.in.BlockHeight != in.BlockHeight {
		t.Fatalf("op %d incorect blockHeight. want=%d got=%d", opIdx,
			tc.in.BlockHeight, in.BlockHeight)
	}

	if tc.in.BlockIndex != in.BlockIndex {
		t.Fatalf("op %d incorect blockIndex. want=%d got=%d", opIdx,
			tc.in.BlockIndex, in.BlockIndex)
	}

	if !bytes.Equal(tc.in.SignatureScript, in.SignatureScript) {
		t.Fatalf("op %d incorect sigScript. want=%x got=%x", opIdx,
			tc.in.SignatureScript, in.SignatureScript)
	}
}

func (tc *rosToTxTestCase) assertMatchesOut(t *testing.T, opIdx int, out *wire.TxOut) {
	if tc.out == nil {
		t.Fatalf("op %d not expecing a txOut", opIdx)
	}

	if tc.out.Value != out.Value {
		t.Fatalf("op %d incorect value. want=%d got=%d", opIdx,
			tc.out.Value, out.Value)
	}

	if tc.out.Version != out.Version {
		t.Fatalf("op %d incorect version. want=%d got=%d", opIdx,
			tc.out.Version, out.Version)
	}

	if !bytes.Equal(tc.out.PkScript, out.PkScript) {
		t.Fatalf("op %d incorect pkScript. want=%x got=%x", opIdx,
			tc.out.PkScript, out.PkScript)
	}
}

type rosToTxTestContext struct {
	testCases []*rosToTxTestCase
}

func (tctx *rosToTxTestContext) ops() []*rtypes.Operation {
	ops := make([]*rtypes.Operation, 0, len(tctx.testCases))
	for _, tc := range tctx.testCases {
		ops = append(ops, tc.op)
	}
	return ops
}

func mustDecodeHash(h string) chainhash.Hash {
	var hh chainhash.Hash
	err := chainhash.Decode(&hh, h)
	if err != nil {
		panic(err)
	}
	return hh
}

func rosToTxTestCases() *rosToTxTestContext {
	amt := DcrAmountToRosetta

	prevHash1 := "574dfd8c1b169acfdfc245d4402346ea4d1aea8806e722e0be5796effa75767c"
	pks1 := "76a914a5a7f924934685fbca3008c9524dae1cea9f9d3488ac"

	cases := []*rosToTxTestCase{{
		op: &rtypes.Operation{
			Type:   "debit",
			Amount: amt(10),
			Metadata: map[string]interface{}{
				"prev_hash":        prevHash1,
				"prev_index":       uint32(1),
				"prev_tree":        int8(1),
				"sequence":         uint32(1000),
				"block_height":     uint32(2000),
				"block_index":      uint32(3000),
				"signature_script": "102030",
			},
		},
		in: &wire.TxIn{
			PreviousOutPoint: wire.OutPoint{
				Hash:  mustDecodeHash(prevHash1),
				Index: 1,
				Tree:  1,
			},
			ValueIn:         10,
			Sequence:        1000,
			BlockHeight:     2000,
			BlockIndex:      3000,
			SignatureScript: []byte{0x10, 0x20, 0x30},
		},
	}, {
		op: &rtypes.Operation{
			Type:   "credit",
			Amount: amt(20),
			Metadata: map[string]interface{}{
				"script_version": uint16(0),
				"pk_script":      pks1,
			},
			Account: &rtypes.AccountIdentifier{
				Address: "RsPSidp9af5pbGBBQYb3VcRLGzHaPma1Xpv",
			},
		},
		out: &wire.TxOut{
			Value:    20,
			Version:  0,
			PkScript: mustHex(pks1),
		},
	}}

	return &rosToTxTestContext{
		testCases: cases,
	}
}

// TestRosettaOpsToTx tests that converting a slice of Rosetta ops to a Decred
// transaction works as expected.
func TestRosettaOpsToTx(t *testing.T) {

	chainParams := chaincfg.RegNetParams()
	tctx := rosToTxTestCases()

	txMeta := map[string]interface{}{
		"version":  uint16(0),
		"expiry":   uint32(0),
		"locktime": uint32(0),
	}

	tx, err := RosettaOpsToTx(txMeta, tctx.ops(), chainParams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The number of operations must match the number of inputs + outputs.
	gotNbIO := len(tx.TxIn) + len(tx.TxOut)
	if gotNbIO != len(tctx.testCases) {
		t.Fatalf("unexpected number of IOs. want=%d got=%d",
			len(tctx.testCases), gotNbIO)
	}

	// Verify the elements of the returned tx match the expected from the
	// test cases.
	var inIdx int
	var outIdx int
	for opIdx, tc := range tctx.testCases {
		if tc.in != nil {
			if inIdx >= len(tx.TxIn) {
				t.Fatalf("unexpected nb of txIn. want=%d got=%d",
					inIdx, len(tx.TxIn))
			}

			tc.assertMatchesIn(t, opIdx, tx.TxIn[inIdx])
			inIdx++
		}

		if tc.out != nil {
			if outIdx >= len(tx.TxOut) {
				t.Fatalf("unexpected nb of txOut. want=%d got=%d",
					outIdx, len(tx.TxOut))
			}

			tc.assertMatchesOut(t, opIdx, tx.TxOut[outIdx])
			outIdx++
		}
	}
}
