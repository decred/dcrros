package types

import (
	"bytes"
	"errors"
	"testing"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v3/ecdsa"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
	"github.com/stretchr/testify/require"
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
		t.Fatalf("op %d incorrect prevout. want=%s got=%s", opIdx,
			tc.in.PreviousOutPoint, in.PreviousOutPoint)
	}

	if tc.in.Sequence != in.Sequence {
		t.Fatalf("op %d incorrect sequence. want=%d got=%d", opIdx,
			tc.in.Sequence, in.Sequence)
	}

	if tc.in.ValueIn != in.ValueIn {
		t.Fatalf("op %d incorrect valueIn. want=%d got=%d", opIdx,
			tc.in.ValueIn, in.ValueIn)
	}

	if tc.in.BlockHeight != in.BlockHeight {
		t.Fatalf("op %d incorrect blockHeight. want=%d got=%d", opIdx,
			tc.in.BlockHeight, in.BlockHeight)
	}

	if tc.in.BlockIndex != in.BlockIndex {
		t.Fatalf("op %d incorrect blockIndex. want=%d got=%d", opIdx,
			tc.in.BlockIndex, in.BlockIndex)
	}

	if !bytes.Equal(tc.in.SignatureScript, in.SignatureScript) {
		t.Fatalf("op %d incorrect sigScript. want=%x got=%x", opIdx,
			tc.in.SignatureScript, in.SignatureScript)
	}
}

func (tc *rosToTxTestCase) assertMatchesOut(t *testing.T, opIdx int, out *wire.TxOut) {
	if tc.out == nil {
		t.Fatalf("op %d not expecing a txOut", opIdx)
	}

	if tc.out.Value != out.Value {
		t.Fatalf("op %d incorrect value. want=%d got=%d", opIdx,
			tc.out.Value, out.Value)
	}

	if tc.out.Version != out.Version {
		t.Fatalf("op %d incorrect version. want=%d got=%d", opIdx,
			tc.out.Version, out.Version)
	}

	if !bytes.Equal(tc.out.PkScript, out.PkScript) {
		t.Fatalf("op %d incorrect pkScript. want=%x got=%x", opIdx,
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
				"prev_tree":        int8(1),
				"sequence":         uint32(1000),
				"block_height":     uint32(2000),
				"block_index":      uint32(3000),
				"signature_script": "102030",
			},

			// Only needed to extract signing payload.
			Account: &rtypes.AccountIdentifier{
				Address: "RsPSidp9af5pbGBBQYb3VcRLGzHaPma1Xpv",
				Metadata: map[string]interface{}{
					"script_version": uint16(0),
				},
			},

			CoinChange: &rtypes.CoinChange{
				CoinIdentifier: &rtypes.CoinIdentifier{
					Identifier: prevHash1 + ":1",
				},
				CoinAction: rtypes.CoinSpent,
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
			Amount: amt(-30),
			Metadata: map[string]interface{}{
				"prev_tree":    int8(0),
				"sequence":     uint32(0),
				"block_height": uint32(0),
				"block_index":  uint32(0),
			},

			// Only needed to extract signing payload.
			Account: &rtypes.AccountIdentifier{
				// Despite being a valid address, using the
				// wrong script_version means this doesn't
				// become a signer.
				Address: "RsPSidp9af5pbGBBQYb3VcRLGzHaPma1Xpv",
				Metadata: map[string]interface{}{
					"script_version": uint16(1),
				},
			},

			CoinChange: &rtypes.CoinChange{
				CoinIdentifier: &rtypes.CoinIdentifier{
					Identifier: prevHash1 + ":0",
				},
				CoinAction: rtypes.CoinSpent,
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
				"prev_tree":    int8(0),
				"sequence":     uint32(0),
				"block_height": uint32(0),
				"block_index":  uint32(0),
			},

			// Only needed to extract signing payload.
			Account: &rtypes.AccountIdentifier{
				Address: "RcaJVhnU11HaKVy95dGaPRMRSSWrb3KK2u1",
				Metadata: map[string]interface{}{
					"script_version": uint16(0),
				},
			},

			CoinChange: &rtypes.CoinChange{
				CoinIdentifier: &rtypes.CoinIdentifier{
					Identifier: prevHash1 + ":0",
				},
				CoinAction: rtypes.CoinSpent,
			},
		},
		in: &wire.TxIn{
			PreviousOutPoint: wire.OutPoint{
				Hash: mustDecodeHash(prevHash1),
			},
			ValueIn: 20,
		},
		signer: "",
	}, {
		op: &rtypes.Operation{
			Type:   "credit",
			Amount: amt(20),
			Metadata: map[string]interface{}{
				"pk_script": pks1,
			},
			Account: &rtypes.AccountIdentifier{
				Address: "RsPSidp9af5pbGBBQYb3VcRLGzHaPma1Xpv",
				Metadata: map[string]interface{}{
					"script_version": uint16(0),
				},
			},

			CoinChange: &rtypes.CoinChange{
				CoinIdentifier: &rtypes.CoinIdentifier{
					Identifier: "xxxxx:0",
				},
				CoinAction: rtypes.CoinCreated,
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

// TestRosettaOpsToTxEdges tests edge cases of conversion from Rosetta ops to a
// transaction.
func TestRosettaOpsToTxEdges(t *testing.T) {
	chainParams := chaincfg.RegNetParams()

	dummyAccount := &rtypes.AccountIdentifier{
		Address: "RsPSidp9af5pbGBBQYb3VcRLGzHaPma1Xpv",
		Metadata: map[string]interface{}{
			"script_version": uint16(0),
		},
	}

	// Tests involving tx metadata (requires calling the exported
	// RosettaOpsToTx).
	t.Run("empty tx meta", func(t *testing.T) {
		txMeta := map[string]interface{}{}
		tx, err := RosettaOpsToTx(txMeta, []*rtypes.Operation{}, chainParams)
		require.NoError(t, err)
		require.Equal(t, uint16(1), tx.Version)
		require.Equal(t, uint32(0), tx.Expiry)
		require.Equal(t, uint32(0), tx.LockTime)
	})

	t.Run("filled tx meta", func(t *testing.T) {
		txMeta := map[string]interface{}{
			"version":  uint16(10),
			"expiry":   uint32(20),
			"locktime": uint32(30),
		}

		tx, err := RosettaOpsToTx(txMeta, []*rtypes.Operation{}, chainParams)
		require.NoError(t, err)
		require.Equal(t, uint16(10), tx.Version)
		require.Equal(t, uint32(20), tx.Expiry)
		require.Equal(t, uint32(30), tx.LockTime)
	})

	// Tests for individual op errors (uses the unexported rosettaOpToTx).
	testCases := []struct {
		name    string
		op      *rtypes.Operation
		wantErr error
	}{{
		name: "unknown op type",
		op: &rtypes.Operation{
			Amount: DcrAmountToRosetta(1),
			Type:   "*unknown",
		},
		wantErr: ErrUnkownOpType,
	}, {
		name: "op without amount",
		op: &rtypes.Operation{
			Type: "debit",
		},
		wantErr: errNilAmount,
	}, {
		name: "debit without coin change",
		op: &rtypes.Operation{
			Type:    "debit",
			Amount:  DcrAmountToRosetta(1),
			Account: dummyAccount,
		},
		wantErr: errNilCoinChange,
	}, {
		name: "debit with wrong coin action",
		op: &rtypes.Operation{
			Type:    "debit",
			Amount:  DcrAmountToRosetta(1),
			Account: dummyAccount,
			CoinChange: &rtypes.CoinChange{
				CoinAction: rtypes.CoinCreated,
				CoinIdentifier: &rtypes.CoinIdentifier{
					Identifier: wire.OutPoint{}.String(),
				},
			},
		},
		wantErr: errWrongDebitCoinAction,
	}, {
		name: "credit without account",
		op: &rtypes.Operation{
			Type:   "credit",
			Amount: DcrAmountToRosetta(1),
		},
		wantErr: ErrNilAccount,
	}}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tx := wire.NewMsgTx()
			gotErr := rosettaOpToTx(tc.op, tx, chainParams)
			if !errors.Is(gotErr, tc.wantErr) {
				t.Fatalf("unexpected error. want=%v, got=%v",
					tc.wantErr, gotErr)
			}
		})
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
		if wantSigners[i] != signers[i].Address {
			t.Fatalf("wrong order of signers at idx %d. want=%s got=%s",
				i, wantSigners[i], signers[i].Address)
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
		if pay.AccountIdentifier.Address != addr.Address() {
			t.Fatalf("tc %d unexpected address. want=%s got=%s",
				tci, addr.Address(), pay.AccountIdentifier.Address)
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

func mustParsePrivKey(s string) *secp256k1.PrivateKey {
	return secp256k1.PrivKeyFromBytes(mustHex(s))
}

// TestCombineSigs tests the function that combines signatures to an unsinged
// transaction.
func TestCombineSigs(t *testing.T) {
	chainParams := chaincfg.RegNetParams()

	privKey := mustParsePrivKey("06013accb1e4683ba950cbf8326bd385ad83e8a25485da6ba3cf1c4de75c47d3")
	pubKey := privKey.PubKey()
	pubKeyBytes := pubKey.SerializeCompressed()
	pkScript := mustHex("76a9140f7298d134a0bf2af6ecf7e978bba4faa6d4fa4188ac")
	scriptVersion := uint16(0)
	scriptFlags := txscript.ScriptDiscourageUpgradableNops |
		txscript.ScriptVerifyCheckLockTimeVerify |
		txscript.ScriptVerifyCleanStack |
		txscript.ScriptVerifySigPushOnly |
		txscript.ScriptVerifySHA256 |
		txscript.ScriptVerifyTreasury

	// Helper to generate a valid, signed tx.
	txAndSigs := func(t *testing.T, nbIns int) ([]*rtypes.Signature, *wire.MsgTx) {
		tx := wire.NewMsgTx()
		tx.AddTxOut(&wire.TxOut{})
		for i := 0; i < nbIns; i++ {
			tx.AddTxIn(&wire.TxIn{})
		}

		sigs := make([]*rtypes.Signature, nbIns)
		for i := 0; i < nbIns; i++ {
			var err error
			sigHash, err := txscript.CalcSignatureHash(pkScript,
				txscript.SigHashAll, tx, i, nil)
			require.NoError(t, err)
			sigBytes := ecdsa.SignCompact(privKey, sigHash, true)
			sigBytes = sigBytes[1:]

			pkCopy := make([]byte, len(pubKeyBytes))
			copy(pkCopy, pubKeyBytes)
			sigs[i] = &rtypes.Signature{
				SigningPayload: &rtypes.SigningPayload{
					Bytes: sigHash,
				},
				PublicKey: &rtypes.PublicKey{
					Bytes:     pkCopy,
					CurveType: rtypes.Secp256k1,
				},
				SignatureType: rtypes.Ecdsa,
				Bytes:         sigBytes,
			}
		}

		return sigs, tx
	}

	nCorrectSigs := func(nbIns int) func(t *testing.T) ([]*rtypes.Signature, *wire.MsgTx) {
		return func(t *testing.T) ([]*rtypes.Signature, *wire.MsgTx) {
			return txAndSigs(t, nbIns)
		}
	}

	type testCase struct {
		name    string
		genSigs func(t *testing.T) ([]*rtypes.Signature, *wire.MsgTx)
		wantErr error
	}

	testCases := []testCase{{
		name:    "zero correct sigs",
		genSigs: nCorrectSigs(0),
	}, {
		name:    "one correct sig",
		genSigs: nCorrectSigs(1),
	}, {
		name:    "three correct sigs",
		genSigs: nCorrectSigs(3),
	}, {
		name: "less inputs than sigs",
		genSigs: func(t *testing.T) ([]*rtypes.Signature, *wire.MsgTx) {
			sigs, tx := txAndSigs(t, 2)
			sigs = append(sigs, sigs[0])
			return sigs, tx
		},
		wantErr: ErrIncorrectSigCount,
	}, {
		name: "less sigs than inputs",
		genSigs: func(t *testing.T) ([]*rtypes.Signature, *wire.MsgTx) {
			sigs, tx := txAndSigs(t, 3)
			sigs = sigs[:2]
			return sigs, tx
		},
		wantErr: ErrIncorrectSigCount,
	}, {
		name: "invalid pubkey",
		genSigs: func(t *testing.T) ([]*rtypes.Signature, *wire.MsgTx) {
			sigs, tx := txAndSigs(t, 3)
			sigs[1].PublicKey.Bytes[1] = ^sigs[1].PublicKey.Bytes[1]
			return sigs, tx
		},
		wantErr: ErrInvalidPubKey,
	}, {
		name: "short signature",
		genSigs: func(t *testing.T) ([]*rtypes.Signature, *wire.MsgTx) {
			sigs, tx := txAndSigs(t, 3)
			sigs[1].Bytes = sigs[1].Bytes[1:]
			return sigs, tx
		},
		wantErr: ErrIncorrectSigSize,
	}, {
		name: "invalid signature",
		genSigs: func(t *testing.T) ([]*rtypes.Signature, *wire.MsgTx) {
			sigs, tx := txAndSigs(t, 3)
			sigs[1].Bytes[0] = ^sigs[1].Bytes[0]
			return sigs, tx
		},
		wantErr: ErrInvalidSig,
	}, {
		name: "unsupported curve",
		genSigs: func(t *testing.T) ([]*rtypes.Signature, *wire.MsgTx) {
			sigs, tx := txAndSigs(t, 3)
			sigs[1].PublicKey.CurveType = "foo"
			return sigs, tx
		},
		wantErr: ErrUnsupportedCurveType,
	}, {
		name: "unsupported sig type",
		genSigs: func(t *testing.T) ([]*rtypes.Signature, *wire.MsgTx) {
			sigs, tx := txAndSigs(t, 3)
			sigs[1].SignatureType = "foo"
			return sigs, tx
		},
		wantErr: ErrUnsupportedSignatureType,
	}}

	test := func(t *testing.T, tc *testCase) {
		t.Parallel()

		sigs, tx := tc.genSigs(t)
		err := CombineTxSigs(sigs, tx, chainParams)
		if !errors.Is(err, tc.wantErr) {
			t.Fatalf("unexpected error. want=%v got=%v",
				tc.wantErr, err)
		}

		if tc.wantErr != nil {
			return
		}

		// If all sigs were successfully combined, all script inputs
		// should be correctly executed.
		for i := 0; i < len(tx.TxIn); i++ {
			vm, err := txscript.NewEngine(pkScript, tx, i,
				scriptFlags, scriptVersion, nil)
			require.NoError(t, err)

			if err := vm.Execute(); err != nil {
				t.Fatalf("error executing resulting script %d: %v",
					i, err)
			}
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}

// TestExtractPrevInputsFromOps ensures the ExtractPRevInputsFromOps behaves as
// expected.
func TestExtractPrevInputsFromOps(t *testing.T) {
	chainParams := chaincfg.RegNetParams()

	type testCase struct {
		name    string
		ops     []*rtypes.Operation
		target  map[wire.OutPoint]*PrevInput
		wantErr bool
	}

	hashHex1 := "574dfd8c1b169acfdfc245d4402346ea4d1aea8806e722e0be5796effa75767c"
	hashHash1 := mustHash(hashHex1)
	hashHex2 := "d1aea8d4be567c402346ea45796effa757806e574dfd8c1b169acfdfc24722e0"
	hashHash2 := mustHash(hashHex2)

	testCases := []testCase{{
		name:   "no ops",
		ops:    []*rtypes.Operation{},
		target: map[wire.OutPoint]*PrevInput{},
	}, {
		name: "no debits",
		ops: []*rtypes.Operation{{
			Type: "credit",
		}},
		target: map[wire.OutPoint]*PrevInput{},
	}, {
		name: "one debit",
		ops: []*rtypes.Operation{{
			Type: "debit",
			Account: &rtypes.AccountIdentifier{
				Address: "RsDTyN2KTSyu5gJzBFN5ra1wbHECUBsXWiA",
				Metadata: map[string]interface{}{
					"script_version": uint16(0),
				},
			},
			Amount: DcrAmountToRosetta(10),
			CoinChange: &rtypes.CoinChange{
				CoinIdentifier: &rtypes.CoinIdentifier{
					Identifier: hashHex1 + ":1",
				},
				CoinAction: "coin_spent",
			},
		}},
		target: map[wire.OutPoint]*PrevInput{
			{Hash: hashHash1, Index: 1}: {
				PkScript: mustHex("76a91438336cea45bafa04c6c1dab1e588c8ce7de3a71b88ac"),
				Amount:   10,
				Version:  0,
			},
		},
	}, {
		name: "two debits",
		ops: []*rtypes.Operation{{
			Type: "debit",
			Account: &rtypes.AccountIdentifier{
				Address: "RsDTyN2KTSyu5gJzBFN5ra1wbHECUBsXWiA",
				Metadata: map[string]interface{}{
					"script_version": uint16(0),
				},
			},
			Amount: DcrAmountToRosetta(10),
			CoinChange: &rtypes.CoinChange{
				CoinIdentifier: &rtypes.CoinIdentifier{
					Identifier: hashHex1 + ":1",
				},
				CoinAction: "coin_spent",
			},
		}, {
			Type: "debit",
			Account: &rtypes.AccountIdentifier{
				Address: "RsDTyN2KTSyu5gJzBFN5ra1wbHECUBsXWiA",
				Metadata: map[string]interface{}{
					"script_version": uint16(0),
				},
			},
			Amount: DcrAmountToRosetta(20),
			CoinChange: &rtypes.CoinChange{
				CoinIdentifier: &rtypes.CoinIdentifier{
					Identifier: hashHex2 + ":10",
				},
				CoinAction: "coin_spent",
			},
		}},
		target: map[wire.OutPoint]*PrevInput{
			{Hash: hashHash1, Index: 1}: {
				PkScript: mustHex("76a91438336cea45bafa04c6c1dab1e588c8ce7de3a71b88ac"),
				Amount:   10,
				Version:  0,
			},
			{Hash: hashHash2, Index: 10}: {
				PkScript: mustHex("76a91438336cea45bafa04c6c1dab1e588c8ce7de3a71b88ac"),
				Amount:   20,
				Version:  0,
			},
		},
	}, {
		name: "no account version",
		ops: []*rtypes.Operation{{
			Type: "debit",
			Account: &rtypes.AccountIdentifier{
				Address:  "RsDTyN2KTSyu5gJzBFN5ra1wbHECUBsXWiA",
				Metadata: map[string]interface{}{},
			},
			Amount: DcrAmountToRosetta(10),
			CoinChange: &rtypes.CoinChange{
				CoinIdentifier: &rtypes.CoinIdentifier{
					Identifier: hashHex1 + ":1",
				},
				CoinAction: "coin_spent",
			},
		}},
		wantErr: true,
	}, {
		name: "no account",
		ops: []*rtypes.Operation{{
			Type:   "debit",
			Amount: DcrAmountToRosetta(10),
			CoinChange: &rtypes.CoinChange{
				CoinIdentifier: &rtypes.CoinIdentifier{
					Identifier: hashHex1 + ":1",
				},
				CoinAction: "coin_spent",
			},
		}},
		wantErr: true,
	}, {
		name: "no coin change identifier",
		ops: []*rtypes.Operation{{
			Type: "debit",
			Account: &rtypes.AccountIdentifier{
				Address: "RsDTyN2KTSyu5gJzBFN5ra1wbHECUBsXWiA",
				Metadata: map[string]interface{}{
					"script_version": uint16(0),
				},
			},
			Amount: DcrAmountToRosetta(10),
			CoinChange: &rtypes.CoinChange{
				CoinAction: "coin_spent",
			},
		}},
		wantErr: true,
	}, {
		name: "no coin change",
		ops: []*rtypes.Operation{{
			Type: "debit",
			Account: &rtypes.AccountIdentifier{
				Address: "RsDTyN2KTSyu5gJzBFN5ra1wbHECUBsXWiA",
				Metadata: map[string]interface{}{
					"script_version": uint16(0),
				},
			},
			Amount: DcrAmountToRosetta(10),
		}},
		wantErr: true,
	}}

	test := func(t *testing.T, tc *testCase) {
		gotMap, err := ExtractPrevInputsFromOps(tc.ops, chainParams)
		gotErr := err != nil
		if gotErr != tc.wantErr {
			t.Fatalf("unexpected error. want=%v, got=%v, err=%v",
				tc.wantErr, gotErr, err)
		}

		if tc.wantErr {
			return
		}

		require.Equal(t, tc.target, gotMap)
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}

}
