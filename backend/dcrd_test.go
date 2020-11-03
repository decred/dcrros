// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrros/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/stretchr/testify/require"
)

// TestCheckDcrd ensures the checkDcrd function fails on the correct cases when
// the underlying dcrd instance should be rejected.
func TestCheckDcrd(t *testing.T) {
	simnet := chaincfg.SimNetParams()

	type testCase struct {
		name      string
		bcinfo    *chainjson.GetBlockChainInfoResult
		info      *chainjson.InfoChainResult
		version   map[string]chainjson.VersionResult
		wantValid bool
	}

	testCases := []testCase{{
		name: "incorrect network",
		bcinfo: &chainjson.GetBlockChainInfoResult{
			Chain: "none",
		},
		info: &chainjson.InfoChainResult{
			TxIndex: true,
		},
		version: map[string]chainjson.VersionResult{
			"dcrdjsonrpcapi": {Major: wantJsonRpcMajor, Minor: wantJsonRpcMinor},
			"dcrd":           {},
		},
		wantValid: false,
	}, {
		name: "dcrd without txindex",
		bcinfo: &chainjson.GetBlockChainInfoResult{
			Chain: "simnet",
		},
		info: &chainjson.InfoChainResult{
			TxIndex: false,
		},
		version: map[string]chainjson.VersionResult{
			"dcrdjsonrpcapi": {Major: wantJsonRpcMajor, Minor: wantJsonRpcMinor},
			"dcrd":           {},
		},
		wantValid: false,
	}, {
		name: "version reply without 'dcrdjsonrpcapi' entry",
		bcinfo: &chainjson.GetBlockChainInfoResult{
			Chain: "simnet",
		},
		info: &chainjson.InfoChainResult{
			TxIndex: true,
		},
		version: map[string]chainjson.VersionResult{
			"dcrd": {},
		},
		wantValid: false,
	}, {
		name: "too low minor version",
		bcinfo: &chainjson.GetBlockChainInfoResult{
			Chain: "simnet",
		},
		info: &chainjson.InfoChainResult{
			TxIndex: true,
		},
		version: map[string]chainjson.VersionResult{
			"dcrdjsonrpcapi": {Major: wantJsonRpcMajor, Minor: wantJsonRpcMinor - 1},
			"dcrd":           {},
		},
		wantValid: false,
	}, {
		name: "too low major version",
		bcinfo: &chainjson.GetBlockChainInfoResult{
			Chain: "simnet",
		},
		info: &chainjson.InfoChainResult{
			TxIndex: true,
		},
		version: map[string]chainjson.VersionResult{
			"dcrdjsonrpcapi": {Major: wantJsonRpcMajor - 1, Minor: wantJsonRpcMinor},
			"dcrd":           {},
		},
		wantValid: false,
	}, {
		name: "too high major version",
		bcinfo: &chainjson.GetBlockChainInfoResult{
			Chain: "simnet",
		},
		info: &chainjson.InfoChainResult{
			TxIndex: true,
		},
		version: map[string]chainjson.VersionResult{
			"dcrdjsonrpcapi": {Major: wantJsonRpcMajor + 1, Minor: wantJsonRpcMinor},
			"dcrd":           {},
		},
		wantValid: false,
	}, {
		name: "version reply without 'dcrd' version",
		bcinfo: &chainjson.GetBlockChainInfoResult{
			Chain: "simnet",
		},
		info: &chainjson.InfoChainResult{
			TxIndex: true,
		},
		version: map[string]chainjson.VersionResult{
			"dcrdjsonrpcapi": {Major: wantJsonRpcMajor, Minor: wantJsonRpcMinor},
		},
		wantValid: false,
	}, {
		name: "all checks pass",
		bcinfo: &chainjson.GetBlockChainInfoResult{
			Chain: "simnet",
		},
		info: &chainjson.InfoChainResult{
			TxIndex: true,
		},
		version: map[string]chainjson.VersionResult{
			"dcrdjsonrpcapi": {Major: wantJsonRpcMajor, Minor: wantJsonRpcMinor},
			"dcrd":           {},
		},
		wantValid: true,
	}, {
		name: "minor version increased",
		bcinfo: &chainjson.GetBlockChainInfoResult{
			Chain: "simnet",
		},
		info: &chainjson.InfoChainResult{
			TxIndex: true,
		},
		version: map[string]chainjson.VersionResult{
			"dcrdjsonrpcapi": {Major: wantJsonRpcMajor, Minor: wantJsonRpcMinor + 1},
			"dcrd":           {},
		},
		wantValid: true,
	}}

	// test executes a specific test case.
	test := func(t *testing.T, tc *testCase) {
		t.Parallel()

		// Setup the mock chain.
		c := newMockChain(t, simnet)
		c.getBlockChainInfoHook = func(ctx context.Context) (*chainjson.GetBlockChainInfoResult, error) {
			return tc.bcinfo, nil
		}
		c.getInfoHook = func(ctx context.Context) (*chainjson.InfoChainResult, error) {
			return tc.info, nil
		}
		c.versionHook = func(ctx context.Context) (map[string]chainjson.VersionResult, error) {
			return tc.version, nil
		}

		_, err := checkDcrd(context.Background(), c, simnet)
		gotValid := err == nil
		if tc.wantValid != gotValid {
			t.Fatalf("unexpected node validity. want=%v got=%v",
				tc.wantValid, gotValid)
		}

	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}

// TestWaitsForBlockchainSync asserts that the function that waits for
// blockchain sync behaves correctly.
func TestWaitsForBlockchainSync(t *testing.T) {
	mainnet := chaincfg.MainNetParams()
	simnet := chaincfg.SimNetParams()

	type testCase struct {
		name       string
		net        *chaincfg.Params
		resp       *chainjson.GetBlockChainInfoResult
		wantSynced bool
	}

	testCases := []testCase{{
		name: "empty mainnet node",
		net:  mainnet,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           0,
			InitialBlockDownload: true,
			Blocks:               0,
		},
		wantSynced: false,
	}, {
		name: "empty simnet node returns anyway",
		net:  simnet,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           0,
			InitialBlockDownload: true,
			Blocks:               0,
		},
		wantSynced: true,
	}, {
		name: "mainnet node during IBD",
		net:  mainnet,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           1000,
			InitialBlockDownload: true,
			Blocks:               250,
		},
		wantSynced: false,
	}, {
		name: "mainnet node syncing, not IBD flagged",
		net:  mainnet,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           1000,
			InitialBlockDownload: false,
			Blocks:               250,
		},
		wantSynced: false,
	}, {
		name: "mainnet node synced, IBD flagged",
		net:  mainnet,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           1000,
			InitialBlockDownload: true,
			Blocks:               1000,
		},
		wantSynced: false,
	}, {
		name: "mainnet node synced",
		net:  mainnet,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           1000,
			InitialBlockDownload: false,
			Blocks:               1000,
		},
		wantSynced: true,
	}, {
		name: "mainnet node past sync height",
		net:  mainnet,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           1000,
			InitialBlockDownload: false,
			Blocks:               1001,
		},
		wantSynced: true,
	}}

	// test executes a specific test case.
	test := func(t *testing.T, tc *testCase) {
		t.Parallel()

		// Initialize server.
		c := newMockChain(t, tc.net)
		cfg := &ServerConfig{
			ChainParams: tc.net,
			DBType:      dbTypePreconfigured,
			c:           c,
		}

		// we se a context that times out after 3 seconds. if the
		// timeout for the context is triggered, this means
		// waitforblockchainsync did not return due to the
		// getblockchaininfo call.
		ctxt, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		t.Cleanup(cancel)

		svr, err := NewServer(ctxt, cfg)
		require.NoError(t, err)

		// Keep track of how many times the hook was called to assert
		// it actually was called. Needs to be atomic due to being
		// accessed from a different goroutine.
		var gbciHookCalled int32
		c.getBlockChainInfoHook = func(ctx context.Context) (*chainjson.GetBlockChainInfoResult, error) {
			atomic.AddInt32(&gbciHookCalled, 1)
			return tc.resp, nil
		}

		// Run waitForBlockchainSync in a different goroutine and wait
		// for it to return.
		waitSyncResult := make(chan error)
		go func() {
			waitSyncResult <- svr.waitForBlockchainSync(ctxt)
		}()

		// syncResult will either be nil (if waitForBlockchainSync
		// returned directly) or context.DeadlineExceeded if the main
		// timeout was triggered.
		syncResult := <-waitSyncResult

		// Ensure GetBlockChainInfo was called at least once.
		if atomic.LoadInt32(&gbciHookCalled) == 0 {
			t.Fatalf("test premise unfulfilled: GetBlockChainInfo() " +
				"not called")
		}

		// Verify result.
		gotSynced := syncResult == nil
		if tc.wantSynced != gotSynced {
			t.Fatalf("unexpected sync result. want=%v got=%v",
				tc.wantSynced, gotSynced)
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}

// TestGetBlockCache tests the behavior of the getBlock function with the
// presence/absence of cache.
func TestGetBlockCache(t *testing.T) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	nbBlocks := uint(10)
	blocks := make([]chainhash.Hash, nbBlocks)
	for i := uint(0); i < nbBlocks; i++ {
		c.extendTip()
		blocks[i] = c.tipHash
	}

	// test is the actual test function.
	test := func(t *testing.T, testSize uint) {
		// Initialize server.
		cfg := &ServerConfig{
			ChainParams: params,
			DBType:      dbTypePreconfigured,
			c:           c,

			CacheSizeBlocks: testSize,
		}
		ctxb := context.Background()
		ctxt, cancel := context.WithCancel(ctxb)
		t.Cleanup(cancel)
		svr, err := NewServer(ctxt, cfg)
		require.NoError(t, err)

		// Request every block twice.
		for i := 0; i < 2; i++ {
			for _, bh := range blocks {
				bl, err := svr.getBlock(ctxb, &bh)
				require.NoError(t, err)
				if bl.BlockHash() != bh {
					t.Fatalf("unexpected block returned. want=%s got=%s",
						bh, bl.BlockHash())
				}
			}
		}

		// Request a block that does not exist.
		_, err = svr.getBlock(ctxb, &chainhash.Hash{})
		wantErr := types.ErrBlockNotFound
		if !errors.Is(err, wantErr) {
			t.Fatalf("unexpected error. want=%v got=%v", wantErr,
				err)
		}
	}

	// Test when the cache is zero, 1, half the maximum number of blocks,
	// exactly the number of blocks and larger than the number of blocks.
	testCacheSizes := []uint{0, 1, nbBlocks / 2, nbBlocks, nbBlocks * 2}
	for _, testSize := range testCacheSizes {
		testSize := testSize
		name := fmt.Sprintf("cache size %d", testSize)
		t.Run(name, func(t *testing.T) { test(t, testSize) })
	}
}
