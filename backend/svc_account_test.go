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

func accountBalanceByHeight(t *testing.T, svr *Server, height int64,
	account string) int64 {
	t.Helper()

	req := &rtypes.AccountBalanceRequest{
		BlockIdentifier: &rtypes.PartialBlockIdentifier{
			Index: &height,
		},
		AccountIdentifier: &rtypes.AccountIdentifier{
			Address: account,
			Metadata: map[string]interface{}{
				"script_version": uint16(0),
			},
		},
	}
	res, rerr := svr.AccountBalance(testCtx(t), req)
	require.Nil(t, rerr)
	gotBalance, err := strconv.ParseInt(res.Balances[0].Value, 10, 64)
	require.NoError(t, err)
	return gotBalance
}

func accountBalanceByHash(t *testing.T, svr *Server, hash chainhash.Hash,
	account string) int64 {
	t.Helper()

	bh := hash.String()
	req := &rtypes.AccountBalanceRequest{
		BlockIdentifier: &rtypes.PartialBlockIdentifier{
			Hash: &bh,
		},
		AccountIdentifier: &rtypes.AccountIdentifier{
			Address: account,
			Metadata: map[string]interface{}{
				"script_version": uint16(0),
			},
		},
	}
	res, rerr := svr.AccountBalance(testCtx(t), req)
	require.Nil(t, rerr)
	gotBalance, err := strconv.ParseInt(res.Balances[0].Value, 10, 64)
	require.NoError(t, err)
	return gotBalance
}

func accountBalanceAtTip(mc *mockChain, svr *Server, account string) int64 {
	mc.t.Helper()

	req := &rtypes.AccountBalanceRequest{
		AccountIdentifier: &rtypes.AccountIdentifier{
			Address: account,
			Metadata: map[string]interface{}{
				"script_version": uint16(0),
			},
		},
	}
	res, rerr := svr.AccountBalance(testCtx(mc.t), req)
	require.Nil(mc.t, rerr)

	// Ensure the balance was returned for the current chain tip.
	if mc.tipHeight != res.BlockIdentifier.Index {
		mc.t.Fatalf("block index returned by AccountBalance() does not "+
			"match chain tip. want=%d got=%d", mc.tipHeight,
			res.BlockIdentifier.Index)
	}
	if mc.tipHash.String() != res.BlockIdentifier.Hash {
		mc.t.Fatalf("block hash returned by AccountBalance() does not " +
			"match chain tip.")
	}

	gotBalance, err := strconv.ParseInt(res.Balances[0].Value, 10, 64)
	require.NoError(mc.t, err)
	return gotBalance
}

func assertBalanceAtTip(mc *mockChain, svr *Server, account string, wantBalance int64) {
	mc.t.Helper()
	gotBalance := accountBalanceAtTip(mc, svr, account)
	if wantBalance != gotBalance {
		mc.t.Fatalf("unexpected balance. want=%d got=%d", wantBalance, gotBalance)
	}
}

func testPreprocessesAccounts(t *testing.T, db backenddb.DB) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	pks := mustHex("76a91464098ee97e822d8b98e93608c4f0cdbb559146fa88ac")
	addr := "RsHTkwUoDYKh3SJCj1FSoWbKy1P2DA6DHA5"

	// disapprove disapproves the block.
	disapprove := func(b *wire.MsgBlock) {
		b.Header.VoteBits = 0
	}

	// Shorter function names to improve readability.
	manglers := func(ms ...blockMangler) []blockMangler {
		return ms
	}
	toAddr := func(v, nb int64) blockMangler {
		return c.txToAddr(pks, v, nb)
	}
	fromAddr := func(v, nb int64) blockMangler {
		return c.txFromAddr(pks, v, nb)
	}

	testCases := []struct {
		name        string
		manglers    []blockMangler
		wantBalance int64
	}{{
		name:        "block before addr receives anything",
		manglers:    manglers(),
		wantBalance: 0,
	}, {
		name:        "single output added to address",
		manglers:    manglers(toAddr(10, 1)),
		wantBalance: 10,
	}, {
		name:        "second block without txs to addr",
		manglers:    manglers(),
		wantBalance: 10,
	}, {
		name:        "second block with single output added to address",
		manglers:    manglers(toAddr(10, 1)),
		wantBalance: 20,
	}, {
		name:        "multiple txs adding to addr",
		manglers:    manglers(toAddr(10, 1), toAddr(10, 1)),
		wantBalance: 40,
	}, {
		name:        "single tx sending multiple times to same addr",
		manglers:    manglers(toAddr(10, 3)),
		wantBalance: 70,
	}, {
		name:        "spend a sigle utxo in the block",
		manglers:    manglers(fromAddr(10, 1)),
		wantBalance: 60,
	}, {
		name:        "spend multiple utxos in the same tx",
		manglers:    manglers(fromAddr(10, 2)),
		wantBalance: 40,
	}, {
		name:        "spend multiple utxo in different txs",
		manglers:    manglers(fromAddr(10, 1), fromAddr(10, 1)),
		wantBalance: 20,
	}, {
		name:        "add and spend in the same block",
		manglers:    manglers(toAddr(20, 1), fromAddr(10, 1)),
		wantBalance: 30,
	}, {
		name:        "add to addr in a block that will be disapproved",
		manglers:    manglers(toAddr(10, 1)),
		wantBalance: 40,
	}, {
		name:        "disapprove the block that added to the addr",
		manglers:    manglers(disapprove),
		wantBalance: 30,
	}, {
		name:        "spend in a block that will be disapproved",
		manglers:    manglers(fromAddr(10, 1)),
		wantBalance: 20,
	}, {
		name:        "disapprove the block that spent from the addr",
		manglers:    manglers(disapprove),
		wantBalance: 30,
	}, {
		name:        "add and spend in a block that will be disapproved",
		manglers:    manglers(toAddr(20, 1), fromAddr(10, 1)),
		wantBalance: 40,
	}, {
		name:        "disapprove previous but also add in this block",
		manglers:    manglers(disapprove, toAddr(20, 1)),
		wantBalance: 50,
	}, {
		name:        "final block",
		manglers:    manglers(),
		wantBalance: 50,
	}}

	// Setup the chain that will be preprocessed.
	for _, tc := range testCases {
		c.extendTip(tc.manglers...)
	}

	// Create the server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
		db:          db,
	}
	svr := newTestServer(t, cfg)

	// Preprocess accounts.
	err := svr.preProcessAccounts(testCtx(t))
	require.NoError(t, err)

	// Verify the historical balance at each block.
	for i, tc := range testCases {
		height := int64(i + 1)

		// Verify the account balance by block height.
		gotBalance := accountBalanceByHeight(t, svr, height, addr)
		if gotBalance != tc.wantBalance {
			t.Fatalf("'%s': unexpected balance. want=%d got=%d",
				tc.name, tc.wantBalance, gotBalance)
		}

		// Verify the account balance by block hash.
		bh := c.blocksByHeight[height]
		gotBalance = accountBalanceByHash(t, svr, *bh, addr)
		if gotBalance != tc.wantBalance {
			t.Fatalf("'%s': unexpected balance by hash. want=%d got=%d",
				tc.name, tc.wantBalance, gotBalance)
		}
	}

	// Verify the balance at tip.
	gotBalance := accountBalanceAtTip(c, svr, addr)
	wantBalance := testCases[len(testCases)-1].wantBalance
	if gotBalance != wantBalance {
		t.Fatalf("unexpected balance at tip. want=%d got=%d",
			wantBalance, gotBalance)
	}
}

// TestPreprocessesAccounts ensures that pre-processing the blockchain yields
// correct historical account balances.
func TestPreprocessesAccounts(t *testing.T) {
	testDbInstances(t, true, testPreprocessesAccounts)
}

func testPreprocessesGenesis(t *testing.T, db backenddb.DB) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	// Initialize server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
		db:          db,
	}
	svr := newTestServer(t, cfg)

	// Preprocess.
	err := svr.preProcessAccounts(testCtx(t))
	require.NoError(t, err)
}

// TestPreprocessesGenesis tests that preprocessing the chain works when the
// underlying chain is still at the genesis block.
func TestPreprocessesGenesis(t *testing.T) {
	testDbInstances(t, true, testPreprocessesGenesis)
}

func testPreprocessesAfterRestart(t *testing.T, db backenddb.DB) {
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

	// Initialize server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
		db:          db,
	}
	svr := newTestServer(t, cfg)

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
		name         string
		chainChanges func()
		wantBalance  int64
		wantError    bool
	}{{
		name:         "no changes before restarting",
		chainChanges: func() {},
		wantBalance:  6,
	}, {
		name: "chain extended",
		chainChanges: func() {
			c.extendTip(fromAddr(3, 1))
			c.extendTip(toAddr(7, 1))
		},
		wantBalance: 10,
	}, {
		name: "chain reorged to same height",
		chainChanges: func() {
			c.rewindChain(1)
			c.extendTip(toAddr(9, 1))
		},
		wantBalance: 12,
	}, {
		name: "chain reorged to higher height",
		chainChanges: func() {
			c.rewindChain(1)
			c.extendTip(toAddr(19, 1))
			c.extendTip(fromAddr(3, 1))
		},
		wantBalance: 19,
	}, {
		name: "chain reorged to lower height",
		chainChanges: func() {
			c.rewindChain(3)
			c.extendTip(toAddr(5, 1))
		},
		wantError: true,
	}, {
		name: "reorged after having failed to process",
		chainChanges: func() {
			c.extendTip(toAddr(21, 1))
			c.extendTip(fromAddr(5, 1))
		},
		wantBalance: 27,
	}}

	for _, tc := range testCases {
		c.mtx.Lock()
		tc.chainChanges()
		c.mtx.Unlock()
		t.Logf("case '%s'", tc.name)

		// Preprocess again, simulating a restart.
		err = svr.preProcessAccounts(testCtx(t))
		if tc.wantError != (err != nil) {
			t.Fatalf("unexpected error. want=%v got=%v",
				tc.wantError, err != nil)
		}
		if err != nil {
			// Do not assert balances when preprocessing should
			// have errored.
			continue
		}

		// Account balance should be the new value.
		assertBalanceAtTip(c, svr, addr, tc.wantBalance)
	}
}

// TestPreprocessesAfterRestart tests various cases of reprocessing the chain
// after the server is restarted.
func TestPreprocessesAfterRestart(t *testing.T) {
	testDbInstances(t, false, testPreprocessesAfterRestart)
}

func testReorgDuringPreprocess(t *testing.T, db backenddb.DB) {
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

	// Initialize server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
		db:          db,
	}
	svr := newTestServer(t, cfg)

	// Override concurrency to ensure multiple blocks are requested
	// concurrently during preprocessing.
	svr.concurrency = 4

	// Generate a chain that funds an account.
	c.extendTip(toAddr(11, 1))
	c.extendTip(fromAddr(5, 1))

	// Mark the point where we'll reorg to a new chain and also create
	// enough blocks that parallel download of blocks will be saturated.
	forkPoint := c.tipHash
	c.extendTip(toAddr(7, 1))
	for i := 0; i < svr.concurrency+1; i++ {
		c.extendTip()
	}
	c.extendTip(fromAddr(3, 1))

	// Hook into GetBlockHash. Before preprocess() fetches the forkPoint
	// block, we'll cause a reorg.
	c.getBlockHashHook = func(ctx context.Context, blockHeight int64) (*chainhash.Hash, error) {
		bh, err := c.getBlockHash(ctx, blockHeight)
		if err != nil {
			return bh, err
		}
		if *bh != forkPoint {
			// Not the reorg point.
			return bh, err
		}

		// Give it enough time for the other goroutines that fetch
		// blocks to fetch the blocks that will be reorged-out.
		time.Sleep(time.Millisecond * 100)

		// Cause the reorg.
		nbBlocks := int(c.tipHeight - blockHeight)
		c.rewindChain(nbBlocks)
		c.extendTip(toAddr(17, 1))
		for i := 0; i < svr.concurrency+1; i++ {
			c.extendTip()
		}
		c.extendTip(fromAddr(5, 1))
		return c.getBlockHash(ctx, blockHeight)
	}

	// Preprocess.
	err := svr.preProcessAccounts(testCtx(t))
	require.NoError(t, err)

	// Account balance should be the reorged-in balance.
	assertBalanceAtTip(c, svr, addr, 18)
}

// TestReorgDuringPreprocess tests that a reorg that happened during the
// preprocessing of accounts is correctly handled.
func TestReorgDuringPreprocess(t *testing.T) {
	testDbInstances(t, true, testReorgDuringPreprocess)
}
