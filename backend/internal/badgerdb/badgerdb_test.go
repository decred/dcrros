// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package badgerdb

import (
	"io/ioutil"
	"os"
	"testing"

	"decred.org/dcrros/backend/backenddb"
)

// TestBadgerDBImpl tests that the badger DB implementation fulfills the
// backenddb interface requirements.
func TestBadgerDBImpl(t *testing.T) {
	dir, err := ioutil.TempDir("", "dcrros_badgerdb")
	if err != nil {
		t.Fatal(err)
	}

	db, err := NewBadgerDB(dir)
	if err != nil {
		t.Fatal(err)
	}

	ok := backenddb.RunTests(t, db, backenddb.TestCases())
	switch ok {
	case true:
		if err := os.RemoveAll(dir); err != nil {
			t.Logf("unable to remove test db dir '%s': %v", dir, err)
		}

	case false:
		t.Logf("Test db dir at '%s'", dir)
	}
}

// TestBadgerDBImpl tests that the badger DB implementation fulfills the
// backenddb interface requirements when using a memory-only badger instance.
func TestBadgerMemDBImpl(t *testing.T) {
	db, err := NewBadgerDB("")
	if err != nil {
		t.Fatal(err)
	}

	backenddb.RunTests(t, db, backenddb.TestCases())
}
