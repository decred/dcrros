package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	rserver "github.com/coinbase/rosetta-sdk-go/server"
	"github.com/decred/dcrros/backend"
	"github.com/decred/dcrros/internal/version"
)

type routerify rserver.Route

func (r routerify) Routes() rserver.Routes {
	return rserver.Routes{rserver.Route(r)}
}

func _main() error {
	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	svrCfg, err := cfg.serverConfig()
	if err != nil {
		fmt.Println(err)
		return err
	}

	log.Infof("Initing dcr-rosetta server v%s on %s", version.String(), cfg.activeNet)

	ctx := shutdownListener()

	drsvr, err := backend.NewServer(ctx, svrCfg)
	if err != nil {
		log.Errorf("Error creating server instance: %v", err)
		return err
	}
	go func() {
		// Errors other than context canceled are fatal.
		err := drsvr.Run(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Errorf("Fatal error running the server instance: %v", err)
			requestShutdown()
		}
	}()

	index := rserver.Route{
		Name:    "index",
		Method:  "GET",
		Pattern: "/",
		HandlerFunc: func(w http.ResponseWriter, req *http.Request) {
			s := fmt.Sprintf("dcr rosetta server\n%s\n", version.String())
			w.Write([]byte(s))
		},
	}
	routes := append(drsvr.Routers(), routerify(index))
	router := rserver.NewRouter(routes...)
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
