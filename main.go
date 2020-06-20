package main

import (
	"github.com/decred/dcrros/internal/version"
)

func main() {
	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	cfg, _, err := loadConfig()
	if err != nil {
		return
	}

	log.Infof("Initing dcr-rosetta server v%s on %s", version.String(), cfg.activeNet)
}
