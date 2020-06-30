package backenddb

import (
	"context"
	"errors"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
)

var (
	ErrBlockHeightNotFound = errors.New("block height not found")

	ErrNotTip = errors.New("specified block was not the tip")
)

type ReadTx interface {
	Context() context.Context
}

type WriteTx interface {
	ReadTx
	Writable() bool
}

type DB interface {
	Balance(tx ReadTx, accountAddr string, height int64) (dcrutil.Amount, error)

	LastProcessedBlock(tx ReadTx) (chainhash.Hash, int64, error)

	ProcessedBlockHash(tx ReadTx, height int64) (chainhash.Hash, error)

	RollbackTip(tx WriteTx, height int64, blockHash chainhash.Hash) error

	StoreBalances(tx WriteTx, blockHash chainhash.Hash, height int64, balances map[string]dcrutil.Amount) error

	View(ctx context.Context, f func(tx ReadTx) error) error

	Update(ctx context.Context, f func(tx WriteTx) error) error

	Close() error
}
