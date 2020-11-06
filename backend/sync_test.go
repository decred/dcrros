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
	t.Parallel()

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
	testDbInstances(t, false, testHandlesBlockNotifications)
}
