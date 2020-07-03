// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package memdb

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"decred.org/dcrros/backend/backenddb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
)

type balanceHeight struct {
	height  int64
	balance dcrutil.Amount
}

type processedBlock struct {
	hash     chainhash.Hash
	accounts []string
}

type transaction struct {
	ctx             context.Context
	writable        bool
	balances        map[string][]balanceHeight
	updatedBlock    bool
	blockHash       chainhash.Hash
	blockHeight     int64
	processedBlocks map[int64]*processedBlock
}

func (t *transaction) Context() context.Context {
	return t.ctx
}

func (t *transaction) Writable() bool {
	return t.writable
}

type MemDB struct {
	balances        map[string][]balanceHeight
	lastBlockHash   chainhash.Hash
	lastHeight      int64
	processedBlocks map[int64]*processedBlock
	mtx             sync.Mutex
}

func NewMemDB() (*MemDB, error) {
	return &MemDB{
		balances:        make(map[string][]balanceHeight),
		processedBlocks: make(map[int64]*processedBlock),
	}, nil
}

func (db *MemDB) Balance(rtx backenddb.ReadTx, accountAddr string, height int64) (dcrutil.Amount, error) {

	// If the first balance in the tx is at a greater height than the
	// target height, check for what is in the db.
	balances := rtx.(*transaction).balances[accountAddr]
	if len(balances) == 0 || balances[0].height > height {
		balances = db.balances[accountAddr]
		if len(balances) == 0 {
			// No balances for this account.
			return 0, nil
		}
	}

	cmp := func(i int) bool {
		return balances[i].height >= height
	}
	i := sort.Search(len(balances), cmp)

	var balance dcrutil.Amount
	switch {
	case i == len(balances) && len(balances) > 0:
		// Found at last height.
		balance = balances[i-1].balance
	case balances[i].height == height:
		// Found entry at height.
		balance = balances[i].balance
	case i == 0:
		// All recorded balances are at a higher height.
	default:
		// Pick up the previous balance since that's the most recent
		// one <= height.
		balance = balances[i-1].balance

	}
	return balance, nil
}

func (db *MemDB) LastProcessedBlock(rtx backenddb.ReadTx) (chainhash.Hash, int64, error) {
	tx := rtx.(*transaction)
	if tx.updatedBlock {
		return tx.blockHash, tx.blockHeight, nil
	}
	return db.lastBlockHash, db.lastHeight, nil
}

func (db *MemDB) StoreBalances(wtx backenddb.WriteTx, blockHash chainhash.Hash, height int64, balances map[string]dcrutil.Amount) error {
	if !wtx.Writable() {
		return fmt.Errorf("unwritable tx")
	}

	tx := wtx.(*transaction)
	accounts := make([]string, 0, len(balances))
	for account, balance := range balances {
		bh := balanceHeight{height: height, balance: balance}
		tx.balances[account] = append(tx.balances[account], bh)
		accounts = append(accounts, account)
	}

	tx.processedBlocks[height] = &processedBlock{
		hash:     blockHash,
		accounts: accounts,
	}

	tx.blockHash = blockHash
	tx.blockHeight = height
	tx.updatedBlock = true

	return nil
}

func (db *MemDB) ProcessedBlockHash(rtx backenddb.ReadTx, height int64) (chainhash.Hash, error) {
	var b *processedBlock
	var ok bool

	tx := rtx.(*transaction)
	if b, ok = tx.processedBlocks[height]; !ok {
		b, ok = db.processedBlocks[height]
		if !ok {
			return chainhash.Hash{}, backenddb.ErrBlockHeightNotFound
		}
	}

	return b.hash, nil
}

// RollbackTip rolls back the current tip. Note this only works if the
// transaction hasn't already modified the tip.
func (db *MemDB) RollbackTip(wtx backenddb.WriteTx, height int64, blockHash chainhash.Hash) error {
	if !wtx.Writable() {
		return fmt.Errorf("unwritable tx")
	}

	tx := wtx.(*transaction)
	lastHash := tx.blockHash
	lastHeight := tx.blockHeight
	if lastHeight == 0 {
		lastHash = db.lastBlockHash
		lastHeight = db.lastHeight
	}

	if lastHash != blockHash || lastHeight != height {
		return backenddb.ErrNotTip
	}

	// Remove balance changes from the accounts. This is technically wrong,
	// in that it removes directly from the db struct instead of storing
	// the change in a journal-like fashion in the tx, but suffices for the
	// current use pattern of the db.
	oldTip := db.processedBlocks[height]
	for _, acct := range oldTip.accounts {
		bals := db.balances[acct]
		if len(bals) == 0 {
			continue
		}

		// We roll back sequentially, so checking the last element of
		// the slice is sufficient.
		if bals[len(bals)-1].height == height {
			bals = bals[:len(bals)-1]
			db.balances[acct] = bals
		}
	}

	// Remove the block from the list of processed blocks.
	delete(db.processedBlocks, height)

	// Store the previous block as the new tip.
	prev := db.processedBlocks[height-1]
	tx.blockHash = prev.hash
	tx.blockHeight = height - 1
	tx.updatedBlock = true

	return nil
}

func (db *MemDB) View(ctx context.Context, f func(tx backenddb.ReadTx) error) error {
	t := &transaction{ctx: ctx}
	db.mtx.Lock()
	defer db.mtx.Unlock()
	return f(t)
}

func (db *MemDB) Update(ctx context.Context, f func(tx backenddb.WriteTx) error) error {
	tx := &transaction{
		ctx:             ctx,
		writable:        true,
		balances:        make(map[string][]balanceHeight),
		processedBlocks: make(map[int64]*processedBlock),
	}
	db.mtx.Lock()
	defer db.mtx.Unlock()

	err := f(tx)
	if err != nil {
		return err
	}

	// Merge the tx stored balances into the main db.
	for account, balances := range tx.balances {
		db.balances[account] = append(db.balances[account], balances...)
	}

	// Record the index of processed blocks.
	for bh, b := range tx.processedBlocks {
		db.processedBlocks[bh] = b
	}

	// Record the last processed height.
	if tx.updatedBlock {
		db.lastBlockHash = tx.blockHash
		db.lastHeight = tx.blockHeight
	}

	return nil
}

func (db *MemDB) Close() error {
	db.mtx.Lock()
	defer db.mtx.Unlock()
	db.balances = nil
	return nil
}
