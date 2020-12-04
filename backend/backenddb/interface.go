// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backenddb

import (
	"context"
	"errors"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/wire"
)

var (
	// ErrBlockHeightNotFound indicates the given block height is not in
	// the DB.
	ErrBlockHeightNotFound = errors.New("block height not found")

	// ErrNotTip indicates the passed block hash/height do not correspond
	// to the tip block.
	ErrNotTip = errors.New("specified block was not the tip")

	// ErrBlockAlreadyProcessed indicates the passed block has already been
	// processed and can't be updated without a previous rollback.
	ErrBlockAlreadyProcessed = errors.New("block already processed")

	// ErrNotExtendingTip is returned when attempting to extend the DB
	// chain with a height that is not a child of the current tip.
	ErrNotExtendingTip = errors.New("block not extending the current tip")

	// ErrAlreadyClosed is returned when attempting to close a DB that has
	// already been closed.
	ErrAlreadyClosed = errors.New("DB already closed")
)

// ReadTx represents a read-only transaction.
type ReadTx interface {
	// Context is the underlying context used to start the transaction.
	Context() context.Context
}

// WriteTx represents a read-write transaction.
type WriteTx interface {
	ReadTx

	// Writable always returns true for writable transactions.
	Writable() bool
}

type DB interface {
	// Balance returns the latest balance for the given account address
	// that is at a height higher than the specified height.
	//
	// If the account does not have any balance associated with it, it
	// returns zero.
	Balance(tx ReadTx, accountAddr string, height int64) (dcrutil.Amount, error)

	// LastProcessedBlock returns the block hash and height corresponding
	// to the last call to StoreBalances().
	LastProcessedBlock(tx ReadTx) (chainhash.Hash, int64, error)

	// ProcessedBlockHash returns the block hash for the block processed at
	// the specified height. If the height is after the height of the most
	// recent call to StoreBalances(), this returns ErrBlockHeightNotFound.
	ProcessedBlockHash(tx ReadTx, height int64) (chainhash.Hash, error)

	// RollbackTip removes the given block from the database, along with
	// all recorded account balances, as long as the current database tip
	// is the specified hash and height (as returned by
	// LastProcessedBlock).
	//
	// If the given hash and height are not the tip, ErrNotTip is returned.
	RollbackTip(tx WriteTx, blockHash chainhash.Hash, height int64) error

	// StoreBalances stores all the balances found at the given block and
	// marks that block height as processed by the given block hash.
	//
	// Note that this should only be called once for some given block
	// hash/height and it returns ErrBlockAlreadyProcessed if the given
	// block height has already been processed.
	//
	// If the passed height does not extend the current chain tip,
	// ErrNotExtendingTip is returned.
	StoreBalances(tx WriteTx, blockHash chainhash.Hash, height int64, balances map[string]dcrutil.Amount) error

	// AddUtxo associates the given oupoint and amount as an unspent output
	// for the given account.
	AddUtxo(wtx WriteTx, account string, outpoint *wire.OutPoint, amt dcrutil.Amount) error

	// DelUtxo removes the given outpoint as an unspent output of the given
	// account.
	DelUtxo(wtx WriteTx, account string, outpoint *wire.OutPoint) error

	// ListUtxos lists all unspent outputs of the given account.
	ListUtxos(rtx ReadTx, account string) (map[wire.OutPoint]dcrutil.Amount, error)

	// View starts a read-only db transaction and executes the given
	// function within its context.
	View(ctx context.Context, f func(tx ReadTx) error) error

	// Update starts a read-write transaction and executes the given
	// function within its context. If the function returns an error, then
	// any DB changes are *not* committed.
	Update(ctx context.Context, f func(tx WriteTx) error) error

	// Close the DB. If the DB is already closed it should return
	// ErrAlreadyClosed.
	Close() error
}
