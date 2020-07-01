package badgerdb

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"decred.org/dcrros/backend/backenddb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
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

func (db *BadgerDB) RollbackTip(wtx backenddb.WriteTx, height int64, blockHash chainhash.Hash) error {
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
	return db.db.Close()
}
