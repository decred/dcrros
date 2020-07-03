// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package badgerdb

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/dgraph-io/badger/v2"
)

var (
	// lastProcessedBlockKey is the key to the value that holds the last
	// processed block hash and height.
	//
	// The value is serialized as:
	//
	// [0:32]:  Block Hash
	// [32:40]: Block Height
	lastProcessedBlockKey = []byte("last-processed-block")
)

const (
	// accountBalanceKeyPrefix is the prefix of keys that store balances
	// for accounts at a given height.
	accountBalanceKeyPrefix = "act/"

	// processedBlockHashKeyPrefix is the prefix of keys that store
	// processed block hashes.
	processedBlockHashKeyPrefix = "pb/"

	// blockAccountsChangedPrefix is the prefix of keys that store accounts
	// processed at a given height.
	blockAccountsChangedPrefix = "bac/"
)

func accountBalanceAtHeightKey(account string, height int64) []byte {
	ab := []byte(accountBalanceKeyPrefix + account)
	var hb [8]byte
	binary.BigEndian.PutUint64(hb[:], uint64(height))
	k := make([]byte, len(ab)+1+8)
	copy(k, ab)
	copy(k[len(ab)+1:], hb[:])
	return k
}

func extractAccountBalanceKeyHeight(k []byte) int64 {
	return int64(binary.BigEndian.Uint64(k[len(k)-8:]))
}

func fetchAccountBalanceAt(dbtx *badger.Txn, account string, height int64) (dcrutil.Amount, int64, error) {
	targetKey := accountBalanceAtHeightKey(account, height)
	keyPrefix := []byte(accountBalanceKeyPrefix + account)
	itOpts := badger.IteratorOptions{
		PrefetchValues: true,
		PrefetchSize:   1,
		Reverse:        true,
		Prefix:         keyPrefix, // Correct?
	}
	it := dbtx.NewIterator(itOpts)
	defer it.Close()
	it.Seek(targetKey)
	if !it.ValidForPrefix(keyPrefix) {
		// No entries for this account, so its current balance is zero.
		return 0, 0, nil
	}

	item := it.Item()
	k := item.Key()
	lastHeight := extractAccountBalanceKeyHeight(k)
	var balance dcrutil.Amount
	err := item.Value(func(v []byte) error {
		if len(v) != 8 {
			return fmt.Errorf("wrong size in balance value")
		}
		balance = dcrutil.Amount(binary.BigEndian.Uint64(v))
		return nil
	})
	if err != nil {
		return 0, 0, err
	}

	return balance, lastHeight, nil
}

func putAccountBalanceAt(dbtx *badger.Txn, account string, height int64, balance dcrutil.Amount) error {
	k := accountBalanceAtHeightKey(account, height)
	var v [8]byte
	binary.BigEndian.PutUint64(v[:], uint64(balance))
	return dbtx.Set(k, v[:])
}

func delAccountBalanceAt(dbtx *badger.Txn, account string, height int64) error {
	k := accountBalanceAtHeightKey(account, height)
	return dbtx.Delete(k)
}

func putLastProcessedAccountBlock(dbtx *badger.Txn, hash *chainhash.Hash, height int64) error {
	var v [32 + 8]byte
	copy(v[:], hash[:])
	binary.BigEndian.PutUint64(v[32:], uint64(height))
	return dbtx.Set(lastProcessedBlockKey, v[:])
}

func fetchLastProcessedAccountBlock(dbtx *badger.Txn) (chainhash.Hash, int64, error) {
	var hash chainhash.Hash
	item, err := dbtx.Get(lastProcessedBlockKey)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return hash, 0, nil
	}
	if err != nil {
		return hash, 0, err
	}
	var height int64
	err = item.Value(func(v []byte) error {
		copy(hash[:], v[:32])
		height = int64(binary.BigEndian.Uint64(v[32:]))
		return nil
	})
	return hash, height, err
}

func processedBlockKey(height int64) []byte {
	key := make([]byte, len(processedBlockHashKeyPrefix)+8)
	k := key[copy(key[:], processedBlockHashKeyPrefix):]
	binary.BigEndian.PutUint64(k, uint64(height))
	return key
}

func putProcessedBlock(dbtx *badger.Txn, hash *chainhash.Hash, height int64) error {
	k := processedBlockKey(height)
	var v [32]byte
	copy(v[:], hash[:])
	return dbtx.Set(k, v[:])
}

func delProcessedBlock(dbtx *badger.Txn, height int64) error {
	k := processedBlockKey(height)
	return dbtx.Delete(k)
}

func fetchProcessedBlockHash(dbtx *badger.Txn, height int64) (chainhash.Hash, error) {
	var hash chainhash.Hash
	k := processedBlockKey(height)
	item, err := dbtx.Get(k)
	if err != nil {
		return hash, err
	}
	err = item.Value(func(v []byte) error {
		copy(hash[:], v[:32])
		return nil
	})
	return hash, err
}

func blockAccountKey(height int64, account string) []byte {
	key := make([]byte, len(blockAccountsChangedPrefix)+8+len(account))
	k := key[copy(key, blockAccountsChangedPrefix):]
	binary.BigEndian.PutUint64(k, uint64(height))
	k = k[8:]
	copy(k, account)
	return key
}

func putBlockAccount(dbtx *badger.Txn, height int64, account string) error {
	return dbtx.Set(blockAccountKey(height, account), nil)
}

func fetchBlockAccounts(dbtx *badger.Txn, height int64) ([]string, error) {
	keyPrefix := blockAccountKey(height, "")
	accounts := make([]string, 0)
	itOpts := badger.DefaultIteratorOptions
	itOpts.PrefetchValues = false
	itOpts.Prefix = keyPrefix
	it := dbtx.NewIterator(itOpts)
	defer it.Close()
	for it.Rewind(); it.ValidForPrefix(keyPrefix); it.Next() {
		k := it.Item().Key()
		s := make([]byte, len(k)-len(keyPrefix))
		copy(s, k[len(keyPrefix):])
		accounts = append(accounts, string(s))
	}

	return accounts, nil
}
