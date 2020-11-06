// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"
	"errors"
	"testing"

	"decred.org/dcrros/types"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/wire"
	"github.com/stretchr/testify/require"
)

// TestMempoolEndpoint asserts the Mempool() call works as expected.
func TestMempoolEndpoint(t *testing.T) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	// Initialize the server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}
	svr := newTestServer(t, cfg)

	// Create a few dummy transactions.
	tx1 := c.addSudoTx(1, []byte{})
	tx2 := c.addSudoTx(2, []byte{})
	tx3 := c.addSudoTx(3, []byte{})

	type testCase struct {
		name       string
		mempoolTxs []*chainhash.Hash
		mempoolErr error
		wantErr    error
	}

	testCases := []testCase{{
		name:       "no mempool txs",
		mempoolTxs: []*chainhash.Hash{},
	}, {
		name:       "3 mempool txs",
		mempoolTxs: []*chainhash.Hash{&tx1, &tx2, &tx3},
	}, {
		name:       "mempool error",
		mempoolErr: errors.New("foo"),
		wantErr:    types.ErrUnknown,
	}}

	test := func(t *testing.T, tc *testCase) {
		// Hook into the getRawMempool call to return the previously created
		// txs.
		c.getRawMempoolHook = func(ctx context.Context, txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error) {
			return tc.mempoolTxs, tc.mempoolErr
		}

		// Call the Mempool() function.
		res, rerr := svr.Mempool(context.Background(), nil)
		if !types.RosettaErrorIs(rerr, tc.wantErr) {
			t.Fatalf("unexpected error. want=%v got=%v", tc.wantErr, rerr)
		}
		if tc.wantErr != nil {
			return
		}

		// Assert the returned transactions are correct.
		txs := res.TransactionIdentifiers
		if len(txs) != len(tc.mempoolTxs) {
			t.Fatalf("unexpected number of txs. want=%d got=%d",
				len(tc.mempoolTxs), len(txs))
		}

		for i := range txs {
			if txs[i].Hash != tc.mempoolTxs[i].String() {
				t.Fatalf("unexpected tx hash %d. want=%s got=%s",
					i, tc.mempoolTxs[i], txs[i].Hash)
			}
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}

// TestMempoolTransactionEndpoint asserts the MempoolTransaction() call works
// as expected.
func TestMempoolTransactionEndpoint(t *testing.T) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	// Initialize the server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}
	svr := newTestServer(t, cfg)

	// Create a transaction which input we'll spend in the mempool. This is
	// needed because during processing all inputs for a mempool tx are
	// fetched from the blockchain.
	fundingTx := c.addSudoTx(1, []byte{})

	// Create a mempool tx and add it to the mock chain.
	tx := wire.NewMsgTx()
	tx.AddTxIn(wire.NewTxIn(&wire.OutPoint{Hash: fundingTx}, 0, nil))
	tx.AddTxOut(wire.NewTxOut(2, []byte{}))
	mempoolTx := tx.TxHash()
	c.txByHash[mempoolTx] = tx

	// Hook into the getRawMempool call to return the previously created
	// txs.
	c.getRawMempoolHook = func(ctx context.Context, txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error) {
		return []*chainhash.Hash{&mempoolTx}, nil
	}

	type testCase struct {
		name    string
		reqHash string
		wantErr error
	}

	testCases := []testCase{{
		name:    "correct hash for mempool tx",
		reqHash: mempoolTx.String(),
	}, {
		name:    "invalid hash",
		reqHash: "xx",
		wantErr: types.ErrInvalidChainHash,
	}, {
		name:    "unknown tx",
		reqHash: chainhash.Hash{}.String(),
		wantErr: types.ErrTxNotFound,
	}}

	test := func(t *testing.T, tc *testCase) {
		// Call the MempoolTransaction() function.
		req := &rtypes.MempoolTransactionRequest{
			TransactionIdentifier: &rtypes.TransactionIdentifier{
				Hash: tc.reqHash,
			},
		}
		res, rerr := svr.MempoolTransaction(context.Background(), req)
		if !types.RosettaErrorIs(rerr, tc.wantErr) {
			t.Fatalf("unexpected error. want=%v, got=%v", tc.wantErr,
				rerr)
		}
		if tc.wantErr != nil {
			return
		}

		gotHash := res.Transaction.TransactionIdentifier.Hash
		if gotHash != mempoolTx.String() {
			t.Fatalf("unexpected mempool tx hash. want=%s got=%s",
				mempoolTx, gotHash)
		}

		// Asking for a tx that does not exist in the mempool should return an
		// error.
		req.TransactionIdentifier.Hash = fundingTx.String()
		_, rerr = svr.MempoolTransaction(context.Background(), req)
		require.NotNil(t, rerr)
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}

}
