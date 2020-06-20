package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"time"

	rserver "github.com/coinbase/rosetta-sdk-go/server"
	"github.com/decred/dcrros/internal/version"
)

func _main() error {
	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	log.Infof("Initing dcr-rosetta server v%s on %s", version.String(), cfg.activeNet)

	ctx := shutdownListener()

	router := rserver.NewRouter()
	svr := &http.Server{
		Handler: router,
	}

	listeners, err := cfg.listeners()
	if err != nil {
		log.Error(err.Error())
		return err
	}

	for _, l := range listeners {
		go func(l net.Listener) {
			log.Infof("Listening on %s", l.Addr().String())
			svr.Serve(l)
		}(l)
	}

	// Wait until the app is commanded to shutdown to close the server.
	select {
	case <-ctx.Done():
	}

	// Wait up to 5 seconds until all connections are gracefully closed
	// before terminating the process.
	timeout, cancel := context.WithTimeout(context.Background(), time.Second*5)
	err = svr.Shutdown(timeout)
	cancel() // Prevent context leak.
	if errors.Is(err, context.Canceled) {
		log.Errorf("Terminating process before all connections were done")
	}

	return nil
}

func main() {
	if err := _main(); err != nil && err != errCmdDone {
		os.Exit(1)
	}
}
