package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"sync"
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
	ctx := shutdownListener()
	var wg sync.WaitGroup

	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	// Run dcrd if we were instructed to.
	if cfg.RunDcrd != "" {
		// IPC pipes so we can get an event when dcrd's startup is
		// done. ros* is the dcrros end of the pipe and dcr* is the
		// dcrd end of the pipe.
		rosPipeRx, dcrPipeTx, err := os.Pipe()
		if err != nil {
			return fmt.Errorf("unable to create dcrd ipc pipes: %v", err)
		}
		dcrPipeRx, rosPipeTx, err := os.Pipe()
		if err != nil {
			return fmt.Errorf("unable to create second dcrd ipc pipes: %v", err)
		}

		// Channel that will be signalled when dcrd's startup is done.
		dcrdStartupDoneChan := make(chan struct{})

		// Prepare the cmd and start dcrd.
		args := cfg.dcrdArgs()
		args = append(args, "--pipetx=3", "--piperx=4", "--lifetimeevents")
		log.Debugf("Starting dcrd at %s with args %v", cfg.RunDcrd, args)
		dcrd := exec.Command(cfg.RunDcrd, args...)
		dcrd.Stdout = os.Stdout
		dcrd.Stderr = os.Stderr
		dcrd.ExtraFiles = []*os.File{dcrPipeTx, dcrPipeRx}
		if err := dcrd.Start(); err != nil {
			return err
		}

		// Create the goroutine that watches over the dcrd process.
		dcrdWaitChan := make(chan error)
		go func() {
			dcrdWaitChan <- dcrd.Wait()
		}()

		// Create the goroutine which handles dcrd closing. If dcrros
		// is commanded to shutdown, this closes dcrd and waits for it
		// finish. Otherwise, dcrd stopped before it should have
		// (panic, etc), so report an error and quit.
		wg.Add(1)
		go func() {
			select {
			case <-ctx.Done():
				// Request dcrd to shutdown and wait for it to
				// return.
				log.Debugf("Requesting dcrd process close")
				rosPipeTx.Close()
				err := <-dcrdWaitChan
				if err != nil {
					log.Warnf("Dcrd process returned an error: %v", err)
				} else {
					log.Infof("Dcrd process finished successfully")
				}
			case err := <-dcrdWaitChan:
				// We're not exiting yet, so if dcrd is closing
				// we must close as well.
				log.Errorf("Dcrd process finished prematurely: %v", err)
				requestShutdown()
			}
			wg.Done()
		}()

		// Drain our end of the IPC pipe and close the signalling chan
		// if we receive an appropriate event.
		go func() {
			event := make([]byte, 1+1+13+4+2)
			for !shutdownRequested(ctx) {
				// The IPC facility is really simple and we've
				// only enabled lifetimevents so do a simple
				// reading instead of a full-blown message
				// parsing.
				n, err := rosPipeRx.Read(event)
				if err != nil {
					log.Errorf("Failed to read dcrd IPC pipe: %v", err)
					return
				}
				if n != len(event) {
					continue
				}
				if string(event[2:15]) != "lifetimeevent" {
					continue
				}
				if event[19] == 1 && event[20] == 0 {
					close(dcrdStartupDoneChan)
					return
				}
			}
		}()

		// Wait for the startup done chan to be signalled or a
		// shutdown.
		select {
		case <-ctx.Done():
			requestShutdown()
			wg.Wait()
			return fmt.Errorf("Dcrd never signalled startup done")
		case <-dcrdStartupDoneChan:
			log.Debugf("Dcrd startup signal received")
			// Startup can now proceed.
		}
	}

	svrCfg, err := cfg.serverConfig()
	if err != nil {
		requestShutdown()
		wg.Wait()
		return err
	}

	// Attempt an early connection to the dcrd server and verify if it's a
	// reasonable backend for dcrros operations.
	err = backend.CheckDcrd(ctx, svrCfg)
	if err != nil {
		requestShutdown()
		wg.Wait()
		return fmt.Errorf("error while checking underlying "+
			"dcrd: %v", err)
	}

	log.Infof("Initing dcr-rosetta server v%s on %s", version.String(), cfg.activeNet)

	// Enable http profiling server if requested.
	if cfg.Profile != "" {
		go func() {
			listenAddr := cfg.Profile
			log.Infof("Creating profiling server "+
				"listening on %s", listenAddr)
			profileRedirect := http.RedirectHandler("/debug/pprof",
				http.StatusSeeOther)
			http.Handle("/", profileRedirect)
			err := http.ListenAndServe(listenAddr, nil)
			if err != nil {
				log.Errorf(err.Error())
				requestShutdown()
			}
		}()
	}

	drsvr, err := backend.NewServer(ctx, svrCfg)
	if err != nil {
		requestShutdown()
		wg.Wait()
		return err
	}
	wg.Add(1)
	go func() {
		// Errors other than context canceled are fatal.
		err := drsvr.Run(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Errorf("Fatal error running the server instance: %v", err)
			requestShutdown()
		}
		wg.Done()
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
		requestShutdown()
		wg.Wait()
		return err
	}

	for _, l := range listeners {
		wg.Add(1)
		go func(l net.Listener) {
			log.Infof("Listening on %s", l.Addr().String())
			svr.Serve(l)
			wg.Done()
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

	// Wait for all goroutines to finish.
	wg.Wait()

	return nil
}

func main() {
	if err := _main(); err != nil && err != errCmdDone {
		fmt.Println(err)
		os.Exit(1)
	}
}
