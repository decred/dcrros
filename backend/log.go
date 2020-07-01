// Copyright (c) 2013-2014 The btcsuite developers
// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"decred.org/dcrros/backend/internal/badgerdb"
	"github.com/decred/slog"
)

var (
	svrLog = slog.Disabled
)

// UseLogger uses a specified Logger to output package logging info.
func UseLogger(logger slog.Logger) {
	svrLog = logger
}

// UseBadgerLogger specifies the logger to use for badger db instances.
func UseBadgerLogger(logger slog.Logger) {
	badgerdb.UseLogger(logger)
}
