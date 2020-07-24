package types

import (
	"bytes"
	"testing"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
)

type rosToTxTestCase struct {
	op     *rtypes.Operation
	in     *wire.TxIn
	out    *wire.TxOut
	signer string
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

func (tctx *rosToTxTestContext) signers() []string {
	signers := make([]string, 0, len(tctx.testCases))
	for _, tc := range tctx.testCases {
		if tc.signer == "" {
			continue
		}
		signers = append(signers, tc.signer)
	}
	return signers
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

				// Only needed to extract signing payload.
				"script_version": uint16(0),
			},

			// Only needed to extract signing payload.
			Account: &rtypes.AccountIdentifier{
				Address: "RsPSidp9af5pbGBBQYb3VcRLGzHaPma1Xpv",
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
		signer: "RsPSidp9af5pbGBBQYb3VcRLGzHaPma1Xpv",
	}, {
		op: &rtypes.Operation{
			Type:   "debit",
			Amount: amt(30),
			Metadata: map[string]interface{}{
				"prev_hash":    prevHash1,
				"prev_index":   uint32(0),
				"prev_tree":    int8(0),
				"sequence":     uint32(0),
				"block_height": uint32(0),
				"block_index":  uint32(0),

				// Only needed to extract signing payload.
				"script_version": uint16(1),
			},

			// Only needed to extract signing payload.
			Account: &rtypes.AccountIdentifier{
				// Despite being a valid address, using the
				// wrong script_version means this doesn't
				// become a signer.
				Address: "RsPSidp9af5pbGBBQYb3VcRLGzHaPma1Xpv",
			},
		},
		in: &wire.TxIn{
			PreviousOutPoint: wire.OutPoint{
				Hash: mustDecodeHash(prevHash1),
			},
			ValueIn: 30,
		},
		signer: "",
	}, {
		op: &rtypes.Operation{
			Type:   "debit",
			Amount: amt(20),
			Metadata: map[string]interface{}{
				"prev_hash":    prevHash1,
				"prev_index":   uint32(0),
				"prev_tree":    int8(0),
				"sequence":     uint32(0),
				"block_height": uint32(0),
				"block_index":  uint32(0),

				// Only needed to extract signing payload.
				"script_version": uint16(0),
			},

			// Only needed to extract signing payload.
			Account: &rtypes.AccountIdentifier{
				Address: "RcaJVhnU11HaKVy95dGaPRMRSSWrb3KK2u1",
			},
		},
		in: &wire.TxIn{
			PreviousOutPoint: wire.OutPoint{
				Hash: mustDecodeHash(prevHash1),
			},
			ValueIn: 20,
		},
		signer: "RcaJVhnU11HaKVy95dGaPRMRSSWrb3KK2u1",
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

// TestExtractTxSigners tests that we can extract the correct signers for a
// given list of Rosetta operations.
func TestExtractTxSigners(t *testing.T) {
	tctx := rosToTxTestCases()
	chainParams := chaincfg.RegNetParams()

	txMeta := map[string]interface{}{
		"version":  uint16(0),
		"expiry":   uint32(0),
		"locktime": uint32(0),
	}

	// We use RosettaOpsToTx in this test since we only expect to extract
	// signing payloads from txs constructed by this function.
	tx, err := RosettaOpsToTx(txMeta, tctx.ops(), chainParams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	signers, err := ExtractTxSigners(tctx.ops(), tx, chainParams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantSigners := tctx.signers()
	if len(wantSigners) != len(signers) {
		t.Fatalf("unexpected nb of signers. want=%d got=%d",
			len(wantSigners), len(signers))
	}

	for i := range wantSigners {
		if wantSigners[i] != signers[i] {
			t.Fatalf("wrong order of signers at idx %d. want=%s got=%s",
				i, wantSigners[i], signers[i])
		}
	}
}

// TestExtractSignPayloads tests that we can extract the correct signature
// payloads for a given list of Rosetta operations.
func TestExtractSignPayloads(t *testing.T) {
	tctx := rosToTxTestCases()
	chainParams := chaincfg.RegNetParams()

	txMeta := map[string]interface{}{
		"version":  uint16(0),
		"expiry":   uint32(0),
		"locktime": uint32(0),
	}

	// We use RosettaOpsToTx in this test since we only expect to extract
	// signing payloads from txs constructed by this function.
	tx, err := RosettaOpsToTx(txMeta, tctx.ops(), chainParams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payloads, err := ExtractSignPayloads(tctx.ops(), tx, chainParams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var pidx int
	var inIdx int
	for tci, tc := range tctx.testCases {
		// Only debits (i.e. inputs) generate a signing payload.
		if tc.op.Type != "debit" {
			continue
		}

		// Skip if this isn't supposed to be signed.
		if tc.signer == "" {
			continue
		}

		addr, _ := dcrutil.DecodeAddress(tc.op.Account.Address, chainParams)
		if _, ok := addr.(*dcrutil.AddressPubKeyHash); !ok {
			// Anything other than an AddresPubKeyHash (including
			// decoding errors) doesn't currently generate a
			// signing payload.
			inIdx++
			continue
		}
		sigType := rtypes.Ecdsa

		pkScript, _ := txscript.PayToAddrScript(addr)
		sigHash, _ := txscript.CalcSignatureHash(pkScript, sigHashType,
			tx, inIdx, nil)

		if pidx >= len(payloads) {
			t.Fatalf("tc %d unexpected nb of payloads. want=%d "+
				"got=%d", tci, pidx+1, len(payloads))
		}

		pay := payloads[pidx]
		if pay.Address != addr.Address() {
			t.Fatalf("tc %d unexpected address. want=%s got=%s",
				tci, addr.Address(), pay.Address)
		}

		if !bytes.Equal(pay.Bytes, sigHash) {
			t.Fatalf("tc %d unexpected bytes. want=%x got=%x",
				tci, sigHash, pay.Bytes)
		}

		if pay.SignatureType != sigType {
			t.Fatalf("tc %d unexpected sigType. want=%s got=%s",
				tci, sigType, pay.SignatureType)
		}
		pidx++
		inIdx++
	}
}
