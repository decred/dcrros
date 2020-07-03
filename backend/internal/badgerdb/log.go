// Copyright (c) 2013-2014 The btcsuite developers
// Copyright (c) 2015-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package badgerdb

import (
	"strings"

	"github.com/decred/slog"
)

var (
	log = slog.Disabled
)

// UseLogger uses a specified Logger to output package logging info.
func UseLogger(logger slog.Logger) {
	log = logger
}

type badgerSlogAdapter struct{}

func (bsa badgerSlogAdapter) Errorf(format string, args ...interface{}) {
	log.Errorf(strings.TrimSpace(format), args...)
}

func (bsa badgerSlogAdapter) Warningf(format string, args ...interface{}) {
	log.Warnf(strings.TrimSpace(format), args...)
}

func (bsa badgerSlogAdapter) Infof(format string, args ...interface{}) {
	log.Infof(strings.TrimSpace(format), args...)
}

func (bsa badgerSlogAdapter) Debugf(format string, args ...interface{}) {
	log.Debugf(strings.TrimSpace(format), args...)
}
