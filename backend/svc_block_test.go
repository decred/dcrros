// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"
	"errors"
	"testing"

	"decred.org/dcrros/backend/backenddb"
	"decred.org/dcrros/types"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
	"github.com/stretchr/testify/require"
)

func testBlockEndpoint(t *testing.T, db backenddb.DB) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	// Shorter function names to improve readability.
	disapprove := func(b *wire.MsgBlock) {
		b.Header.VoteBits = 0
	}
	extendTip := func(manglers ...blockMangler) (*chainhash.Hash, int64) {
		c.mtx.Lock()
		c.extendTip(manglers...)
		c.mtx.Unlock()
		hash := c.tipHash
		return &hash, c.tipHeight
	}
	reqByHash := func(hash *chainhash.Hash) *rtypes.BlockRequest {
		s := hash.String()
		return &rtypes.BlockRequest{
			BlockIdentifier: &rtypes.PartialBlockIdentifier{
				Hash: &s,
			},
		}
	}
	reqByHashString := func(hash string) *rtypes.BlockRequest {
		return &rtypes.BlockRequest{
			BlockIdentifier: &rtypes.PartialBlockIdentifier{
				Hash: &hash,
			},
		}
	}
	reqByIndex := func(index int64) *rtypes.BlockRequest {
		return &rtypes.BlockRequest{
			BlockIdentifier: &rtypes.PartialBlockIdentifier{
				Index: &index,
			},
		}
	}

	// Generate a test chain.
	extendTip()
	extendTip()
	disapprovedHash, disapprovedHeight := extendTip()
	disapprovingHash, disapprovingHeight := extendTip(disapprove)
	extendTip()
	_, lastHeight := extendTip()

	// Initialize the server and process the blockchain.
	cfg := &ServerConfig{
		ChainParams:     params,
		DBType:          dbTypePreconfigured,
		CacheSizeBlocks: 2,
		c:               c,
		db:              db,
	}
	svr := newTestServer(t, cfg)
	err := svr.preProcessAccounts(testCtx(t))
	require.NoError(t, err)

	// Generate a block on the chain that isn't processed.
	extendTip()

	type testCase struct {
		name           string
		req            *rtypes.BlockRequest
		wantErr        error
		wantChainBlock int64
	}

	testCases := []testCase{{
		name:           "genesis by hash",
		req:            reqByHash(&params.GenesisHash),
		wantChainBlock: 0,
	}, {
		name:           "genesis by height",
		req:            reqByIndex(0),
		wantChainBlock: 0,
	}, {
		name:           "block one by hash",
		req:            reqByHash(c.blocksByHeight[1]),
		wantChainBlock: 1,
	}, {
		name:           "block one by height",
		req:            reqByIndex(1),
		wantChainBlock: 1,
	}, {
		name:           "disapproved block by hash",
		req:            reqByHash(disapprovedHash),
		wantChainBlock: disapprovedHeight,
	}, {
		name:           "disapproved block by height",
		req:            reqByIndex(disapprovedHeight),
		wantChainBlock: disapprovedHeight,
	}, {
		name:           "disapproving block by hash",
		req:            reqByHash(disapprovingHash),
		wantChainBlock: disapprovingHeight,
	}, {
		name:           "disapproving block by height",
		req:            reqByIndex(disapprovingHeight),
		wantChainBlock: disapprovingHeight,
	}, {
		name:           "last processed with empty block identifier",
		req:            &rtypes.BlockRequest{},
		wantChainBlock: lastHeight,
	}, {
		name:           "last processed with block identifier",
		req:            &rtypes.BlockRequest{BlockIdentifier: &rtypes.PartialBlockIdentifier{}},
		wantChainBlock: lastHeight,
	}, {
		name:    "block higher than chain tip",
		req:     reqByIndex(c.tipHeight * 2),
		wantErr: types.ErrBlockIndexAfterTip,
	}, {
		name:           "block that exists in chain but wasn't processed",
		req:            reqByHash(&c.tipHash),
		wantChainBlock: c.tipHeight,
	}, {
		name:    "block that exists in chain but wasn't processed by height",
		req:     reqByIndex(lastHeight + 1),
		wantErr: types.ErrBlockIndexAfterTip,
	}, {
		name:    "negative block",
		req:     reqByIndex(-1),
		wantErr: types.ErrBlockIndexAfterTip,
	}, {
		name:    "inexistent block by hash",
		req:     reqByHash(&chainhash.Hash{0: 0xff}),
		wantErr: types.ErrBlockNotFound,
	}, {
		name:    "invalid block hash",
		req:     reqByHashString("xx"),
		wantErr: types.ErrInvalidChainHash,
	}}

	// Actual test function.
	test := func(t *testing.T, tc *testCase) {
		//t.Parallel()
		res, rerr := svr.Block(context.Background(), tc.req)
		if !types.RosettaErrorIs(rerr, tc.wantErr) {
			t.Fatalf("unexpected error. want=%v got=%v",
				tc.wantErr, rerr)
		}

		if tc.wantErr != nil {
			// No need to continue testing if we exepceted an
			// error.
			return
		}

		c.mtx.Lock()
		wantBlock := c.blocks[*c.blocksByHeight[tc.wantChainBlock]]
		c.mtx.Unlock()
		gotBlock := res.Block
		if gotBlock.BlockIdentifier.Hash != wantBlock.Header.BlockHash().String() {
			t.Fatalf("unexpected block hash. want=%s got=%s",
				wantBlock.BlockHash(), gotBlock.BlockIdentifier.Hash)
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}

// TestBlockEndpoint asserts the Block() Rosetta endpoint behaves correctly.
// This doesn't (currently) test the contents itself of the conversion, since
// that's done by the types package and unit tested there.
func TestBlockEndpoint(t *testing.T) {
	testDbInstances(t, true, testBlockEndpoint)
}

// TestPrevOutsFetcher verifies the PrevOutsFetcher function behaves as
// expected.
func TestPrevOutsFetcher(t *testing.T) {
	t.Parallel()

	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	// Add a tx to the chain so we can verify it exists. We'll add an
	// additional tx out to it in order to do some tests.
	addTxOut := func(tx *wire.MsgTx) {
		tx.AddTxOut(wire.NewTxOut(10, []byte{}))
	}
	txHash := c.addSudoTx(10, []byte{}, addTxOut)
	correctOutPoint0 := wire.OutPoint{Hash: txHash, Index: 0}
	correctOutPoint1 := wire.OutPoint{Hash: txHash, Index: 1}
	txHash2 := c.addSudoTx(10, []byte{})
	correctOutPointTx2 := wire.OutPoint{Hash: txHash2, Index: 0}
	wrongIndexOutPoint := wire.OutPoint{Hash: txHash, Index: 100}
	wrongHashOutPoint := wire.OutPoint{Hash: chainhash.Hash{0: 0x01}, Index: 0}

	// Initialize the server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}
	svr := newTestServer(t, cfg)

	type testCase struct {
		name         string
		utxoSet      map[wire.OutPoint]*types.PrevOutput
		reqOutPoints []*wire.OutPoint
		forceErr     error
		wantErr      error
	}

	dummyErr := errors.New("boo")
	testCases := []testCase{{
		name:         "prevout exists in chain",
		reqOutPoints: []*wire.OutPoint{&correctOutPoint0},
	}, {
		name: "prevout exists in utxo set",
		utxoSet: map[wire.OutPoint]*types.PrevOutput{
			correctOutPoint0: nil,
		},
		reqOutPoints: []*wire.OutPoint{&correctOutPoint0},
	}, {
		name:         "same prevout requested twice",
		reqOutPoints: []*wire.OutPoint{&correctOutPoint0, &correctOutPoint0},
	}, {
		name:         "different prevouts from same tx in chain",
		reqOutPoints: []*wire.OutPoint{&correctOutPoint0, &correctOutPoint1},
	}, {
		name:         "different prevouts from same tx in utxo set",
		reqOutPoints: []*wire.OutPoint{&correctOutPoint0, &correctOutPoint1},
		utxoSet: map[wire.OutPoint]*types.PrevOutput{
			correctOutPoint0: nil,
			correctOutPoint1: nil,
		},
	}, {
		name:         "different prevouts from same tx partially in utxo set",
		reqOutPoints: []*wire.OutPoint{&correctOutPoint0, &correctOutPoint1},
		utxoSet: map[wire.OutPoint]*types.PrevOutput{
			correctOutPoint0: nil,
		},
	}, {
		name: "different prevouts partially in utxo set",
		reqOutPoints: []*wire.OutPoint{&correctOutPoint0,
			&correctOutPoint1, &correctOutPointTx2},
		utxoSet: map[wire.OutPoint]*types.PrevOutput{
			correctOutPointTx2: nil,
		},
	}, {
		name:         "requested tx with wrong hash",
		reqOutPoints: []*wire.OutPoint{&correctOutPoint0, &wrongHashOutPoint},
		wantErr:      types.ErrPrevOutTxNotFound,
	}, {
		name:         "requested tx with wrong index",
		reqOutPoints: []*wire.OutPoint{&correctOutPoint0, &wrongIndexOutPoint},
		wantErr:      types.ErrPrevOutIndexNotFound,
	}, {
		name:         "force error on getRawTransaction",
		reqOutPoints: []*wire.OutPoint{&correctOutPoint0},
		forceErr:     dummyErr,
		wantErr:      dummyErr,
	}}

	test := func(t *testing.T, tc *testCase) {
		// Hook into the getRawTx function in case we need to return a
		// specific error.
		c.getRawTransactionHook = func(ctx context.Context, txHash *chainhash.Hash) (*dcrutil.Tx, error) {
			if tc.forceErr != nil {
				return nil, tc.forceErr
			}
			return c.getRawTransaction(ctx, txHash)
		}

		res, err := svr.prevOutsFetcher(testCtx(t), tc.utxoSet,
			tc.reqOutPoints...)
		if !errors.Is(err, tc.wantErr) {
			t.Fatalf("unexpected error. want=%v, got=%v",
				tc.wantErr, err)
		}
		if tc.wantErr != nil {
			return
		}

		// Ensure we got all requested outpoints.
		gotPrevOuts := make(map[wire.OutPoint]struct{})
		for outp := range res {
			gotPrevOuts[outp] = struct{}{}
		}
		for _, wantOutp := range tc.reqOutPoints {
			if _, ok := gotPrevOuts[*wantOutp]; !ok {
				t.Fatalf("outpoint %s not returned", wantOutp)
			}
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}
