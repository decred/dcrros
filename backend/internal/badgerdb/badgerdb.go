// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package badgerdb

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"decred.org/dcrros/backend/backenddb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/wire"
	"github.com/dgraph-io/badger/v2"
)

type transaction struct {
	ctx      context.Context
	writable bool
	tx       *badger.Txn
}

func (tx *transaction) Context() context.Context {
	return tx.ctx
}

func (tx *transaction) Writable() bool {
	return tx.writable
}

type BadgerDB struct {
	mtx sync.Mutex
	db  *badger.DB
}

// NewBadgerDB creates a new instance of a backenddb.DB implementation backed
// by a Badger database.
//
// If filepath is empty, a memory-only DB is created.
func NewBadgerDB(filepath string) (*BadgerDB, error) {
	var db *badger.DB
	var err error
	var opts badger.Options

	if filepath == "" {
		opts = badger.DefaultOptions("").WithInMemory(true)
	} else {
		opts = badger.DefaultOptions(filepath)
	}

	opts = opts.WithLogger(badgerSlogAdapter{})
	db, err = badger.Open(opts)
	if err != nil {
		return nil, err
	}
	return &BadgerDB{
		db: db,
	}, nil
}

func (db *BadgerDB) Balance(rtx backenddb.ReadTx, accountAddr string, height int64) (dcrutil.Amount, error) {
	tx := rtx.(*transaction)
	balance, _, err := fetchAccountBalanceAt(tx.tx, accountAddr, height)
	return balance, err
}

func (db *BadgerDB) LastProcessedBlock(rtx backenddb.ReadTx) (chainhash.Hash, int64, error) {
	tx := rtx.(*transaction)
	return fetchLastProcessedAccountBlock(tx.tx)
}

func (db *BadgerDB) ProcessedBlockHash(rtx backenddb.ReadTx, height int64) (chainhash.Hash, error) {
	tx := rtx.(*transaction)
	hash, err := fetchProcessedBlockHash(tx.tx, height)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return hash, backenddb.ErrBlockHeightNotFound
	} else if err != nil {
		return hash, err
	}
	return hash, nil
}

func (db *BadgerDB) RollbackTip(wtx backenddb.WriteTx, blockHash chainhash.Hash, height int64) error {
	tx := wtx.(*transaction)
	if !wtx.Writable() {
		return fmt.Errorf("unwritable tx")
	}

	tipHash, tipHeight, err := fetchLastProcessedAccountBlock(tx.tx)
	if err != nil {
		return err
	}

	if tipHeight != height || tipHash != blockHash {
		return backenddb.ErrNotTip
	}

	// Figure out all accounts modified at this height.
	accounts, err := fetchBlockAccounts(tx.tx, height)
	if err != nil {
		return err
	}

	// Go over each one and remove the balance at that height.
	for _, acct := range accounts {
		if err := delAccountBalanceAt(tx.tx, acct, height); err != nil {
			return err
		}
	}

	// Find out the block hash at the previous height. This assumes we
	// won't reorg past genesis.
	prevBh, err := fetchProcessedBlockHash(tx.tx, height-1)
	if err != nil {
		return err
	}

	// Remove the rolled back block from the store of processed blocks.
	if err := delProcessedBlock(tx.tx, height); err != nil {
		return err
	}

	// Finally roll back the processed tip to the previous block.
	return putLastProcessedAccountBlock(tx.tx, &prevBh, height-1)
}

func (db *BadgerDB) StoreBalances(wtx backenddb.WriteTx, blockHash chainhash.Hash, height int64, balances map[string]dcrutil.Amount) error {
	if !wtx.Writable() {
		return fmt.Errorf("unwritable tx")
	}

	tx := wtx.(*transaction)
	// We shouldn't have processed this height yet.
	_, err := db.ProcessedBlockHash(wtx, height)
	switch {
	case errors.Is(err, backenddb.ErrBlockHeightNotFound):
		// Correct behavior (block not processed yet).
	case err != nil:
		// Other errors
		return err
	default:
		// Wrong behavior (called StoreBalances having already
		// processed this block).
		return backenddb.ErrBlockAlreadyProcessed
	}

	// We should be extending the tip.
	_, currentHeight, err := db.LastProcessedBlock(tx)
	switch {
	case err != nil:
		return err
	case height != currentHeight+1:
		return backenddb.ErrNotExtendingTip
	}

	for account, balance := range balances {
		if err := putAccountBalanceAt(tx.tx, account, height, balance); err != nil {
			return err
		}

		if err := putBlockAccount(tx.tx, height, account); err != nil {
			return err
		}
	}

	if err := putProcessedBlock(tx.tx, &blockHash, height); err != nil {
		return err
	}

	return putLastProcessedAccountBlock(tx.tx, &blockHash, height)
}

func (db *BadgerDB) AddUtxo(wtx backenddb.WriteTx, account string, outpoint *wire.OutPoint, amt dcrutil.Amount) error {
	if !wtx.Writable() {
		return fmt.Errorf("unwritable tx")
	}

	tx := wtx.(*transaction)
	return putAccountUtxo(tx.tx, account, outpoint, amt)
}

func (db *BadgerDB) DelUtxo(wtx backenddb.WriteTx, account string, outpoint *wire.OutPoint) error {
	if !wtx.Writable() {
		return fmt.Errorf("unwritable tx")
	}

	tx := wtx.(*transaction)
	return delAccountUtxo(tx.tx, account, outpoint)
}

func (db *BadgerDB) ListUtxos(rtx backenddb.ReadTx, account string) (map[wire.OutPoint]dcrutil.Amount, error) {
	tx := rtx.(*transaction)
	return fetchAccountUtxos(tx.tx, account)
}

func (db *BadgerDB) View(ctx context.Context, f func(tx backenddb.ReadTx) error) error {
	return db.db.View(func(dbtx *badger.Txn) error {
		tx := &transaction{ctx: ctx, tx: dbtx}
		return f(tx)
	})
}

func (db *BadgerDB) Update(ctx context.Context, f func(tx backenddb.WriteTx) error) error {
	return db.db.Update(func(dbtx *badger.Txn) error {
		tx := &transaction{ctx: ctx, tx: dbtx, writable: true}
		return f(tx)
	})
}

func (db *BadgerDB) Close() error {
	if db.db.IsClosed() {
		return backenddb.ErrAlreadyClosed
	}
	return db.db.Close()
}
