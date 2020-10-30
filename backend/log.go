// Copyright (c) 2013-2014 The btcsuite developers
// Copyright (c) 2015-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"
	"sync"
	"time"

	"decred.org/dcrros/backend/internal/badgerdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/slog"
)

var (
	svrLog = slog.Disabled
)

// blocksConnectedLogger takes care of correctly logging the number of
// connected logs after a certain amount of time has passed.
type blocksConnectedLogger struct {
	mtx       sync.Mutex
	count     int
	lastTime  time.Time
	tipHeight int64
	tipHash   chainhash.Hash
}

func (bcl *blocksConnectedLogger) flush() {
	now := time.Now()

	bcl.mtx.Lock()
	delta := now.Sub(bcl.lastTime)
	bcl.lastTime = now
	count := bcl.count
	bcl.count = 0
	tipHeight := bcl.tipHeight
	tipHash := bcl.tipHash
	bcl.mtx.Unlock()

	if count == 0 {
		return
	}

	blocks := "blocks"
	if count == 1 {
		blocks = "block"
	}
	svrLog.Infof("%d %s connected in the last %s. Tip height "+
		"%d hash %s", count, blocks,
		delta.Truncate(100*time.Millisecond),
		tipHeight, tipHash)

}

func (bcl *blocksConnectedLogger) run(ctx context.Context) {
	ticker := time.NewTicker(10*time.Second + 100*time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			bcl.flush()
		}
	}
}

func (bcl *blocksConnectedLogger) inc(tipHeight int64, tipHash *chainhash.Hash) {
	bcl.mtx.Lock()
	bcl.count++
	bcl.tipHeight = tipHeight
	bcl.tipHash = *tipHash
	bcl.mtx.Unlock()
}

// UseLogger uses a specified Logger to output package logging info.
func UseLogger(logger slog.Logger) {
	svrLog = logger
}

// UseBadgerLogger specifies the logger to use for badger db instances.
func UseBadgerLogger(logger slog.Logger) {
	badgerdb.UseLogger(logger)
}
