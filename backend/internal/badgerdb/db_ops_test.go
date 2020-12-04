// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package badgerdb

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
)

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// TestAccountUtxoKey ensures the accountUtxoKey function works as expected.
func TestAccountUtxoKey(t *testing.T) {
	// hex2bs decodes the given hex string to a []byte then type casts that
	// into a string.
	hex2bs := func(s string) string {
		return string(mustHex(s))
	}

	testCases := []struct {
		name     string
		account  string
		outpoint *wire.OutPoint
		wantKey  string
	}{{
		name:    "account and outpoint defined",
		account: "acct01",
		outpoint: &wire.OutPoint{
			Hash:  chainhash.Hash{0: 0xff, 31: 0xee},
			Index: 0x01020304,
			Tree:  0x7b,
		},
		wantKey: "autxos/acct01/" +
			hex2bs("ff000000000000000000000000000000000000000000000000000000000000ee") +
			hex2bs("01020304") +
			hex2bs("7b"),
	}, {
		name:    "empty account",
		account: "",
		outpoint: &wire.OutPoint{
			Hash:  chainhash.Hash{0: 0xff, 31: 0xee},
			Index: 0x01020304,
			Tree:  0x7b,
		},
		wantKey: "autxos//" +
			hex2bs("ff000000000000000000000000000000000000000000000000000000000000ee") +
			hex2bs("01020304") +
			hex2bs("7b"),
	}, {
		name:     "nil outpoint",
		account:  "acct01",
		outpoint: nil,
		wantKey:  "autxos/acct01/",
	}, {
		name:     "nil outpoint and empty account",
		account:  "",
		outpoint: nil,
		wantKey:  "autxos//",
	}}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gotKey := accountUtxoKey(tc.account, tc.outpoint)
			wantKey := []byte(tc.wantKey)
			if !bytes.Equal(gotKey, wantKey) {
				t.Fatalf("unexpected key. want=%x got=%x",
					wantKey, gotKey)
			}
		})
	}
}

// TestExtractAccountUtxoKeyOutpoint ensures the extractAccountUtxoKeyOutpoint
// function works as expected.
func TestExtractAccountUtxoKeyOutpoint(t *testing.T) {
	hex2bs := func(s string) string {
		return string(mustHex(s))
	}

	key := []byte("autxos/acct01/" +
		hex2bs("ff000000000000000000000000000000000000000000000000000000000000ee") +
		hex2bs("01020304") +
		hex2bs("7b"))
	gotOutpoint := extractAccountUtxoKeyOutpoint(key)
	wantOutpoint := wire.OutPoint{
		Hash:  chainhash.Hash{0: 0xff, 31: 0xee},
		Index: 0x01020304,
		Tree:  0x7b,
	}
	if gotOutpoint != wantOutpoint {
		t.Fatalf("unexpected outpoint. want=%#v got=%#v", wantOutpoint,
			gotOutpoint)
	}
}
