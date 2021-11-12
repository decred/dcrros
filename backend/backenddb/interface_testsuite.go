// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backenddb

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
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
		{Name: "account utxos", Run: testAccountUtxos},
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

	err = db.Update(testCtx(t), func(wtx WriteTx) error {
		// Attempting to extend at the start of a tx without extending
		// the tip should fail.
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

// testAccountUtxos ensures the Add/Del/List Utxos functions work as expected.
func testAccountUtxos(t *testing.T, db DB) {

	// Helper functions.
	outpoint := func(i byte) *wire.OutPoint {
		return &wire.OutPoint{Hash: chainhash.Hash{0: i}}
	}
	account := func(i int) string {
		return fmt.Sprintf("utxoTestAcct%d", i)
	}

	type utxo struct {
		outp wire.OutPoint
		amt  dcrutil.Amount
	}
	wantUtxo := func(outpIdx byte, amt dcrutil.Amount) utxo {
		outp := outpoint(outpIdx)
		return utxo{outp: *outp, amt: amt}
	}
	checkAccountUtxos := func(dbtx ReadTx, account string, utxos ...utxo) {
		t.Helper()
		gotUtxos, err := db.ListUtxos(dbtx, account)
		require.NoError(t, err)
		if len(utxos) != len(gotUtxos) {
			t.Fatalf("unexpected nb of utxos. want=%d got=%d",
				len(utxos), len(gotUtxos))
		}
		for _, wantUtxo := range utxos {
			gotAmt, ok := gotUtxos[wantUtxo.outp]
			if !ok {
				t.Fatalf("outpoint not found in list: %s", wantUtxo.outp)
			}
			if gotAmt != wantUtxo.amt {
				t.Fatalf("unexpected amount in utxo %s. want=%d "+
					"got=%d", wantUtxo.outp, wantUtxo.amt,
					gotAmt)
			}
		}
	}

	// Commit some test utxos.
	err := db.Update(testCtx(t), func(wtx WriteTx) error {
		// Add 3 utxos for the same account.
		err := db.AddUtxo(wtx, account(1), outpoint(1), 10)
		require.NoError(t, err)
		err = db.AddUtxo(wtx, account(1), outpoint(2), 20)
		require.NoError(t, err)
		err = db.AddUtxo(wtx, account(1), outpoint(3), 30)
		require.NoError(t, err)

		// Add a single utxo for an account.
		err = db.AddUtxo(wtx, account(2), outpoint(3), 30)
		require.NoError(t, err)

		// Add and delete the utxo in the same dbtx.
		err = db.AddUtxo(wtx, account(3), outpoint(4), 40)
		require.NoError(t, err)
		err = db.DelUtxo(wtx, account(3), outpoint(4))
		require.NoError(t, err)

		// Add an utxo that will be deleted in a later tx.
		err = db.AddUtxo(wtx, account(4), outpoint(5), 50)
		require.NoError(t, err)

		// Add a large number of utxos to a single account.
		for i := 0; i < 100; i++ {
			err = db.AddUtxo(wtx, account(5), outpoint(byte(i)), 50)
			require.NoError(t, err)
		}

		// Attempt to delete utxos that haven't been added.
		err = db.DelUtxo(wtx, account(1), outpoint(255))
		require.NoError(t, err)
		err = db.DelUtxo(wtx, account(2), outpoint(255))
		require.NoError(t, err)
		err = db.DelUtxo(wtx, account(5), outpoint(255))
		require.NoError(t, err)

		// Add and delete, then add again the utxo in the same dbtx.
		err = db.AddUtxo(wtx, account(6), outpoint(6), 60)
		require.NoError(t, err)
		err = db.DelUtxo(wtx, account(6), outpoint(6))
		require.NoError(t, err)
		err = db.AddUtxo(wtx, account(6), outpoint(6), 66)
		require.NoError(t, err)

		// Attempt to delete an utxo that doesn't yet exist, then add
		// it.
		err = db.DelUtxo(wtx, account(7), outpoint(7))
		require.NoError(t, err)
		err = db.AddUtxo(wtx, account(7), outpoint(7), 70)
		require.NoError(t, err)

		// Attempt to add an utxo twice. The second one should be the
		// one that gets saved.
		err = db.AddUtxo(wtx, account(8), outpoint(8), 80)
		require.NoError(t, err)
		err = db.AddUtxo(wtx, account(8), outpoint(8), 88)
		require.NoError(t, err)

		// Add an account with a single utxo that will have additional
		// ones added later.
		err = db.AddUtxo(wtx, account(9), outpoint(9), 90)
		require.NoError(t, err)

		// Add an account with a few utxos that will have additional
		// ones added later.
		err = db.AddUtxo(wtx, account(10), outpoint(10), 100)
		require.NoError(t, err)
		err = db.AddUtxo(wtx, account(10), outpoint(11), 110)
		require.NoError(t, err)

		// Check the utxos while in the dbtx.
		checkAccountUtxos(wtx, account(1), wantUtxo(1, 10),
			wantUtxo(2, 20), wantUtxo(3, 30))
		checkAccountUtxos(wtx, account(2), wantUtxo(3, 30))
		checkAccountUtxos(wtx, account(3))
		checkAccountUtxos(wtx, account(4), wantUtxo(5, 50))
		acct5utxos := make([]utxo, 100)
		for i := 0; i < 100; i++ {
			acct5utxos[i] = wantUtxo(byte(i), 50)
		}
		checkAccountUtxos(wtx, account(5), acct5utxos...)
		checkAccountUtxos(wtx, account(6), wantUtxo(6, 66))
		checkAccountUtxos(wtx, account(7), wantUtxo(7, 70))
		checkAccountUtxos(wtx, account(8), wantUtxo(8, 88))
		checkAccountUtxos(wtx, account(9), wantUtxo(9, 90))
		checkAccountUtxos(wtx, account(10), wantUtxo(10, 100),
			wantUtxo(11, 110))

		checkAccountUtxos(wtx, account(999))

		return nil
	})
	require.NoError(t, err)

	// Perform some tests on committed values.
	err = db.Update(testCtx(t), func(wtx WriteTx) error {
		// Delete a previously added utxo and add a new one.
		err := db.DelUtxo(wtx, account(4), outpoint(5))
		require.NoError(t, err)
		checkAccountUtxos(wtx, account(4))

		// Attempt to delete an existing utxo, then re-add it.
		err = db.DelUtxo(wtx, account(7), outpoint(7))
		require.NoError(t, err)
		err = db.AddUtxo(wtx, account(7), outpoint(7), 77)
		require.NoError(t, err)

		// Attempt to re-add an utxo.
		err = db.AddUtxo(wtx, account(8), outpoint(8), 89)
		require.NoError(t, err)

		// Delete one outpoint from an account that had a few.
		err = db.DelUtxo(wtx, account(1), outpoint(2))
		require.NoError(t, err)

		// Add a bunch of new utxos to existing accounts.
		for i := 0; i < 20; i++ {
			err = db.AddUtxo(wtx, account(9), outpoint(byte(100+i)), 90)
			require.NoError(t, err)
			err = db.AddUtxo(wtx, account(10), outpoint(byte(100+i)), 100)
			require.NoError(t, err)
		}

		// Add a new utxo.
		err = db.AddUtxo(wtx, account(100), outpoint(100), 100)
		require.NoError(t, err)
		return nil
	})
	require.NoError(t, err)

	// Verify all utxos again.
	err = db.View(testCtx(t), func(rtx ReadTx) error {
		checkAccountUtxos(rtx, account(1), wantUtxo(1, 10), wantUtxo(3, 30))
		checkAccountUtxos(rtx, account(2), wantUtxo(3, 30))
		checkAccountUtxos(rtx, account(3))
		checkAccountUtxos(rtx, account(4))
		acct5utxos := make([]utxo, 100)
		for i := 0; i < 100; i++ {
			acct5utxos[i] = wantUtxo(byte(i), 50)
		}
		checkAccountUtxos(rtx, account(5), acct5utxos...)
		checkAccountUtxos(rtx, account(6), wantUtxo(6, 66))
		checkAccountUtxos(rtx, account(7), wantUtxo(7, 77))
		checkAccountUtxos(rtx, account(8), wantUtxo(8, 89))

		acct9utxos := []utxo{wantUtxo(9, 90)}
		acct10utxos := []utxo{wantUtxo(10, 100), wantUtxo(11, 110)}
		for i := 0; i < 20; i++ {
			acct9utxos = append(acct9utxos, wantUtxo(byte(100+i), 90))
			acct10utxos = append(acct10utxos, wantUtxo(byte(100+i), 100))
		}
		checkAccountUtxos(rtx, account(9), acct9utxos...)
		checkAccountUtxos(rtx, account(10), acct10utxos...)

		checkAccountUtxos(rtx, account(100), wantUtxo(100, 100))
		checkAccountUtxos(rtx, account(999))
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
