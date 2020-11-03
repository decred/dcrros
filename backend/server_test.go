// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"
	"errors"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"decred.org/dcrros/backend/backenddb"
	"decred.org/dcrros/backend/internal/memdb"
	"github.com/decred/dcrd/chaincfg/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/stretchr/testify/require"
)

// TestServerProcessesNotifications ensures the server processes notifications
// as they are received from the blockchain.
func TestServerProcessesNotifications(t *testing.T) {
	params := chaincfg.SimNetParams()
	c := newMockChain(t, params)

	c.extendTip()
	c.getBlockChainInfoHook = func(ctx context.Context) (*chainjson.GetBlockChainInfoResult, error) {
		return &chainjson.GetBlockChainInfoResult{
			SyncHeight: 1,
			Blocks:     1,
		}, nil
	}

	// Initialize server.
	db, err := memdb.NewMemDB()
	require.NoError(t, err)
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		db:          db,
		c:           c,
	}
	ctxt, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svr, err := NewServer(ctxt, cfg)
	require.NoError(t, err)

	// Helpful functions to drive the server forward.
	tipHeader := func() []byte {
		header := &c.blocks[c.tipHash].Header
		headerBytes, err := header.Bytes()
		require.NoError(t, err)
		return headerBytes
	}
	extendTip := func(manglers ...blockMangler) []byte {
		c.mtx.Lock()
		defer c.mtx.Unlock()
		c.extendTip(manglers...)
		return tipHeader()
	}
	rewindTip := func() []byte {
		c.mtx.Lock()
		defer c.mtx.Unlock()
		prevHeader := tipHeader()
		c.rewindChain(1)
		return prevHeader
	}

	// Run the server.
	runResult := make(chan error)
	go func() {
		runResult <- svr.Run(ctxt)
	}()

	// Send bogus headers to ensure the server doesn't break.
	svr.onDcrdBlockConnected([]byte{0xff}, nil)
	svr.onDcrdBlockDisconnected([]byte{0xff})

	// Send a bunch of block connect/disconnect events.
	svr.onDcrdBlockConnected(extendTip(), nil)
	svr.onDcrdBlockConnected(extendTip(), nil)
	svr.onDcrdBlockDisconnected(rewindTip())
	svr.onDcrdBlockConnected(extendTip(), nil)
	svr.onDcrdBlockConnected(extendTip(), nil)

	// Server should still be running.
	select {
	case err := <-runResult:
		t.Fatalf("unexpected run error: %v", err)
	case <-time.After(3 * time.Second):
	}

	// Server tip should be the current chain tip.
	hash, height, err := svr.lastProcessedBlock(ctxt)
	require.NoError(t, err)
	if height != c.tipHeight {
		t.Fatalf("unexpected tip height. want=%d got=%d", c.tipHeight, height)
	}
	if *hash != c.tipHash {
		t.Fatalf("unexpected tip hash. want=%s got=%s", c.tipHash, hash)
	}

	// Drain the runResult channel to avoid it leaking.
	go func() {
		<-runResult
	}()
}

// TestRunsAllDBTypes ensures all exported DB types can be used when creating
// and running a server.
func TestRunsAllDBTypes(t *testing.T) {
	params := chaincfg.SimNetParams()
	c := newMockChain(t, params)
	c.extendTip()

	for _, dbtype := range SupportedDBTypes() {
		dbtype := dbtype
		t.Run(string(dbtype), func(t *testing.T) {
			tmpDir, err := ioutil.TempDir("", string(dbtype))
			require.NoError(t, err)
			t.Cleanup(func() { os.RemoveAll(tmpDir) })

			// Initialize server.
			cfg := &ServerConfig{
				ChainParams: params,
				DBType:      dbtype,
				DBDir:       tmpDir,
				c:           c,
			}
			ctxt, cancel := context.WithCancel(context.Background())
			svr, err := NewServer(ctxt, cfg)
			require.NoError(t, err)

			// runDone will receive the result of the Run() call.
			runDone := make(chan error)
			go func() {
				runDone <- svr.Run(ctxt)
			}()

			// Wait for run to stabilize.
			time.Sleep(200 * time.Millisecond)

			// Cancel the Run() call.
			cancel()

			// Ensure it returned a context.Canceled error.
			select {
			case err := <-runDone:
				wantErr := context.Canceled
				if !errors.Is(err, wantErr) {
					t.Fatalf("unexpected error. want=%v, got=%v",
						wantErr, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("timeout waiting for Run() to return")
			}

			// Ensure the DB was actually closed.
			err = svr.db.Close()
			wantErr := backenddb.ErrAlreadyClosed
			if !errors.Is(err, wantErr) {
				t.Fatalf("unexpected error. want=%v, got=%v",
					wantErr, err)
			}
		})
	}
}
