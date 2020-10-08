// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backenddb

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/stretchr/testify/require"
)

// defaultTimeout is the default timeout for test contexts.
const defaultTimeout = time.Second * 30

// TestCase is an individual test case for implementation testing.
type TestCase struct {
	// Name of the test case.
	Name string

	// Run executes the test case.
	Run func(t *testing.T, db DB)
}

// TestCases returns the standard test cases used to assert a given DB
// implementation behaves consistently as expected by the dcrros server.
func TestCases() []TestCase {
	return []TestCase{
		{Name: "propagates context", Run: testPropagatesContext},
		{Name: "update wtx writeable", Run: testUpdateWriteTx},
		{Name: "error rollsback update", Run: testErrorRollsbackUpdate},
		{Name: "stores balances", Run: testStoresBalances},
		{Name: "processed block hash", Run: testProcessedBlockHash},
		{Name: "rollback tip", Run: testRollbackTip},
		{Name: "multiple rollback", Run: testMultipleRollback},
	}
}

// RunTests runs the specified tests against a DB instance. Note that the db is
// closed at the end of the tests (independtly of whether they succeeded or
// not) so the DB instance is not usable after this returns.
//
// Returns true if no tests failed.
func RunTests(t *testing.T, db DB, tests []TestCase) bool {
	for _, tc := range tests {
		tc := tc
		ok := t.Run(tc.Name, func(t *testing.T) {
			tc.Run(t, db)
		})
		if !ok {
			return false
		}
	}

	err := db.Close()
	require.NoError(t, err)

	return t.Run("closing twice errors", func(t *testing.T) {
		testCloseTwice(t, db)
	})
}

// testCtx returns a context that gets canceled after defaultTimeout or after
// the test ends.
func testCtx(t *testing.T) context.Context {
	ctxt, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	t.Cleanup(cancel)
	return ctxt
}

// randomHash returns a random chainhash.
func randomHash() chainhash.Hash {
	var h chainhash.Hash
	_, err := rand.Read(h[:])
	if err != nil {
		panic(err)
	}
	return h
}

// lastHeight returns the last height processed by this DB instance or panics.
func lastHeight(db DB) int64 {
	var height int64
	err := db.View(context.Background(), func(rtx ReadTx) error {
		var err error
		_, height, err = db.LastProcessedBlock(rtx)
		return err
	})
	if err != nil {
		panic(err)
	}
	return height
}

// testPropagatesContext ensures the context passed to View/Update is correctly
// propagated to callers.
func testPropagatesContext(t *testing.T, db DB) {
	key := "testkey"
	value := "testvalue"
	ctxb := context.Background()
	err := db.View(context.WithValue(ctxb, key, value), func(rtx ReadTx) error {
		gotValue := rtx.Context().Value(key)
		require.Equal(t, value, gotValue)
		return nil
	})
	require.NoError(t, err)

	err = db.Update(context.WithValue(ctxb, key, value), func(wtx WriteTx) error {
		gotValue := wtx.Context().Value(key)
		require.Equal(t, value, gotValue)
		return nil
	})
	require.NoError(t, err)
}

// testUpdateWriteTx ensures that a WriteTx on an Update() call is writable.
func testUpdateWriteTx(t *testing.T, db DB) {
	err := db.Update(testCtx(t), func(wtx WriteTx) error {
		require.True(t, wtx.Writable())
		return nil
	})
	require.NoError(t, err)
}

// testErrorRollsbackUpdate ensures that returning an error inside an Update()
// call causes the changes to _not_ be committed.
func testErrorRollsbackUpdate(t *testing.T, db DB) {
	wantErr := errors.New("test error")

	bh := randomHash()
	height := lastHeight(db) + 1

	err := db.Update(testCtx(t), func(wtx WriteTx) error {
		// Successfully store a dummy balance.
		err := db.StoreBalances(wtx, bh, height, nil)
		require.NoError(t, err)
		return nil
	})
	require.NoError(t, err)

	err = db.Update(testCtx(t), func(wtx WriteTx) error {
		// Attempt to store a dummy balance.
		err := db.StoreBalances(wtx, randomHash(), height+1, nil)
		require.NoError(t, err)

		// Return an error, forcing a roll back.
		return wantErr
	})
	require.ErrorIs(t, err, wantErr)

	err = db.View(testCtx(t), func(rtx ReadTx) error {
		// The last processed hash and height should be the last one we
		// successfully saved.
		gotHash, gotHeight, err := db.LastProcessedBlock(rtx)
		require.NoError(t, err)
		require.Equal(t, height, gotHeight)
		require.Equal(t, bh, gotHash)

		// ProcessedBlockHash should return an error for the height we
		// attempted to save.
		_, err = db.ProcessedBlockHash(rtx, height+1)
		require.ErrorIs(t, err, ErrBlockHeightNotFound)

		return nil
	})
	require.NoError(t, err)
}

// testStoresBalances asserts that balances can be stored and retrieved.
func testStoresBalances(t *testing.T, db DB) {
	bh := randomHash()
	height := lastHeight(db) + 1

	balances := map[string]dcrutil.Amount{
		"first":  10,
		"second": 20,
		"third":  30,
	}

	err := db.Update(testCtx(t), func(wtx WriteTx) error {
		// Store the sample balances.
		err := db.StoreBalances(wtx, bh, height, balances)
		require.NoError(t, err)

		// We should be able to read the correct balances before
		// committing.
		bal, err := db.Balance(wtx, "second", height)
		require.NoError(t, err)
		require.Equal(t, balances["second"], bal)

		// Trying to update a value before committing should fail.
		err = db.StoreBalances(wtx, bh, height, balances)
		require.ErrorIs(t, err, ErrBlockAlreadyProcessed)

		return nil
	})
	require.NoError(t, err)

	err = db.View(testCtx(t), func(rtx ReadTx) error {
		var zero dcrutil.Amount

		// We should be able to read the correct balances after
		// committing.
		bal, err := db.Balance(rtx, "third", height)
		require.NoError(t, err)
		require.Equal(t, balances["third"], bal)

		// Reading from a block height less than the stored one should
		// return zero.
		bal, err = db.Balance(rtx, "third", height-1)
		require.NoError(t, err)
		require.Equal(t, zero, bal)

		// Reading from a block height greater than the stored one
		// should return the most recent balance.
		bal, err = db.Balance(rtx, "third", height+1)
		require.NoError(t, err)
		require.Equal(t, balances["third"], bal)

		// Reading a blank account should return 0.
		bal, err = db.Balance(rtx, "unknown", height)
		require.NoError(t, err)
		require.Equal(t, zero, bal)

		return nil
	})
	require.NoError(t, err)

	err = db.Update(testCtx(t), func(wtx WriteTx) error {
		// Trying to store the same block twice should fail
		// (StoreBalance can only be called once for a given height).
		err = db.StoreBalances(wtx, bh, height, balances)
		require.ErrorIs(t, err, ErrBlockAlreadyProcessed)
		return nil
	})
	require.NoError(t, err)
}

// testProcessedBlockHash asserts the behavior of the DB regarding storing the
// processed tip hash.
func testProcessedBlockHash(t *testing.T, db DB) {
	bh1 := randomHash()
	height := lastHeight(db) + 1

	err := db.Update(testCtx(t), func(wtx WriteTx) error {
		// Store a test height.
		err := db.StoreBalances(wtx, bh1, height, nil)
		require.NoError(t, err)

		// Block hash and height should match before committing.
		bh, h, err := db.LastProcessedBlock(wtx)
		require.NoError(t, err)
		require.Equal(t, bh1, bh)
		require.Equal(t, height, h)

		// Checking if this block height was processed should succeed
		// before committing.
		bh, err = db.ProcessedBlockHash(wtx, height)
		require.NoError(t, err)
		require.Equal(t, bh1, bh)

		// Attempting to extend without being a child of current tip
		// should fail.
		err = db.StoreBalances(wtx, bh1, height+2, nil)
		require.ErrorIs(t, err, ErrNotExtendingTip)

		return nil
	})
	require.NoError(t, err)

	err = db.View(testCtx(t), func(rtx ReadTx) error {
		// Block hash and height should match after committing.
		bh, h, err := db.LastProcessedBlock(rtx)
		require.NoError(t, err)
		require.NoError(t, err)
		require.Equal(t, bh1, bh)
		require.Equal(t, height, h)

		// Checking if this block height was processed should succeed.
		bh, err = db.ProcessedBlockHash(rtx, height)
		require.NoError(t, err)
		require.Equal(t, bh1, bh)

		// Checking if the next height was processed should fail.
		_, err = db.ProcessedBlockHash(rtx, height+1)
		require.ErrorIs(t, err, ErrBlockHeightNotFound)

		// Checking if a negative height was processed should fail.
		_, err = db.ProcessedBlockHash(rtx, -1)
		require.ErrorIs(t, err, ErrBlockHeightNotFound)

		return nil
	})
	require.NoError(t, err)
}

// testRollbackTip verifies RollbackTip() works as expected.
func testRollbackTip(t *testing.T, db DB) {
	account := randomHash().String()
	prevHash := randomHash()
	prevHeight := lastHeight(db) + 1
	prevBalances := map[string]dcrutil.Amount{
		account: 10,
	}
	testHash := randomHash()
	testHeight := prevHeight + 1
	testBalances := map[string]dcrutil.Amount{
		account: 20,
	}

	// Helper to assert the current last processed block is
	// prevHash/prevHeight.
	assertCorrectLastBlock := func(rtx ReadTx) {
		// The last processed hash and height should be the correct one
		// again.
		bh, h, err := db.LastProcessedBlock(rtx)
		require.NoError(t, err)
		require.Equal(t, prevHeight, h)
		require.Equal(t, prevHash, bh)

		// The balance at testHeight should still be the previous
		// balance.
		bal, err := db.Balance(rtx, account, testHeight)
		require.NoError(t, err)
		require.Equal(t, prevBalances[account], bal)
	}

	err := db.Update(testCtx(t), func(wtx WriteTx) error {
		// Store the initial block.
		err := db.StoreBalances(wtx, prevHash, prevHeight, prevBalances)
		require.NoError(t, err)
		return nil
	})
	require.NoError(t, err)

	// Currently the backend only does rollbacks on committed blocks so we
	// don't test rolling back an uncommitted StoreBalances() call.

	err = db.Update(testCtx(t), func(wtx WriteTx) error {
		// Store the test block that will be rolled back.
		err := db.StoreBalances(wtx, testHash, testHeight, testBalances)
		require.NoError(t, err)
		return nil
	})
	require.NoError(t, err)

	err = db.Update(testCtx(t), func(wtx WriteTx) error {
		// Rolling back should work on a committed tx.
		err = db.RollbackTip(wtx, testHash, testHeight)
		require.NoError(t, err)
		assertCorrectLastBlock(wtx)
		return nil
	})
	require.NoError(t, err)

	err = db.View(testCtx(t), func(rtx ReadTx) error {
		// Assert the correct block after committing the rollback.
		assertCorrectLastBlock(rtx)
		return nil
	})
	require.NoError(t, err)

	// Perform negative tests. Recall that the tip is now
	// prevHash/prevHeight.

	err = db.Update(testCtx(t), func(wtx WriteTx) error {
		// Trying to rollback without passing the correct height should
		// fail (recall the tip is now prevHash/prevHeight).
		err = db.RollbackTip(wtx, prevHash, testHeight)
		require.ErrorIs(t, err, ErrNotTip)

		// Trying to rollback without passing the correct hash should
		// fail.
		err = db.RollbackTip(wtx, testHash, prevHeight)
		require.ErrorIs(t, err, ErrNotTip)

		return nil
	})
	require.NoError(t, err)
}

// testMultipleRollback ensures we can rollback multiple blocks within a single
// tx.
func testMultipleRollback(t *testing.T, db DB) {
	// We'll roll back nbBlocks -1 blocks.
	nbBlocks := 10
	height := lastHeight(db)
	type block struct {
		hash   chainhash.Hash
		height int64
	}
	blocks := make([]block, nbBlocks)
	for i := 0; i < nbBlocks; i++ {
		blocks[i] = block{
			hash:   randomHash(),
			height: height + 1,
		}
		height++
	}

	// Commit all blocks.
	err := db.Update(testCtx(t), func(wtx WriteTx) error {
		for _, b := range blocks {
			err := db.StoreBalances(wtx, b.hash, b.height, nil)
			require.NoError(t, err)
		}
		return nil
	})
	require.NoError(t, err)

	// Rollback nbBlocks -1 blocks.
	err = db.Update(testCtx(t), func(wtx WriteTx) error {
		for i := nbBlocks - 1; i > 0; i-- {
			b := blocks[i]
			err := db.RollbackTip(wtx, b.hash, b.height)
			require.NoError(t, err)

			// The new tip should be the previous block.
			prev := blocks[i-1]
			bh, h, err := db.LastProcessedBlock(wtx)
			require.NoError(t, err)
			require.Equal(t, prev.height, h)
			require.Equal(t, prev.hash, bh)
		}
		return nil
	})
	require.NoError(t, err)

	// Assert the correct ending block.
	err = db.View(testCtx(t), func(rtx ReadTx) error {
		b := blocks[0]
		bh, h, err := db.LastProcessedBlock(rtx)
		require.NoError(t, err)
		require.Equal(t, b.height, h)
		require.Equal(t, b.hash, bh)
		return nil
	})
	require.NoError(t, err)

}

// testCloseTwice ensures that trying to close a closed db fails with the
// appropriate error.
//
// This must be executed after the test db has been closed.
func testCloseTwice(t *testing.T, db DB) {
	err := db.Close()
	wantErr := ErrAlreadyClosed
	if !errors.Is(err, wantErr) {
		t.Fatalf("unexpected error. want=%v got=%v", wantErr, err)
	}
}
