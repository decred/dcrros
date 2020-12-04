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
	"github.com/decred/dcrd/wire"
)

type balanceHeight struct {
	height  int64
	balance dcrutil.Amount
}

type processedBlock struct {
	hash     chainhash.Hash
	accounts []string
}

type utxo struct {
	outp wire.OutPoint
	amt  dcrutil.Amount
}

type transaction struct {
	ctx             context.Context
	writable        bool
	balances        map[string][]balanceHeight
	updatedBlock    bool
	blockHash       chainhash.Hash
	blockHeight     int64
	processedBlocks map[int64]*processedBlock
	utxos           map[string]map[wire.OutPoint]dcrutil.Amount
	delUtxos        map[string]map[wire.OutPoint]struct{}
}

func (t *transaction) Context() context.Context {
	return t.ctx
}

func (t *transaction) Writable() bool {
	return t.writable
}

// MemDB is a backenddb.DB implementation that holds data only in memory.
//
// Note that this implementation is not a fully featured DB and is only usable
// according to the current pattern of calls for the backenddb.
type MemDB struct {
	balances        map[string][]balanceHeight
	lastBlockHash   chainhash.Hash
	lastHeight      int64
	processedBlocks map[int64]*processedBlock
	mtx             sync.Mutex

	// Tracking of utxos per account.
	//
	// We track utxos in three "buckets" (maps): one that maps an account
	// to a single utxo (experimentally, > 90% of accounts fall within this
	// case), one for "a few" utxos (~7% of accounts) and one for the long
	// tail of other accounts.
	//
	// This design is used because it has been empirically determined to
	// offer the best tradeoff between memory and cpu usage.
	singleUtxos map[string]*utxo
	fewUtxos    map[string][]*utxo
	utxos       map[string]map[wire.OutPoint]dcrutil.Amount
}

// NewMemDB returns a new instance of a MemDB.
func NewMemDB() (*MemDB, error) {
	return &MemDB{
		balances:        make(map[string][]balanceHeight),
		processedBlocks: make(map[int64]*processedBlock),
		singleUtxos:     make(map[string]*utxo),
		fewUtxos:        make(map[string][]*utxo),
		utxos:           make(map[string]map[wire.OutPoint]dcrutil.Amount),
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

	// Do not reprocess a block.
	if tx.updatedBlock && tx.blockHeight == height {
		return backenddb.ErrBlockAlreadyProcessed
	}
	if db.lastHeight == height {
		return backenddb.ErrBlockAlreadyProcessed
	}

	// Do not extend non-tip blocks.
	if tx.updatedBlock && height != tx.blockHeight+1 {
		return backenddb.ErrNotExtendingTip
	}
	if !tx.updatedBlock && height != db.lastHeight+1 {
		return backenddb.ErrNotExtendingTip
	}

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
func (db *MemDB) RollbackTip(wtx backenddb.WriteTx, blockHash chainhash.Hash, height int64) error {
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
	if oldTip == nil {
		// Haven't processed this yet.
		return nil
	}

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

// AddUtxo adds the specified utxo to the db.
func (db *MemDB) AddUtxo(wtx backenddb.WriteTx, account string,
	outpoint *wire.OutPoint, amount dcrutil.Amount) error {

	if !wtx.Writable() {
		return fmt.Errorf("unwritable tx")
	}
	tx := wtx.(*transaction)

	// Add this new utxo to the tx.
	acctUtxos := tx.utxos[account]
	if acctUtxos == nil {
		acctUtxos = make(map[wire.OutPoint]dcrutil.Amount, 1)
		tx.utxos[account] = acctUtxos
	}
	acctUtxos[*outpoint] = amount

	// If the utxo was previously scheduled to be deleted, cancel its
	// deletion.
	delAcctUtxos := tx.delUtxos[account]
	delete(delAcctUtxos, *outpoint)

	return nil
}

// DelUtxo removes the given utxo from the db.
func (db *MemDB) DelUtxo(wtx backenddb.WriteTx, account string,
	outpoint *wire.OutPoint) error {

	if !wtx.Writable() {
		return fmt.Errorf("unwritable tx")
	}
	tx := wtx.(*transaction)

	// If the utxo was added to the tx, remove it.
	acctUtxos := tx.utxos[account]
	delete(acctUtxos, *outpoint)

	// Schedule this outpoint for removal from the db when the tx is
	// committed.
	delAcctUtxos := tx.delUtxos[account]
	if delAcctUtxos == nil {
		delAcctUtxos = make(map[wire.OutPoint]struct{})
		tx.delUtxos[account] = delAcctUtxos
	}
	delAcctUtxos[*outpoint] = struct{}{}

	return nil
}

// ListUtxos lists the utxos in the database for a given account.
func (db *MemDB) ListUtxos(rtx backenddb.ReadTx, account string) (map[wire.OutPoint]dcrutil.Amount, error) {
	tx := rtx.(*transaction)
	res := make(map[wire.OutPoint]dcrutil.Amount)

	// First check utxos that exist in the db. Since we store utxos in
	// three different maps, we need to check each one.
	if utxo, ok := db.singleUtxos[account]; ok {
		res[utxo.outp] = utxo.amt
	}
	if utxos, ok := db.fewUtxos[account]; ok {
		for _, utxo := range utxos {
			res[utxo.outp] = utxo.amt
		}
	}
	if utxos, ok := db.utxos[account]; ok {
		for outp, amt := range utxos {
			res[outp] = amt
		}
	}

	// Next, add utxos from the tx itself.
	for outp, amt := range tx.utxos[account] {
		res[outp] = amt
	}

	// Finally, if there are any utxos scheduled for removal in the current
	// tx, remove them from the resulting map.
	for outp := range tx.delUtxos[account] {
		delete(res, outp)
	}

	return res, nil
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
		utxos:           make(map[string]map[wire.OutPoint]dcrutil.Amount),
		delUtxos:        make(map[string]map[wire.OutPoint]struct{}),
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

	// fewUtxoLimit is the limit for entries in the "fewUtxos" bucket. This
	// is chosen as a compromise between instantiaing a map that may
	// potentially hold a small number of utxos versus the added cpu time
	// to add and delete elements from the slice version.
	fewUtxoLimit := 12

	// Update the account utxos index.
	for acct, utxos := range tx.utxos {
		// Fetch the current utxos for the specified account.
		acctUtxos := db.utxos[acct]
		firstUtxo := db.singleUtxos[acct]
		lenFirstUtxo := 0
		if firstUtxo != nil {
			lenFirstUtxo++
		}
		fewUtxos := db.fewUtxos[acct]

		// First case: adding a single utxo to a previously unseen
		// account, record it in the singleUtxos map.
		if acctUtxos == nil && fewUtxos == nil && firstUtxo == nil && len(utxos) == 1 {
			for outp, amt := range utxos {
				db.singleUtxos[acct] = &utxo{outp: outp, amt: amt}
			}
			continue
		}

		// Second case, if the total nb of utxos is less than
		// fewUtxoLimit, store in the fewUtxos map.
		if acctUtxos == nil && len(utxos)+len(fewUtxos)+lenFirstUtxo < fewUtxoLimit {
			if fewUtxos == nil {
				// Initialize the array for the first time.
				fewUtxos = make([]*utxo, 0, len(utxos)+lenFirstUtxo)
			}
			if firstUtxo != nil {
				// Add the first utxo as needed.
				fewUtxos = append(fewUtxos, firstUtxo)
				delete(db.singleUtxos, acct)
			}
			// Add the new utxos.
			for outp, amt := range utxos {
				fewUtxos = append(fewUtxos, &utxo{outp: outp, amt: amt})
			}
			db.fewUtxos[acct] = fewUtxos
			continue
		}

		// Handle the long tail of accounts with multiple utxos.

		if acctUtxos == nil {
			// Initialize the array for the first time.
			szHint := len(utxos) + len(fewUtxos) + lenFirstUtxo
			acctUtxos = make(map[wire.OutPoint]dcrutil.Amount, szHint)
			db.utxos[acct] = acctUtxos
		}
		if firstUtxo != nil {
			// Add the first utxo if defined.
			acctUtxos[firstUtxo.outp] = firstUtxo.amt
			delete(db.singleUtxos, acct)
		}
		if fewUtxos != nil {
			// Add all the first few utxos if defined.
			for _, utxo := range fewUtxos {
				acctUtxos[utxo.outp] = utxo.amt
			}
			delete(db.fewUtxos, acct)
		}

		// Add all remaining utxos.
		for outp, amt := range utxos {
			acctUtxos[outp] = amt
		}
	}

	// Remove all utxos.
	for acct, delUtxos := range tx.delUtxos {
		firstUtxo := db.singleUtxos[acct]
		fewUtxos := db.fewUtxos[acct]
		acctUtxos := db.utxos[acct]
		for outp := range delUtxos {
			// Safe to always remove from the long tail utxo map.
			delete(acctUtxos, outp)

			// The firstUtxo is either the one we want to delete or
			// not.
			if firstUtxo != nil && firstUtxo.outp == outp {
				delete(db.singleUtxos, acct)
			}

			// The fewUtxos are a simple slice, so we need to
			// remove it by iterating, finding the appropriate
			// entry, then re-slicing.
			for i := 0; i < len(fewUtxos); {
				if fewUtxos[i].outp == outp {
					l := len(fewUtxos)
					fewUtxos[i] = fewUtxos[l-1]
					fewUtxos[l-1] = nil
					fewUtxos = fewUtxos[:l-1]
					db.fewUtxos[acct] = fewUtxos
					break
				} else {
					i++
				}
			}
		}
	}

	return nil
}

func (db *MemDB) Close() error {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	if db.balances == nil {
		return backenddb.ErrAlreadyClosed
	}
	db.balances = nil
	return nil
}
