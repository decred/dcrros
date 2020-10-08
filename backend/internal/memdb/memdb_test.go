// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package memdb

import (
	"testing"

	"decred.org/dcrros/backend/backenddb"
)

// TestMemDBImpl tests that the MemDB implementation fulfills the backenddb
// interface requirements.
func TestMemDBImpl(t *testing.T) {
	db, err := NewMemDB()
	if err != nil {
		t.Fatal(err)
	}

	backenddb.RunTests(t, db, backenddb.TestCases())
}
