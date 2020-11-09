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
	"github.com/decred/dcrd/wire"
	"github.com/stretchr/testify/require"
)

// TestOnDcrdConnected ensures the onDcrdConnected handler behaves as expected
// when (re-)connection is done.
func TestOnDcrdConnected(t *testing.T) {
	// Initialize the server.
	params := chaincfg.RegNetParams()
	otherParams := chaincfg.MainNetParams()

	c := newMockChain(t, params)
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}
	svr, err := NewServer(cfg)
	require.NoError(t, err)

	// Server starts out disconnected.
	wantErr := errDcrdUnconnected
	if err := svr.isDcrdActive(); !errors.Is(err, wantErr) {
		t.Fatalf("unexpected isDcrdActive error. want=%v got=%v",
			wantErr, err)
	}

	// First connection to a suitable chain clears the connection error.
	svr.onDcrdConnected()
	wantErr = nil
	if err := svr.isDcrdActive(); !errors.Is(err, wantErr) {
		t.Fatalf("unexpected isDcrdActive error. want=%v got=%v",
			wantErr, err)
	}

	// Connecting to an unsuitable chain throws an error. Easiest way to
	// force an unsuitable dcrd is to change the network.
	c.params = otherParams
	svr.onDcrdConnected()
	wantErr = errDcrdUnsuitable
	if err := svr.isDcrdActive(); !errors.Is(err, wantErr) {
		t.Fatalf("unexpected isDcrdActive error. want=%v got=%v",
			wantErr, err)
	}

	// Finally, reconnecting to a suitable chain clears the error back.
	c.params = params
	svr.onDcrdConnected()
	wantErr = nil
	if err := svr.isDcrdActive(); !errors.Is(err, wantErr) {
		t.Fatalf("unexpected isDcrdActive error. want=%v got=%v",
			wantErr, err)
	}

}

// TestServerProcessesNotifications ensures the server processes notifications
// as they are received from the blockchain.
func TestServerProcessesNotifications(t *testing.T) {
	t.Parallel()

	params := chaincfg.SimNetParams()
	otherParams := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	c.extendTip()

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
	svr, err := NewServer(cfg)
	require.NoError(t, err)
	t.Cleanup(cancel)

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
	hdr := &wire.BlockHeader{}
	blankHeader, err := hdr.Bytes()
	require.NoError(t, err)

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

	// Wait until Run processed the events.
	time.Sleep(100 * time.Millisecond)

	// Simulate a reconnection to an unsuitable dcrd instance.
	c.mtx.Lock()
	c.params = otherParams
	c.mtx.Unlock()
	svr.onDcrdConnected()

	// Attempt to send notification with bogus headers.
	svr.onDcrdBlockConnected(blankHeader, nil)
	svr.onDcrdBlockDisconnected(blankHeader)

	// Server should still be running.
	select {
	case err := <-runResult:
		t.Fatalf("unexpected run error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	// Reconnect to the right chain.
	c.mtx.Lock()
	c.params = params
	c.mtx.Unlock()
	svr.onDcrdConnected()

	// Perform additional block events.
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

// TestNewErrorsUnknownDBType ensures NewServer fails when passed an unknown DB
// type.
func TestNewErrorsUnknownDBType(t *testing.T) {
	params := chaincfg.SimNetParams()
	c := newMockChain(t, params)
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      DBType("*booo"),
		c:           c,
	}
	_, err := NewServer(cfg)
	wantErr := errUnknownDBType
	if !errors.Is(err, wantErr) {
		t.Fatalf("unexpected error. want=%v, got=%v", wantErr, err)
	}
}

// TestRunsAllDBTypes ensures all exported DB types can be used when creating
// and running a server.
func TestRunsAllDBTypes(t *testing.T) {
	t.Parallel()

	params := chaincfg.SimNetParams()
	c := newMockChain(t, params)
	c.extendTip()

	for _, dbtype := range SupportedDBTypes() {
		dbtype := dbtype
		t.Run(string(dbtype), func(t *testing.T) {
			t.Parallel()

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
			svr, err := NewServer(cfg)
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

// TestRunAPI ensures Run() can't be called with an empty context or twice for
// the same server.
func TestRunAPI(t *testing.T) {
	params := chaincfg.SimNetParams()
	c := newMockChain(t, params)

	// Helper to call Run() and assert it doesn't immediately return with
	// an error. Returns a chan that will get written to once Run() ends.
	runNoErr := func(t *testing.T, svr *Server, ctx context.Context) chan error {
		c := make(chan error)
		go func() {
			c <- svr.Run(ctx)
		}()

		select {
		case err := <-c:
			t.Fatalf("unexpected Run() error: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
		return c
	}

	// Helper to call Run() on a goroutine and assert it returns
	// immediately with an error.
	runWithErr := func(t *testing.T, svr *Server, ctx context.Context) error {
		c := make(chan error)
		go func() {
			c <- svr.Run(ctx)
		}()
		var err error
		select {
		case err = <-c:
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("unexpected timeout while waiting for Run() to error")
		}
		return err
	}

	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}

	t.Run("passing nil context errors", func(t *testing.T) {
		svr, err := NewServer(cfg)
		require.NoError(t, err)
		err = runWithErr(t, svr, nil)
		if err == nil {
			t.Fatalf("unexpected nil error when calling Run(nil)")
		}
	})

	t.Run("passing done context errors", func(t *testing.T) {
		svr, err := NewServer(cfg)
		require.NoError(t, err)
		ctxt, cancel := context.WithCancel(context.Background())
		cancel()
		err = runWithErr(t, svr, ctxt)
		wantErr := context.Canceled
		if !errors.Is(err, wantErr) {
			t.Fatalf("unexpected error. want=%v got=%v", wantErr, err)
		}
	})

	t.Run("call run twice errors", func(t *testing.T) {
		ctxt, cancel := context.WithCancel(context.Background())
		svr, err := NewServer(cfg)
		require.NoError(t, err)

		// First Run() call should work.
		errChan := runNoErr(t, svr, ctxt)

		// Second Run() call while the server is running should fail.
		wantErr := errRunCalledTwice
		err = svr.Run(ctxt)
		if !errors.Is(err, wantErr) {
			t.Fatalf("unexpected error. want=%v, got=%v", wantErr, err)
		}

		// Second Run() call after the original context is closed
		// should fail.
		cancel()
		<-errChan
		err = runWithErr(t, svr, ctxt)
		if !errors.Is(err, wantErr) {
			t.Fatalf("unexpected error. want=%v, got=%v", wantErr, err)
		}

		// Second Run() call with a different context should fail.
		err = svr.Run(context.Background())
		if !errors.Is(err, wantErr) {
			t.Fatalf("unexpected error. want=%v, got=%v", wantErr, err)
		}
	})
}
