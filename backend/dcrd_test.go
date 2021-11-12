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

	"decred.org/dcrros/backend/internal/memdb"
	"decred.org/dcrros/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
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
	},
		// Commented out because the current minor version is 0.
		/*
			{
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
			},
		*/
		{
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

// TestWaitForPeers asserts the waitForPeers function works as intended.
func TestWaitForPeers(t *testing.T) {
	t.Parallel()
	params := chaincfg.SimNetParams()

	type testCase struct {
		name       string
		peerCount  int
		err        error
		timeout    time.Duration
		ctxTimeout time.Duration
		wantErr    error
	}

	dummyErr := errors.New("dummy")

	testCases := []testCase{{
		name:       "negative timeout, negative context",
		timeout:    -1,
		ctxTimeout: -1,
		wantErr:    errNoPeersAfterTimeout,
	}, {
		name:       "negative timeout, positive context",
		timeout:    -1,
		ctxTimeout: time.Second,
		wantErr:    errNoPeersAfterTimeout,
	}, {
		name:       "positive timeout, negative timeout",
		timeout:    time.Second,
		ctxTimeout: -1,
		wantErr:    context.DeadlineExceeded,
	}, {
		name:       "GetPeerInfo errored",
		timeout:    time.Second,
		ctxTimeout: time.Second,
		err:        dummyErr,
		wantErr:    dummyErr,
	}, {
		name:       "found peers",
		timeout:    time.Second,
		ctxTimeout: time.Second,
		peerCount:  1,
	}}

	test := func(t *testing.T, tc *testCase) {
		t.Parallel()

		// Create a mock chain and hook into GetPeerInfo.
		c := newMockChain(t, params)
		c.getPeerInfoHook = func(ctx context.Context) ([]chainjson.GetPeerInfoResult, error) {
			return make([]chainjson.GetPeerInfoResult, tc.peerCount),
				tc.err
		}

		// Initialize server.
		db, err := memdb.NewMemDB()
		require.NoError(t, err)
		cfg := &ServerConfig{
			ChainParams: params,
			DBType:      dbTypePreconfigured,
			db:          db,
			c:           c,
		}
		svr := newTestServer(t, cfg)

		ctx, cancel := context.WithTimeout(context.Background(), tc.ctxTimeout)
		defer cancel()

		gotErr := svr.waitForPeers(ctx, tc.timeout)
		if !errors.Is(gotErr, tc.wantErr) {
			t.Fatalf("unexpected error. want=%v got=%v", tc.wantErr,
				gotErr)
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
	t.Parallel()

	mainnet := chaincfg.MainNetParams()
	simnet := chaincfg.SimNetParams()

	type testCase struct {
		name             string
		net              *chaincfg.Params
		resp             *chainjson.GetBlockChainInfoResult
		wantError        error
		wantSynced       bool
		preprocessBlocks int
	}

	testCases := []testCase{{
		name: "empty mainnet node",
		net:  mainnet,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           0,
			InitialBlockDownload: true,
			Blocks:               0,
		},
		wantError:  nil,
		wantSynced: false,
	}, {
		name: "empty simnet node returns anyway",
		net:  simnet,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           0,
			InitialBlockDownload: true,
			Blocks:               0,
		},
		wantError:  nil,
		wantSynced: false,
	}, {
		name: "mainnet node during IBD",
		net:  mainnet,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           1000,
			InitialBlockDownload: true,
			Blocks:               250,
		},
		wantError:  context.DeadlineExceeded,
		wantSynced: false,
	}, {
		name: "mainnet node downloading blocks, not IBD flagged",
		net:  mainnet,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           1000,
			InitialBlockDownload: false,
			Blocks:               250,
		},
		wantError:  context.DeadlineExceeded,
		wantSynced: false,
	}, {
		name: "mainnet node all blocks, IBD still flagged",
		net:  mainnet,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           1000,
			InitialBlockDownload: true,
			Blocks:               1000,
		},
		wantError:  context.DeadlineExceeded,
		wantSynced: false,
	}, {
		name: "mainnet node fetched all blocks",
		net:  mainnet,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           1000,
			InitialBlockDownload: false,
			Blocks:               1000,
		},
		wantError:  nil,
		wantSynced: true,
	}, {
		name: "mainnet node past sync height",
		net:  mainnet,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           1000,
			InitialBlockDownload: false,
			Blocks:               1001,
		},
		wantError:  nil,
		wantSynced: true,
	}, {
		name:             "mainnet node with old db connected to unsynced node",
		net:              mainnet,
		preprocessBlocks: 3,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           2,
			InitialBlockDownload: false,
			Blocks:               2,
		},
		wantError:  context.DeadlineExceeded,
		wantSynced: false,
	}, {
		name:             "mainnet node with old db connected to synced node",
		net:              mainnet,
		preprocessBlocks: 3,
		resp: &chainjson.GetBlockChainInfoResult{
			SyncHeight:           3,
			InitialBlockDownload: false,
			Blocks:               3,
		},
		wantError:  nil,
		wantSynced: true,
	}}

	// test executes a specific test case.
	test := func(t *testing.T, tc *testCase) {
		t.Parallel()

		// Initialize server.
		c := newMockChain(t, tc.net)
		db, err := memdb.NewMemDB()
		require.NoError(t, err)
		cfg := &ServerConfig{
			ChainParams: tc.net,
			DBType:      dbTypePreconfigured,
			db:          db,
			c:           c,
		}
		svr := newTestServer(t, cfg)

		// If we're simulating an existing server restarting, generate
		// a chain and have the server preprocess it.
		for i := 0; i < tc.preprocessBlocks; i++ {
			c.extendTip()
		}
		err = svr.preProcessAccounts(testCtx(t))
		require.NoError(t, err)

		// We use a context that times out after 3 seconds. If the
		// timeout for the context is triggered, this means
		// waitForBlockchainSync did not return due to the
		// getBlockChainInfo call.
		ctxt, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		t.Cleanup(cancel)

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
		if !errors.Is(tc.wantError, syncResult) {
			t.Fatalf("unexpected sync error. want=%v got=%v",
				tc.wantError, syncResult)
		}

		// Assert the sync status is the expected one.
		svr.mtx.Lock()
		gotSS := svr.syncStatus
		svr.mtx.Unlock()
		if gotSS.Stage == nil || *gotSS.Stage != syncStatusStageBlockchainSync {
			t.Fatalf("unexpected syncStatus.Stage. want=%v got=%v",
				syncStatusStageBlockchainSync, gotSS.Stage)
		}
		if gotSS.CurrentIndex == nil || *gotSS.CurrentIndex != tc.resp.Blocks {
			t.Fatalf("unexpected syncStatus.CurrentIndex. want=%d got=%d",
				tc.resp.Blocks, gotSS.CurrentIndex)
		}
		if gotSS.TargetIndex == nil || *gotSS.TargetIndex != tc.resp.SyncHeight {
			t.Fatalf("unexpected syncStatus.TargetIndex. want=%d got=%v",
				tc.resp.SyncHeight, gotSS.TargetIndex)
		}
		if gotSS.Synced == nil || *gotSS.Synced != tc.wantSynced {
			t.Fatalf("unexpected syncStatus.Synced. want=%v got=%v",
				tc.wantSynced, *gotSS.Synced)
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}

// TestWaitsForBlockchainSyncWrongNet asserts that the function that waits for
// blockchain sync behaves correctly when the underlying dcrd instance changes
// to an unsuitable node.
func TestWaitsForBlockchainSyncWrongNet(t *testing.T) {
	t.Parallel()

	mainnet := chaincfg.MainNetParams()
	simnet := chaincfg.SimNetParams()

	// Initialize server.
	c := newMockChain(t, mainnet)
	c.extendTip()

	db, err := memdb.NewMemDB()
	require.NoError(t, err)
	cfg := &ServerConfig{
		ChainParams: mainnet,
		DBType:      dbTypePreconfigured,
		db:          db,
		c:           c,
	}
	svr := newTestServer(t, cfg)

	// Switch the chain to simnet to simulate a connection to a node in a
	// different chain.
	c.mtx.Lock()
	c.params = simnet
	c.mtx.Unlock()
	svr.onDcrdConnected()

	// Run waitForBlockchainSync in a different goroutine and wait
	// for it to return.
	ctxt, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	waitSyncResult := make(chan error)
	go func() {
		waitSyncResult <- svr.waitForBlockchainSync(ctxt)
	}()

	// Shouldn't be done with the wait yet.
	select {
	case err := <-waitSyncResult:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	// Go back to the correct network.
	c.mtx.Lock()
	c.params = mainnet
	c.mtx.Unlock()
	svr.onDcrdConnected()

	// The wait should now complete without errors.
	select {
	case err := <-waitSyncResult:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(3 * time.Second):
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
		svr := newTestServer(t, cfg)

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
		_, err := svr.getBlock(ctxb, &chainhash.Hash{})
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
