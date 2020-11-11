// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"decred.org/dcrros/types"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/davecgh/go-spew/spew"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v3/ecdsa"
	"github.com/decred/dcrd/dcrjson/v3"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
	"github.com/stretchr/testify/require"
)

// TestConsturctionDeriveEndpoint tests the ConstructionDerive() call behaves
// as expected.
func TestConstructionDeriveEndpoint(t *testing.T) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	pubkey := mustHex("03eb7ae61d440e13823589ce2e3728a02b7f55530d4463aaf8059c7c41ef521e7d")
	v0 := float64(0)
	v1 := float64(1)

	// Initialize the server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}
	svr := newTestServer(t, cfg)

	type testCase struct {
		name     string
		req      *rtypes.ConstructionDeriveRequest
		wantErr  error
		wantAddr string
	}

	testCases := []testCase{{
		name: "secp25k1 key for ecdsa algo",
		req: &rtypes.ConstructionDeriveRequest{
			PublicKey: &rtypes.PublicKey{
				Bytes:     pubkey,
				CurveType: rtypes.Secp256k1,
			},
			Metadata: map[string]interface{}{
				"script_version": v0,
				"algo":           "ecdsa",
			},
		},
		wantErr:  nil,
		wantAddr: "RsL5dxVrAaEdwHB37sQYe5HA8RoSKVnq8LN",
	}, {
		name: "secp25k1 key for schnorr algo",
		req: &rtypes.ConstructionDeriveRequest{
			PublicKey: &rtypes.PublicKey{
				Bytes:     pubkey,
				CurveType: rtypes.Secp256k1,
			},
			Metadata: map[string]interface{}{
				"script_version": v0,
				"algo":           "schnorr",
			},
		},
		wantErr:  nil,
		wantAddr: "RSP8EumNJZ3ucaJPkMPtt1WHMQXTqUeDcqA",
	}, {
		name: "secp25k1 key without algo specified defaults to ecdsa",
		req: &rtypes.ConstructionDeriveRequest{
			PublicKey: &rtypes.PublicKey{
				Bytes:     pubkey,
				CurveType: rtypes.Secp256k1,
			},
			Metadata: map[string]interface{}{
				"script_version": v0,
			},
		},
		wantErr:  nil,
		wantAddr: "RsL5dxVrAaEdwHB37sQYe5HA8RoSKVnq8LN",
	}, {
		name: "non v0 script fails",
		req: &rtypes.ConstructionDeriveRequest{
			PublicKey: &rtypes.PublicKey{
				Bytes:     pubkey,
				CurveType: rtypes.Secp256k1,
			},
			Metadata: map[string]interface{}{
				"script_version": v1,
			},
		},
		wantErr: types.ErrUnsupportedAddressVersion,
	}, {
		name: "script_version unspecified fails",
		req: &rtypes.ConstructionDeriveRequest{
			PublicKey: &rtypes.PublicKey{
				Bytes:     pubkey,
				CurveType: rtypes.Secp256k1,
			},
			Metadata: map[string]interface{}{},
		},
		wantErr: types.ErrUnspecifiedAddressVersion,
	}, {
		name: "short pubkey fails",
		req: &rtypes.ConstructionDeriveRequest{
			PublicKey: &rtypes.PublicKey{
				Bytes:     pubkey[:32],
				CurveType: rtypes.Secp256k1,
			},
			Metadata: map[string]interface{}{
				"script_version": v0,
			},
		},
		wantErr: types.ErrInvalidSecp256k1PubKey,
	}, {
		name: "long pubkey fails",
		req: &rtypes.ConstructionDeriveRequest{
			PublicKey: &rtypes.PublicKey{
				Bytes:     append(pubkey, 0x01),
				CurveType: rtypes.Secp256k1,
			},
			Metadata: map[string]interface{}{
				"script_version": v0,
			},
		},
		wantErr: types.ErrInvalidSecp256k1PubKey,
	}, {
		name: "invalid pubkey prefix byte fails",
		req: &rtypes.ConstructionDeriveRequest{
			PublicKey: &rtypes.PublicKey{
				Bytes:     append([]byte{0xff}, pubkey[:32]...),
				CurveType: rtypes.Secp256k1,
			},
			Metadata: map[string]interface{}{
				"script_version": v0,
			},
		},
		wantErr: types.ErrNotCompressedSecp256k1Key,
	}, {
		name: "unsupported algo fails",
		req: &rtypes.ConstructionDeriveRequest{
			PublicKey: &rtypes.PublicKey{
				Bytes:     pubkey,
				CurveType: rtypes.Secp256k1,
			},
			Metadata: map[string]interface{}{
				"script_version": v0,
				"algo":           "none",
			},
		},
		wantErr: types.ErrUnsupportedAddressAlgo,
	}, {
		name: "unsupported curve type fails",
		req: &rtypes.ConstructionDeriveRequest{
			PublicKey: &rtypes.PublicKey{
				Bytes:     pubkey,
				CurveType: "foo",
			},
			Metadata: map[string]interface{}{
				"script_version": v0,
			},
		},
		wantErr: types.ErrUnsupportedCurveType,
	}}

	test := func(t *testing.T, tc *testCase) {
		t.Parallel()

		res, rerr := svr.ConstructionDerive(context.Background(), tc.req)
		if !types.RosettaErrorIs(rerr, tc.wantErr) {
			t.Fatalf("unexpected error. want=%v got=%v",
				tc.wantErr, rerr)
		}

		if tc.wantErr != nil {
			return
		}

		gotAddr := res.AccountIdentifier.Address
		if gotAddr != tc.wantAddr {
			t.Fatalf("unexpected address. want=%v got=%v", tc.wantAddr,
				gotAddr)
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}

// TestConstructionPreprocessEndpoint tests that the ConstructionPreprocess()
// call works as expected.
func TestConstructionPreprocessEndpoint(t *testing.T) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	// Shorter function names to improve readability.
	amt := types.DcrAmountToRosetta
	prevHash1 := "574dfd8c1b169acfdfc245d4402346ea4d1aea8806e722e0be5796effa75767c"
	pks1 := "76a914a5a7f924934685fbca3008c9524dae1cea9f9d3488ac"
	debit := &rtypes.Operation{
		Type:   "debit",
		Amount: amt(-10),
		Metadata: map[string]interface{}{
			"prev_tree": int8(1),
			"sequence":  uint32(1000),
		},
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
	}
	credit := &rtypes.Operation{
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
	}

	varIntSize := func(i int) int { return wire.VarIntSerializeSize(uint64(i)) }
	sizeMeta := 4 + 4 + 4 // [version+sertype] + locktime + expiry
	sizeIn := func(nbInputs int) int {
		sizePrefix := 32 + 4 + 1 + 4           // hash + index + tree + sequence
		sizeWitness := 8 + 4 + 4               // valueIn + blockHeight + blockIndex
		sizeSigScript := varIntSize(108) + 108 // varint + p2pkh sigscript
		sizeInput := sizePrefix + sizeWitness + sizeSigScript
		return varIntSize(nbInputs)*2 + sizeInput*nbInputs
	}
	sizeOut := func(nbOutputs int) int {
		sizePrefix := 8 + 2    // value + scriptVersion
		sizePkScript := 1 + 25 // varint + p2pkh pkscript
		sizeOutput := sizePrefix + sizePkScript
		return varIntSize(nbOutputs) + sizeOutput*nbOutputs
	}

	// Initialize the server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}
	svr := newTestServer(t, cfg)

	type testCase struct {
		name        string
		nbCredits   int
		nbDebits    int
		wantSerSize int
	}

	testCases := []testCase{{
		name:        "1 credit and 1 debit",
		nbCredits:   1,
		nbDebits:    1,
		wantSerSize: sizeMeta + sizeOut(1) + sizeIn(1),
	}, {
		name:        "1 credit and 256 debits",
		nbCredits:   1,
		nbDebits:    256,
		wantSerSize: sizeMeta + sizeOut(1) + sizeIn(256),
	}, {
		name:        "256 credits and 1 debit",
		nbCredits:   256,
		nbDebits:    1,
		wantSerSize: sizeMeta + sizeOut(256) + sizeIn(1),
	}, {
		name:        "256 credits and 256 debits",
		nbCredits:   256,
		nbDebits:    256,
		wantSerSize: sizeMeta + sizeOut(256) + sizeIn(256),
	}}

	// test is the actual test function.
	test := func(t *testing.T, tc *testCase) {
		ops := make([]*rtypes.Operation, 0, tc.nbCredits+tc.nbDebits)
		for i := 0; i < tc.nbDebits; i++ {
			ops = append(ops, debit)
		}
		for i := 0; i < tc.nbCredits; i++ {
			ops = append(ops, credit)
		}
		req := &rtypes.ConstructionPreprocessRequest{
			Operations: ops,
			Metadata:   map[string]interface{}{},
		}
		res, rerr := svr.ConstructionPreprocess(context.Background(), req)
		require.Nil(t, rerr)

		gotSerSize := res.Options["serialize_size"].(int)
		if gotSerSize != tc.wantSerSize {
			t.Fatalf("unexpected serialize_size. want=%d got=%d",
				tc.wantSerSize, gotSerSize)
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}

// TestConstructionMetadataEndpoint tests that the ConstructionMetadata call
// behaves as expected.
func TestConstructionMetadataEndpoint(t *testing.T) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	// Initialize the server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}
	svr := newTestServer(t, cfg)

	type testCase struct {
		name    string
		req     *rtypes.ConstructionMetadataRequest
		wantErr error
		wantFee int64
	}

	testCases := []testCase{{
		name: "1 credit 1 debit tx",
		req: &rtypes.ConstructionMetadataRequest{
			Options: map[string]interface{}{
				"serialize_size": 217,
			},
		},
		wantErr: nil,
		wantFee: 2170,
	}, {
		name: "100 credits 100 debits tx",
		req: &rtypes.ConstructionMetadataRequest{
			Options: map[string]interface{}{
				"serialize_size": 20215,
			},
		},
		wantErr: nil,
		wantFee: 202150,
	}, {
		name: "serialize size not specified",
		req: &rtypes.ConstructionMetadataRequest{
			Options: map[string]interface{}{},
		},
		wantErr: types.ErrSerializeSizeUnspecified,
	}, {
		name: "serialize size not a number",
		req: &rtypes.ConstructionMetadataRequest{
			Options: map[string]interface{}{
				"serialize_size": "xxx",
			},
		},
		wantErr: types.ErrSerSizeNotNumber,
	}, {
		name: "serialize size json decoded number",
		req: &rtypes.ConstructionMetadataRequest{
			Options: map[string]interface{}{
				"serialize_size": float64(217),
			},
		},
		wantErr: nil,
		wantFee: 2170,
	}}

	// test is the actual test function.
	test := func(t *testing.T, tc *testCase) {
		res, rerr := svr.ConstructionMetadata(context.Background(), tc.req)
		if !types.RosettaErrorIs(rerr, tc.wantErr) {
			t.Fatalf("unexpected error. want=%v got=%v",
				tc.wantErr, rerr)
		}

		if tc.wantErr != nil {
			return
		}

		gotFee, err := strconv.ParseInt(res.SuggestedFee[0].Value, 10, 64)
		require.NoError(t, err)

		if gotFee != tc.wantFee {
			t.Fatalf("unexpected fee. want=%d got=%d", tc.wantFee, gotFee)
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}

// TestConstructionPayloadsEndpoint asserts the ConstructionPayloads() call
// works as expected.
//
// Note this doesn't test every possibility of the underlying RosettaOpsToTx()
// and ExtractPayloads() since those functions are independently tested in
// their originating package.
func TestConstructionPayloadsEndpoint(t *testing.T) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	// Shorter function names to improve readability.
	amt := types.DcrAmountToRosetta
	prevHash1 := "574dfd8c1b169acfdfc245d4402346ea4d1aea8806e722e0be5796effa75767c"
	pksEcdsa := "76a9144dab7c134c8b5f277b6ef9175e4d617e88d6d40088ac"
	addrEcdsa := "RsFRVNutxrodAcBuCLMNstc1XU1SMTWyqNo"
	expiry := uint32(2000)
	locktime := uint32(3000)
	version := uint16(3)
	txMeta := map[string]interface{}{
		"expiry":   expiry,
		"locktime": locktime,
		"version":  version,
	}

	// debit & debitIn define the same input.
	debit := &rtypes.Operation{
		Type:   "debit",
		Amount: amt(-10),
		Metadata: map[string]interface{}{
			"prev_tree": int8(1),
			"sequence":  uint32(1000),
		},
		Account: &rtypes.AccountIdentifier{
			Address: addrEcdsa,
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
	}
	debitIn := &wire.TxIn{
		PreviousOutPoint: wire.OutPoint{
			Hash:  mustHash(prevHash1),
			Index: 1,
			Tree:  1,
		},
		ValueIn:    10,
		BlockIndex: wire.NullBlockIndex,
		Sequence:   1000,
	}
	debitPayload := &rtypes.SigningPayload{
		AccountIdentifier: &rtypes.AccountIdentifier{
			Address: addrEcdsa,
			Metadata: map[string]interface{}{
				"script_version": 0,
			},
		},
		SignatureType: rtypes.Ecdsa,
	}

	// credit and creditOut define the same output.
	credit := &rtypes.Operation{
		Type:   "credit",
		Amount: amt(20),
		Metadata: map[string]interface{}{
			"pk_script": pksEcdsa,
		},
		Account: &rtypes.AccountIdentifier{
			Address: addrEcdsa,
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
	}
	creditOut := &wire.TxOut{
		Value:    20,
		PkScript: mustHex(pksEcdsa),
		Version:  0,
	}

	invalidOp := &rtypes.Operation{Type: "invalid"}

	// Initialize the server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}
	svr := newTestServer(t, cfg)

	type testCase struct {
		name         string
		req          *rtypes.ConstructionPayloadsRequest
		wantErr      error
		ins          []*wire.TxIn
		outs         []*wire.TxOut
		wantPayloads []*rtypes.SigningPayload
	}

	testCases := []testCase{{
		name: "1 credit 1 debit",
		req: &rtypes.ConstructionPayloadsRequest{
			Metadata:   txMeta,
			Operations: []*rtypes.Operation{debit, credit},
		},
		wantErr:      nil,
		ins:          []*wire.TxIn{debitIn},
		outs:         []*wire.TxOut{creditOut},
		wantPayloads: []*rtypes.SigningPayload{debitPayload},
	}, {
		name: "3 credits 3 debits",
		req: &rtypes.ConstructionPayloadsRequest{
			Metadata: txMeta,
			Operations: []*rtypes.Operation{debit, debit, debit,
				credit, credit, credit},
		},
		wantErr: nil,
		ins:     []*wire.TxIn{debitIn, debitIn, debitIn},
		outs:    []*wire.TxOut{creditOut, creditOut, creditOut},
		wantPayloads: []*rtypes.SigningPayload{debitPayload, debitPayload,
			debitPayload},
	}, {
		name: "invalid op",
		req: &rtypes.ConstructionPayloadsRequest{
			Metadata:   txMeta,
			Operations: []*rtypes.Operation{invalidOp},
		},
		wantErr:      types.ErrInvalidOp,
		ins:          []*wire.TxIn{debitIn},
		outs:         []*wire.TxOut{creditOut},
		wantPayloads: []*rtypes.SigningPayload{debitPayload},
	}}

	// test is the actual test function.
	test := func(t *testing.T, tc *testCase) {
		t.Parallel()

		res, rerr := svr.ConstructionPayloads(context.Background(), tc.req)
		if !types.RosettaErrorIs(rerr, tc.wantErr) {
			t.Fatalf("unexpected error. want=%v got=%v",
				tc.wantErr, rerr)
		}

		if tc.wantErr != nil {
			return
		}

		wantTx := &wire.MsgTx{
			Version:  version,
			Expiry:   expiry,
			LockTime: locktime,
			TxIn:     tc.ins,
			TxOut:    tc.outs,
		}
		wantTxBytes, err := wantTx.Bytes()
		require.NoError(t, err)
		gotTxBytes := mustHex(res.UnsignedTransaction)
		var gotTx wire.MsgTx
		if err := gotTx.FromBytes(gotTxBytes); err != nil {
			t.Logf("tx bytes: %s", res.UnsignedTransaction)
			t.Fatalf("unable to decode got tx: %v", err)
		}

		if !bytes.Equal(wantTxBytes, gotTxBytes) {
			t.Logf("want tx %s", spew.Sdump(wantTx))
			t.Logf("got tx %s", spew.Sdump(gotTx))
			t.Fatalf("unexpected usinged tx. want\n%x\n\ngot\n%x\n",
				wantTxBytes, gotTxBytes)
		}

		if len(tc.wantPayloads) != len(res.Payloads) {
			t.Fatalf("unexpected nb of payloads. want=%d got=%d",
				len(tc.wantPayloads), len(res.Payloads))
		}

		for i := 0; i < len(tc.wantPayloads); i++ {
			wantPay := tc.wantPayloads[i]
			gotPay := res.Payloads[i]
			wantAccount := wantPay.AccountIdentifier
			gotAccount := gotPay.AccountIdentifier
			if wantAccount.Address != gotAccount.Address {
				t.Fatalf("unexpected address. want=%s got=%s",
					wantAccount.Address, gotAccount.Address)
			}

			wantVersion := wantAccount.Metadata["script_version"].(int)
			gotVersion := gotAccount.Metadata["script_version"].(uint16)
			if wantVersion != int(gotVersion) {
				t.Fatalf("unexpected version. want=%d got=%d",
					wantVersion, gotVersion)
			}

			if wantPay.SignatureType != gotPay.SignatureType {
				t.Fatalf("unexpected sig type. want=%s got=%s",
					wantPay.SignatureType, gotPay.SignatureType)
			}

			// Calculate the expected sighash given the test tx.
			wantSigHash, err := txscript.CalcSignatureHash(
				mustHex(pksEcdsa), txscript.SigHashAll, wantTx,
				i, nil)
			if err != nil {
				t.Fatalf("unable to calc sighash: %v", err)
			}

			gotSigHash := gotPay.Bytes
			if !bytes.Equal(wantSigHash, gotSigHash) {
				t.Fatalf("unexpected sighash. want=%x got=%x",
					wantSigHash, gotSigHash)
			}
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}

// TestConstructionParseEndpoint asserts the ConstructionParse() call works as
// expected.
func TestConstructionParseEndpoint(t *testing.T) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	// Shorter function names to improve readability.
	amt := types.DcrAmountToRosetta
	privKey := mustHex("c387ce35ba8e5d6a566f76c2cf5b055b3a01fe9fd5e5856e93aca8f6d3405696")
	pksEcdsa := "76a9144dab7c134c8b5f277b6ef9175e4d617e88d6d40088ac"
	prevHash1 := c.addSudoTx(10, mustHex(pksEcdsa)).String() // Tx needs to exist in the chain.
	addrEcdsa := "RsFRVNutxrodAcBuCLMNstc1XU1SMTWyqNo"
	expiry := uint32(2000)
	locktime := uint32(3000)
	version := uint16(3)

	// debit & debitIn define the same input.
	debit := &rtypes.Operation{
		Type:   "debit",
		Amount: amt(-10),
		Metadata: map[string]interface{}{
			"prev_tree": int8(1),
			"sequence":  uint32(1000),
		},
		Account: &rtypes.AccountIdentifier{
			Address: addrEcdsa,
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
	}
	debitIn := &wire.TxIn{
		PreviousOutPoint: wire.OutPoint{
			Hash:  mustHash(prevHash1),
			Index: 0,
			Tree:  1,
		},
		ValueIn:    10,
		BlockIndex: wire.NullBlockIndex,
		Sequence:   1000,
	}
	debitSigner := &rtypes.AccountIdentifier{
		Address: addrEcdsa,
		Metadata: map[string]interface{}{
			"script_version": 0,
		},
	}

	// credit and creditOut define the same output.
	credit := &rtypes.Operation{
		Type:   "credit",
		Amount: amt(20),
		Metadata: map[string]interface{}{
			"pk_script": pksEcdsa,
		},
		Account: &rtypes.AccountIdentifier{
			Address: addrEcdsa,
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
	}
	creditOut := &wire.TxOut{
		Value:    20,
		PkScript: mustHex(pksEcdsa),
		Version:  0,
	}

	// Debit that references a tx not found either mined or in the mempool.
	debitNoPrevTxIn := &wire.TxIn{
		PreviousOutPoint: wire.OutPoint{
			Hash: chainhash.Hash{0: 0x01},
		},
	}

	// Initialize the server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}
	svr := newTestServer(t, cfg)

	type testCase struct {
		name        string
		forceReq    *rtypes.ConstructionParseRequest
		ins         []*wire.TxIn
		outs        []*wire.TxOut
		signed      bool
		wantErr     error
		wantOps     []*rtypes.Operation
		wantSigners []*rtypes.AccountIdentifier
	}

	testCases := []testCase{{
		name:        "1 credit 1 debit unsigned",
		ins:         []*wire.TxIn{debitIn},
		outs:        []*wire.TxOut{creditOut},
		signed:      false,
		wantOps:     []*rtypes.Operation{debit, credit},
		wantSigners: []*rtypes.AccountIdentifier{},
	}, {
		name:        "1 credit 1 debit signed",
		ins:         []*wire.TxIn{debitIn},
		outs:        []*wire.TxOut{creditOut},
		signed:      true,
		wantOps:     []*rtypes.Operation{debit, credit},
		wantSigners: []*rtypes.AccountIdentifier{debitSigner},
	}, {
		name:   "3 credits 3 debits unsigned",
		ins:    []*wire.TxIn{debitIn, debitIn, debitIn},
		outs:   []*wire.TxOut{creditOut, creditOut, creditOut},
		signed: false,
		wantOps: []*rtypes.Operation{debit, debit, debit, credit,
			credit, credit},
		wantSigners: []*rtypes.AccountIdentifier{},
	}, {
		name:   "3 credits 3 debits signed",
		ins:    []*wire.TxIn{debitIn, debitIn, debitIn},
		outs:   []*wire.TxOut{creditOut, creditOut, creditOut},
		signed: true,
		wantOps: []*rtypes.Operation{debit, debit, debit, credit,
			credit, credit},
		wantSigners: []*rtypes.AccountIdentifier{debitSigner, debitSigner,
			debitSigner},
	}, {
		name:    "debit that references unknown tx",
		ins:     []*wire.TxIn{debitNoPrevTxIn},
		outs:    []*wire.TxOut{creditOut},
		signed:  false,
		wantErr: types.ErrPrevOutTxNotFound,
	}, {
		name: "invalid hex",
		forceReq: &rtypes.ConstructionParseRequest{
			Transaction: "xx",
		},
		wantErr: types.ErrInvalidHexString,
	}, {
		name: "invalid tx",
		forceReq: &rtypes.ConstructionParseRequest{
			Transaction: "0000ffff",
		},
		wantErr: types.ErrInvalidTransaction,
	}}

	// test is the actual test function.
	test := func(t *testing.T, tc *testCase) {
		t.Parallel()

		tx := &wire.MsgTx{
			Version:  version,
			Expiry:   expiry,
			LockTime: locktime,
			TxIn:     tc.ins,
			TxOut:    tc.outs,
		}
		txh := tx.TxHash()

		if tc.signed {
			// Copy the tx so we can modify the sigscript.
			tx = tx.Copy()
			for i := 0; i < len(tx.TxIn); i++ {
				sigScript, err := txscript.SignatureScript(
					tx, i, mustHex(pksEcdsa),
					txscript.SigHashAll, privKey,
					dcrec.STEcdsaSecp256k1, true)
				require.NoError(t, err)
				tx.TxIn[i].SignatureScript = sigScript
			}
		}

		txBytes, err := tx.Bytes()
		require.NoError(t, err)

		req := tc.forceReq
		if req == nil {
			req = &rtypes.ConstructionParseRequest{
				Signed:      tc.signed,
				Transaction: hex.EncodeToString(txBytes),
			}
		}
		res, rerr := svr.ConstructionParse(context.Background(), req)
		if !types.RosettaErrorIs(rerr, tc.wantErr) {
			t.Fatalf("unexpected error. want=%v got=%v",
				tc.wantErr, rerr)
		}

		if tc.wantErr != nil {
			return
		}

		if len(res.Operations) != len(tc.wantOps) {
			t.Fatalf("unexpected number of ops. want=%d got=%d",
				len(tc.wantOps), len(res.Operations))
		}

		var outIndex int
		for i := 0; i < len(tc.wantOps); i++ {
			wantOp := tc.wantOps[i]
			gotOp := res.Operations[i]

			require.Equal(t, wantOp.Status, gotOp.Status, "incorrect status")
			require.Equal(t, wantOp.Type, gotOp.Type, "incorrect type")
			require.Equal(t, wantOp.Amount.Value, gotOp.Amount.Value, "incorrect amount value")
			require.Equal(t, wantOp.CoinChange.CoinAction, gotOp.CoinChange.CoinAction, "incorrect coin action")

			// If we're checking a credit, generate the wanted
			// coinchainge identifier based on the generated tx,
			// since it could change.
			wantCCID := wantOp.CoinChange.CoinIdentifier.Identifier
			if wantOp.Type == "credit" {
				wantCCID = fmt.Sprintf("%s:%d", txh.String(), outIndex)
				outIndex++
			}
			require.Equal(t, wantCCID, gotOp.CoinChange.CoinIdentifier.Identifier, "incorrect coin change identifier")

		}

		if len(res.AccountIdentifierSigners) != len(tc.wantSigners) {
			t.Fatalf("unexpected number of signers. want=%d got=%d",
				len(tc.wantSigners), len(res.AccountIdentifierSigners))
		}

		for i := 0; i < len(res.AccountIdentifierSigners); i++ {
			wantAccount := tc.wantSigners[i]
			gotAccount := res.AccountIdentifierSigners[i]
			if wantAccount.Address != gotAccount.Address {
				t.Fatalf("unexpected address. want=%s got=%s",
					wantAccount.Address, gotAccount.Address)
			}

			wantVersion := wantAccount.Metadata["script_version"].(int)
			gotVersion := gotAccount.Metadata["script_version"].(uint16)
			if wantVersion != int(gotVersion) {
				t.Fatalf("unexpected version. want=%d got=%d",
					wantVersion, gotVersion)
			}
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}

// TestConstructionCombine ensures the ConstructionCombine() call behaves as
// expected.
func TestConstructionCombine(t *testing.T) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	// Shorter function names to improve readability.
	privKey := mustHex("c387ce35ba8e5d6a566f76c2cf5b055b3a01fe9fd5e5856e93aca8f6d3405696")
	pubKey := mustHex("0367c9d81503f8f2e5dcadce43f199073d485fd422866d7638af5a1d4134a9c429")
	pksEcdsa := "76a9144dab7c134c8b5f277b6ef9175e4d617e88d6d40088ac"
	prevHash1 := c.addSudoTx(10, mustHex(pksEcdsa)).String() // Tx needs to exist in the chain.
	expiry := uint32(2000)
	locktime := uint32(3000)
	version := uint16(3)

	pubkeySecp256k1 := &rtypes.PublicKey{
		Bytes:     pubKey,
		CurveType: rtypes.Secp256k1,
	}

	inEcdsaSecp256k1 := &wire.TxIn{
		PreviousOutPoint: wire.OutPoint{
			Hash:  mustHash(prevHash1),
			Index: 0,
			Tree:  1,
		},
		ValueIn:    10,
		BlockIndex: wire.NullBlockIndex,
		Sequence:   1000,
	}

	out := &wire.TxOut{
		Value:    20,
		PkScript: mustHex(pksEcdsa),
		Version:  0,
	}

	genSigEcdsaSecp256k1 := func(t *testing.T, tx *wire.MsgTx, i int) ([]byte, []byte) {
		sigHash, err := txscript.CalcSignatureHash(mustHex(pksEcdsa),
			txscript.SigHashAll, tx, i, nil)
		require.NoError(t, err)

		priv := secp256k1.PrivKeyFromBytes(privKey)
		rawSig := ecdsa.SignCompact(priv, sigHash, true)

		// Strip the recovery code.
		return sigHash, rawSig[1:]
	}

	genInvalidSigEcdsaSecp256k1 := func(t *testing.T, tx *wire.MsgTx, i int) ([]byte, []byte) {
		sigHash, res := genSigEcdsaSecp256k1(t, tx, i)
		res[1] ^= res[1]
		return sigHash, res
	}

	// Initialize the server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}
	svr := newTestServer(t, cfg)

	type genSig func(t *testing.T, tx *wire.MsgTx, i int) ([]byte, []byte)

	type testCase struct {
		name     string
		forceTx  string
		pubkeys  []*rtypes.PublicKey
		sigTypes []rtypes.SignatureType
		ins      []*wire.TxIn
		genSigs  []genSig
		wantErr  error
		wantSigs []bool
	}

	testCases := []testCase{{
		name:     "combine 1 secp256k1 ecdsa input",
		pubkeys:  []*rtypes.PublicKey{pubkeySecp256k1},
		sigTypes: []rtypes.SignatureType{rtypes.Ecdsa},
		ins:      []*wire.TxIn{inEcdsaSecp256k1},
		genSigs:  []genSig{genSigEcdsaSecp256k1},
		wantErr:  nil,
		wantSigs: []bool{true},
	}, {
		name:     "combine 3 secp256k1 ecdsa inputs",
		pubkeys:  []*rtypes.PublicKey{pubkeySecp256k1, pubkeySecp256k1, pubkeySecp256k1},
		sigTypes: []rtypes.SignatureType{rtypes.Ecdsa, rtypes.Ecdsa, rtypes.Ecdsa},
		ins:      []*wire.TxIn{inEcdsaSecp256k1, inEcdsaSecp256k1, inEcdsaSecp256k1},
		genSigs:  []genSig{genSigEcdsaSecp256k1, genSigEcdsaSecp256k1, genSigEcdsaSecp256k1},
		wantErr:  nil,
		wantSigs: []bool{true, true, true},
	}, {
		name:     "send incorrect nb of sigs",
		pubkeys:  []*rtypes.PublicKey{pubkeySecp256k1, pubkeySecp256k1, pubkeySecp256k1},
		sigTypes: []rtypes.SignatureType{rtypes.Ecdsa, rtypes.Ecdsa, rtypes.Ecdsa},
		ins:      []*wire.TxIn{inEcdsaSecp256k1, inEcdsaSecp256k1, inEcdsaSecp256k1},
		genSigs:  []genSig{genSigEcdsaSecp256k1, genSigEcdsaSecp256k1},
		wantErr:  types.ErrIncorrectSigCount,
	}, {
		name:     "send unknown sig type",
		pubkeys:  []*rtypes.PublicKey{pubkeySecp256k1},
		sigTypes: []rtypes.SignatureType{"***"},
		ins:      []*wire.TxIn{inEcdsaSecp256k1},
		genSigs:  []genSig{genSigEcdsaSecp256k1},
		wantErr:  types.ErrUnsupportedSignatureType,
	}, {
		name:     "send invalid sig",
		pubkeys:  []*rtypes.PublicKey{pubkeySecp256k1},
		sigTypes: []rtypes.SignatureType{rtypes.Ecdsa},
		ins:      []*wire.TxIn{inEcdsaSecp256k1},
		genSigs:  []genSig{genInvalidSigEcdsaSecp256k1},
		wantErr:  types.ErrInvalidSig,
		wantSigs: []bool{true},
	}, {
		name:    "invalid hex string",
		forceTx: "xx",
		wantErr: types.ErrInvalidHexString,
	}, {
		name:    "invalid tx",
		forceTx: "0000ffff",
		wantErr: types.ErrInvalidTransaction,
	}}

	test := func(t *testing.T, tc *testCase) {
		t.Parallel()

		tx := &wire.MsgTx{
			Version:  version,
			Expiry:   expiry,
			LockTime: locktime,
			TxIn:     tc.ins,
			TxOut:    []*wire.TxOut{out},
		}

		var sigs []*rtypes.Signature
		for i := 0; i < len(tc.genSigs); i++ {
			sigHash, sigBytes := tc.genSigs[i](t, tx, i)
			sig := &rtypes.Signature{
				SigningPayload: &rtypes.SigningPayload{
					Bytes: sigHash,
				},
				PublicKey:     tc.pubkeys[i],
				SignatureType: tc.sigTypes[i],
				Bytes:         sigBytes,
			}
			sigs = append(sigs, sig)
		}

		txBytes, err := tx.Bytes()
		require.NoError(t, err)
		req := &rtypes.ConstructionCombineRequest{
			UnsignedTransaction: hex.EncodeToString(txBytes),
			Signatures:          sigs,
		}

		if tc.forceTx != "" {
			req.UnsignedTransaction = tc.forceTx
		}

		res, rerr := svr.ConstructionCombine(context.Background(), req)
		if !types.RosettaErrorIs(rerr, tc.wantErr) {
			t.Fatalf("unexpected error. want=%v got=%v",
				tc.wantErr, rerr)
		}

		if tc.wantErr != nil {
			return
		}

		var gotTx wire.MsgTx
		if err := gotTx.FromBytes(mustHex(res.SignedTransaction)); err != nil {
			t.Fatalf("unexpected error decoding resulting tx: %v", err)
		}
		if len(gotTx.TxIn) != len(tx.TxIn) {
			t.Fatalf("unexpected number of inputs. want=%d got=%d",
				len(tx.TxIn), len(gotTx.TxIn))
		}
		for i := 0; i < len(gotTx.TxIn); i++ {
			gotSig := len(gotTx.TxIn[i].SignatureScript) > 0
			if tc.wantSigs[i] != gotSig {
				t.Fatalf("unexpected sig %d. want=%v got=%v", i,
					tc.wantSigs[i], gotSig)
			}
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}

// TestConstructionHashEndpoint ensures the ConstructionHash() call works as
// expected.
func TestConstructionHashEndpoint(t *testing.T) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	dummyTx := wire.NewMsgTx()
	dummyTx.AddTxIn(wire.NewTxIn(&wire.OutPoint{}, 0, nil))
	dummyTx.AddTxOut(wire.NewTxOut(0, nil))
	dummyTxh := dummyTx.TxHash()
	dummyTxBytes, err := dummyTx.Bytes()
	require.NoError(t, err)
	dummyTxHex := hex.EncodeToString(dummyTxBytes)

	// Initialize the server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}

	svr := newTestServer(t, cfg)

	type testCase struct {
		name       string
		tx         string
		wantErr    error
		wantTxHash string
	}

	testCases := []testCase{{
		name:       "valid tx",
		tx:         dummyTxHex,
		wantErr:    nil,
		wantTxHash: dummyTxh.String(),
	}, {
		name:    "invalid hex",
		tx:      dummyTxHex + "x",
		wantErr: types.ErrInvalidHexString,
	}, {
		name:    "invalid tx",
		tx:      dummyTxHex[:4] + "ffff" + dummyTxHex[8:], // Break sertype.
		wantErr: types.ErrInvalidTransaction,
	}}

	test := func(t *testing.T, tc *testCase) {
		t.Parallel()

		req := &rtypes.ConstructionHashRequest{
			SignedTransaction: tc.tx,
		}
		res, rerr := svr.ConstructionHash(context.Background(), req)

		if !types.RosettaErrorIs(rerr, tc.wantErr) {
			t.Fatalf("unexpected error. want=%v got=%v", tc.wantErr,
				rerr)
		}

		if tc.wantErr != nil {
			return
		}

		gotHash := res.TransactionIdentifier.Hash
		if tc.wantTxHash != gotHash {
			t.Fatalf("unexpected hash. want=%s got=%s", tc.wantTxHash,
				gotHash)
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}

// TestConstructionSubmitEndpoint verifies the ConstructionSubmit() call works
// as expected.
func TestConstructionSubmitEndpoint(t *testing.T) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	dummyTx := wire.NewMsgTx()
	dummyTx.AddTxIn(wire.NewTxIn(&wire.OutPoint{}, 0, nil))
	dummyTx.AddTxOut(wire.NewTxOut(0, nil))
	dummyTxh := dummyTx.TxHash()
	dummyTxBytes, err := dummyTx.Bytes()
	require.NoError(t, err)
	dummyTxHex := hex.EncodeToString(dummyTxBytes)

	// Initialize the server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}
	svr := newTestServer(t, cfg)

	type testCase struct {
		name       string
		tx         string
		submitErr  error
		wantErr    error
		wantTxHash string
	}

	testCases := []testCase{{
		name:       "valid tx",
		tx:         dummyTxHex,
		submitErr:  nil,
		wantErr:    nil,
		wantTxHash: dummyTxh.String(),
	}, {
		name:    "invalid hex",
		tx:      dummyTxHex + "x",
		wantErr: types.ErrInvalidHexString,
	}, {
		name:    "invalid tx",
		tx:      dummyTxHex[:4] + "ffff" + dummyTxHex[8:], // Break sertype.
		wantErr: types.ErrInvalidTransaction,
	}, {
		name:      "tx already exists in mempool",
		tx:        dummyTxHex,
		submitErr: &dcrjson.RPCError{Code: dcrjson.ErrRPCDuplicateTx},
		wantErr:   types.ErrAlreadyHaveTx,
	}, {
		name:      "tx already exists mined",
		tx:        dummyTxHex,
		submitErr: &dcrjson.RPCError{Code: dcrjson.ErrRPCMisc, Message: "transaction already exists"},
		wantErr:   types.ErrTxAlreadyMined,
	}, {
		name:      "rule error while processing tx",
		tx:        dummyTxHex,
		submitErr: &dcrjson.RPCError{Code: dcrjson.ErrRPCMisc},
		wantErr:   types.ErrProcessingTx,
	}, {
		name:      "generic RPC json error",
		tx:        dummyTxHex,
		submitErr: &dcrjson.RPCError{},
		wantErr:   types.ErrUnknown,
	}, {
		name:      "generic send error",
		tx:        dummyTxHex,
		submitErr: errors.New("foo"),
		wantErr:   types.ErrUnknown,
	}}

	test := func(t *testing.T, tc *testCase) {
		c.sendRawTransactionHook = func(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error) {
			return &dummyTxh, tc.submitErr
		}

		req := &rtypes.ConstructionSubmitRequest{
			SignedTransaction: tc.tx,
		}
		res, rerr := svr.ConstructionSubmit(context.Background(), req)

		if !types.RosettaErrorIs(rerr, tc.wantErr) {
			t.Fatalf("unexpected error. want=%v got=%v", tc.wantErr,
				rerr)
		}

		if tc.wantErr != nil {
			return
		}

		gotHash := res.TransactionIdentifier.Hash
		if tc.wantTxHash != gotHash {
			t.Fatalf("unexpected hash. want=%s got=%s", tc.wantTxHash,
				gotHash)
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}
