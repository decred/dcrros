// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"
	"strconv"
	"testing"
	"time"

	"decred.org/dcrros/backend/backenddb"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/wire"
	"github.com/stretchr/testify/require"
)

// TestProcessSequentialBlocks ensures the processSequentialBlocks function
// behaves correctly even when blocks are fetched out of order.
func TestProcessSequentialBlocks(t *testing.T) {
	t.Parallel()

	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	concurrency := 4
	for i := 0; i < concurrency*2; i++ {
		c.extendTip()
	}

	// Initialize server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}
	svr := newTestServer(t, cfg)

	// Override concurrency to ensure multiple blocks are requested
	// concurrently during preprocessing.
	svr.concurrency = concurrency

	// Hook into GetBlockHash. We'll delay returning the block hash (and
	// therefore delay block fetching) by "inverse" mod concurrency
	// milliseconds (i.e. higher blocks are delayed less than lower blocks,
	// so blocks are retrieved out-of-order).
	c.getBlockHashHook = func(ctx context.Context, blockHeight int64) (*chainhash.Hash, error) {
		// Unlock the mock chain mutex to allow faster goroutines to
		// complete first.
		c.mtx.Unlock()

		// Wait an amount such that higher blocks are delayed less than
		// lower blocks.
		wait := time.Duration(concurrency-(int(blockHeight)%concurrency)) * 50 * time.Millisecond
		time.Sleep(wait)

		// Lock again since getBlockHash expexts a locked chain.
		c.mtx.Lock()
		return c.getBlockHash(ctx, blockHeight)
	}

	var wantBlock uint32
	f := func(bh *chainhash.Hash, b *wire.MsgBlock) error {
		if b.Header.Height != wantBlock {
			t.Fatalf("unexpected block. want=%d got=%d",
				wantBlock, b.Header.Height)
		}
		wantBlock++
		return nil
	}

	svr.processSequentialBlocks(testCtx(t), 0, f)
}

func testHandlesBlockNotifications(t *testing.T, db backenddb.DB) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	pks := mustHex("76a91464098ee97e822d8b98e93608c4f0cdbb559146fa88ac")
	addr := "RsHTkwUoDYKh3SJCj1FSoWbKy1P2DA6DHA5"

	// Shorter function names to improve readability.
	toAddr := func(v, nb int64) blockMangler {
		return c.txToAddr(pks, v, nb)
	}
	fromAddr := func(v, nb int64) blockMangler {
		return c.txFromAddr(pks, v, nb)
	}
	disapprove := func(b *wire.MsgBlock) {
		b.Header.VoteBits = 0
	}

	// Initialize server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
		db:          db,
	}
	svr := newTestServer(t, cfg)

	// Helpful functions to drive the server forward.
	tipHeader := func() *wire.BlockHeader {
		return &c.blocks[c.tipHash].Header
	}
	extendTip := func(manglers ...blockMangler) *wire.BlockHeader {
		c.mtx.Lock()
		defer c.mtx.Unlock()
		c.extendTip(manglers...)
		return tipHeader()
	}
	rewindTip := func() *wire.BlockHeader {
		c.mtx.Lock()
		defer c.mtx.Unlock()
		c.rewindChain(1)
		return &c.blocks[c.tipHash].Header
	}
	connectHeader := func(h *wire.BlockHeader) {
		err := svr.handleBlockConnected(testCtx(t), h)
		if err != nil {
			t.Fatalf("unexpected error while connecting header: %v", err)
		}
	}
	disconnectHeader := func(h *wire.BlockHeader) {
		err := svr.handleBlockDisconnected(testCtx(t), h)
		if err != nil {
			t.Fatalf("unexpected error while disconnecting header: %v", err)
		}
	}
	connectTip := func() {
		connectHeader(tipHeader())
	}
	assertServerTip := func(wantTip chainhash.Hash) {
		gotTip, _, err := svr.lastProcessedBlock(testCtx(t))
		require.NoError(t, err)
		if *gotTip != wantTip {
			t.Fatalf("unexpected tip. want=%s got=%s", wantTip, gotTip)
		}
	}

	// Generate a chain that funds an account.
	c.extendTip(toAddr(11, 1))
	c.extendTip(fromAddr(5, 1))

	// Preprocess.
	err := svr.preProcessAccounts(testCtx(t))
	require.NoError(t, err)

	// Account balance should be correct.
	assertBalanceAtTip(c, svr, addr, 6)

	// Define the test cases.
	testCases := []struct {
		name        string
		test        func()
		wantBalance int64
	}{{
		name: "connect single block",
		test: func() {
			c.mtx.Lock()
			c.extendTip(toAddr(7, 1))
			c.mtx.Unlock()
			connectTip()
		},
		wantBalance: 13,
	}, {
		name: "connect multiple blocks",
		test: func() {
			h1 := extendTip(toAddr(9, 1))
			h2 := extendTip(fromAddr(11, 1))
			connectHeader(h1)
			connectHeader(h2)
		},
		wantBalance: 11,
	}, {
		name: "connect then disconnect block",
		test: func() {
			oldTip := c.tipHash
			h1 := extendTip(toAddr(23, 1))
			connectHeader(h1)
			disconnectHeader(h1)
			c.rewindChain(1)
			assertServerTip(oldTip)
		},
		wantBalance: 11,
	}, {
		name: "connect then disconnect multiple blocks",
		test: func() {
			oldTip := c.tipHash
			h1 := extendTip(toAddr(23, 1))
			h2 := extendTip(toAddr(23, 1))
			h3 := extendTip(toAddr(23, 1))
			connectHeader(h1)
			connectHeader(h2)
			connectHeader(h3)
			assertServerTip(h3.BlockHash())

			rewindTip()
			disconnectHeader(h3)
			assertServerTip(h2.BlockHash())

			rewindTip()
			disconnectHeader(h2)
			assertServerTip(h1.BlockHash())

			rewindTip()
			disconnectHeader(h1)
			assertServerTip(oldTip)
		},
		wantBalance: 11,
	}, {
		name: "perform a reorg",
		test: func() {
			h1 := extendTip(toAddr(23, 1))
			h2 := extendTip(toAddr(23, 1))
			h3 := extendTip(toAddr(23, 1))
			connectHeader(h1)
			connectHeader(h2)
			connectHeader(h3)
			assertServerTip(h3.BlockHash())

			rewindTip()
			disconnectHeader(h3)
			rewindTip()
			disconnectHeader(h2)

			h3b := extendTip(fromAddr(5, 1))
			connectHeader(h3b)
			assertServerTip(h3b.BlockHash())
		},
		wantBalance: 29,
	}, {
		name: "send a later header and wait for catchup",
		test: func() {
			_ = extendTip(toAddr(3, 1))
			_ = extendTip(toAddr(3, 1))
			h3 := extendTip(toAddr(3, 1))
			connectHeader(h3)
			assertServerTip(h3.BlockHash())
		},
		wantBalance: 38,
	}, {
		name: "send a later header with a reorg and wait for catchup",
		test: func() {
			h1 := extendTip(toAddr(3, 1))
			_ = extendTip(toAddr(3, 1))
			_ = extendTip(toAddr(3, 1))
			connectHeader(h1)

			rewindTip()
			rewindTip()
			rewindTip()
			_ = extendTip(toAddr(16, 1))
			_ = extendTip(disapprove)
			h3b := extendTip(fromAddr(5, 1))
			_ = extendTip() // Does not catch up to this.

			connectHeader(h3b)
			assertServerTip(h3b.BlockHash())
		},
		wantBalance: 33,
	}, {
		name: "connect header from inexistent block",
		test: func() {
			header := wire.BlockHeader{
				Height: uint32(c.tipHeight) + 1,
			}
			h1 := extendTip(toAddr(3, 1))
			connectHeader(h1)

			err := svr.handleBlockConnected(testCtx(t), &header)
			if err == nil {
				t.Fatalf("expected error but got nil")
			}
			assertServerTip(h1.BlockHash())
		},
		wantBalance: 36,
	}, {
		name: "disconnect non tip block",
		test: func() {
			h1 := extendTip(toAddr(3, 1))
			connectHeader(h1)

			// Disconnecting an non-tip block doesn't error but
			// also doesn't modify the tip.
			var header wire.BlockHeader
			disconnectHeader(&header)
			assertServerTip(h1.BlockHash())
		},
		wantBalance: 39,
	}, {
		name: "connect a block that was already connected",
		test: func() {
			h1 := extendTip(toAddr(3, 1))
			h2 := extendTip(fromAddr(19, 1))
			connectHeader(h1)
			connectHeader(h2)
			connectHeader(h1)
		},
		wantBalance: 23,
	}}

	for _, tc := range testCases {
		t.Logf(tc.name)
		tc.test()

		// Fetch the server's tip balance.
		req := &rtypes.AccountBalanceRequest{
			AccountIdentifier: &rtypes.AccountIdentifier{
				Address: addr,
				Metadata: map[string]interface{}{
					"script_version": uint16(0),
				},
			},
		}
		res, rerr := svr.AccountBalance(testCtx(t), req)
		require.Nil(t, rerr)

		gotBalance, err := strconv.ParseInt(res.Balances[0].Value, 10, 64)
		require.NoError(t, err)

		if gotBalance != tc.wantBalance {
			t.Fatalf("balance mismatch. want=%d got=%d", tc.wantBalance,
				gotBalance)
		}
	}
}

// TestHandlesBlockNotifications ensures that block connected/disconnected
// notifications from the underlying chain instance behave correctly.
func TestHandlesBlockNotifications(t *testing.T) {
	testDbInstances(t, true, testHandlesBlockNotifications)
}

func testTracksAccountUtxos(t *testing.T, db backenddb.DB) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)
	var svr *Server

	pks := mustHex("76a91464098ee97e822d8b98e93608c4f0cdbb559146fa88ac")
	addr := "RsHTkwUoDYKh3SJCj1FSoWbKy1P2DA6DHA5"

	tipHeader := func() *wire.BlockHeader {
		return &c.blocks[c.tipHash].Header
	}
	extendTip := func(manglers ...blockMangler) *wire.BlockHeader {
		c.mtx.Lock()
		defer c.mtx.Unlock()
		c.extendTip(manglers...)
		return tipHeader()
	}
	rewindTip := func() *wire.BlockHeader {
		c.mtx.Lock()
		defer c.mtx.Unlock()
		c.rewindChain(1)
		return &c.blocks[c.tipHash].Header
	}
	connectHeader := func(h *wire.BlockHeader) {
		err := svr.handleBlockConnected(testCtx(t), h)
		if err != nil {
			t.Fatalf("unexpected error while connecting header: %v", err)
		}
	}
	connectTip := func() {
		connectHeader(tipHeader())
	}
	extendAndConnect := func(manglers ...blockMangler) {
		extendTip(manglers...)
		connectTip()
	}
	disapprove := func(b *wire.MsgBlock) {
		b.Header.VoteBits = 0
	}

	// wantUtxos tracks which utxos we expect the server to know.
	type utxo struct {
		outp wire.OutPoint
		amt  int64
	}
	wantUtxos := make(map[string]*utxo)

	// Shorter function names to improve readability.
	createUtxo := func(value, nb int64, tree int8) blockMangler {
		prevHash := c.addSudoTx(0, []byte{})
		tx := wire.NewMsgTx()
		tx.AddTxIn(wire.NewTxIn(&wire.OutPoint{Hash: prevHash}, value*nb, nil))
		for i := int64(0); i < nb; i++ {
			tx.AddTxOut(wire.NewTxOut(value, pks))
		}
		txh := tx.TxHash()
		for i := 0; i < int(nb); i++ {
			outp := wire.OutPoint{Hash: txh, Index: uint32(i), Tree: tree}
			// Sanity check to ensure we're not storing duplicate
			// utxos which indicates something is wrong with the
			// test code.
			if _, ok := wantUtxos[outp.String()]; ok {
				t.Fatalf("test premise broken: duplicate utxo created")
			}
			wantUtxos[outp.String()] = &utxo{outp: outp, amt: value}
		}
		return func(b *wire.MsgBlock) {
			switch tree {
			case 0:
				b.Transactions = append(b.Transactions, tx)
			case 1:
				b.STransactions = append(b.STransactions, tx)
			}
		}
	}
	spendUtxo := func(tree int8) blockMangler {
		// Select a random utxo.
		var utxo *utxo
		for _, utxo = range wantUtxos {
			break
		}
		tx := wire.NewMsgTx()
		tx.AddTxIn(wire.NewTxIn(&utxo.outp, utxo.amt, nil))
		tx.AddTxOut(wire.NewTxOut(utxo.amt, []byte{}))
		delete(wantUtxos, utxo.outp.String())
		return func(b *wire.MsgBlock) {
			switch tree {
			case 0:
				b.Transactions = append(b.Transactions, tx)
			case 1:
				b.STransactions = append(b.STransactions, tx)
			}
		}
	}
	copyWantUtxos := func() map[string]*utxo {
		cp := make(map[string]*utxo, len(wantUtxos))
		for k, v := range wantUtxos {
			cp[k] = v
		}
		return cp
	}

	// checkUtxos compares the contents of the wantUtxos var against what
	// the server currently stores as utxos for the account.
	checkUtxos := func() {
		t.Helper()

		req := &rtypes.AccountCoinsRequest{
			AccountIdentifier: &rtypes.AccountIdentifier{
				Address: addr,
				Metadata: map[string]interface{}{
					"script_version": uint16(0),
				},
			},
		}

		res, rerr := svr.AccountCoins(testCtx(t), req)
		require.Nil(t, rerr)

		if len(res.Coins) != len(wantUtxos) {
			t.Fatalf("unexpected nb of coins. want=%d got=%d",
				len(wantUtxos), len(res.Coins))
		}
		wantUtxosKeys := make(map[string]struct{}, len(wantUtxos))
		for k := range wantUtxos {
			wantUtxosKeys[k] = struct{}{}
		}
		for _, gotCoin := range res.Coins {
			gotOutp := gotCoin.CoinIdentifier.Identifier
			wantUtxo, ok := wantUtxos[gotOutp]
			if !ok {
				t.Fatalf("could not find returned coin id %s",
					gotCoin.CoinIdentifier.Identifier)
			}
			gotAmt, err := strconv.ParseInt(gotCoin.Amount.Value, 10, 64)
			require.NoError(t, err)

			if gotAmt != wantUtxo.amt {
				t.Fatalf("unexpected utxo amount. want=%d got=%d",
					wantUtxo.amt, gotAmt)
			}
			delete(wantUtxosKeys, gotOutp)
		}
		if len(wantUtxosKeys) > 0 {
			t.Fatalf("some utxos were not returned by the server: %v", wantUtxosKeys)
		}
	}

	// Initialize server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
		db:          db,
	}
	svr = newTestServer(t, cfg)

	// Start by extending the tip and creating an outpoint, then adding
	// dummy blocks to ensure preprocessing works.
	extendTip(createUtxo(11, 1, 0))
	extendTip(createUtxo(12, 1, 1))
	extendTip(spendUtxo(0), createUtxo(13, 1, 1))
	extendTip()
	extendTip()

	// Preprocess.
	err := svr.preProcessAccounts(testCtx(t))
	require.NoError(t, err)

	// Check the preprocess found out the outpoints.
	checkUtxos()

	// Add new utxos.
	extendAndConnect(createUtxo(13, 2, 0), createUtxo(14, 3, 1))
	checkUtxos()

	// Spend a couple of the utxos.
	extendAndConnect(spendUtxo(0), spendUtxo(0), spendUtxo(1))
	checkUtxos()

	// Spend and create a different one in the same block.
	extendAndConnect(spendUtxo(0), createUtxo(15, 1, 1))
	checkUtxos()

	// Delete all utxos.
	var manglers []blockMangler
	for len(wantUtxos) > 0 {
		manglers = append(manglers, spendUtxo(0))
	}
	extendAndConnect(manglers...)
	checkUtxos()

	// Add and spend an utxo in the very same block.
	extendAndConnect(createUtxo(16, 1, 0), spendUtxo(0))
	checkUtxos()

	// Add an utxo in a block then disapprove this block, which should
	// remove the utxo from the server.
	oldUtxos := copyWantUtxos()
	extendAndConnect(createUtxo(16, 1, 0))
	checkUtxos()
	extendAndConnect(disapprove)
	wantUtxos = oldUtxos
	checkUtxos()

	// Spend a utxo in a block that is then disapproved. This should return
	// the utxo.
	extendAndConnect(createUtxo(17, 1, 0))
	oldUtxos = copyWantUtxos()
	extendAndConnect(spendUtxo(0))
	checkUtxos()
	extendAndConnect(disapprove)
	wantUtxos = oldUtxos
	checkUtxos()

	// Create and spend an utxo in the stake tree, which isn't reversed
	// even when the next block disapproves its parent.
	extendAndConnect(spendUtxo(1), createUtxo(18, 1, 1))
	checkUtxos()
	extendAndConnect(disapprove)
	checkUtxos()

	// Create and spend an utxo then reorg to a chain that does not contain
	// those changes. This should undo those changes.
	extendAndConnect(createUtxo(19, 1, 0))
	oldUtxos = copyWantUtxos()
	extendAndConnect(spendUtxo(0), createUtxo(20, 1, 1))
	checkUtxos()
	rewindTip()
	wantUtxos = oldUtxos
	extendAndConnect(createUtxo(21, 1, 0))
	checkUtxos()

	// Create and spend an utxo, then disapprove these changes on the next
	// block and finally reorg to a different chain that rolls back the
	// disapproval.
	utxosBlockDisapproved := copyWantUtxos()
	extendAndConnect(spendUtxo(0), createUtxo(23, 2, 0))
	checkUtxos()
	utxosBlockApproved := copyWantUtxos()
	extendAndConnect(disapprove)
	wantUtxos = utxosBlockDisapproved
	checkUtxos()
	rewindTip()
	extendAndConnect()
	wantUtxos = utxosBlockApproved
	checkUtxos()
}

// TestTracksAccountUtxos ensures that chain processing tracks the unspent
// utxos for accounts under various scenarios.
func TestTracksAccountUtxos(t *testing.T) {
	testDbInstances(t, true, testTracksAccountUtxos)
}
