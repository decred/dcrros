// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
)

var (
	chainParams      = chaincfg.SimNetParams()
	prefundAddr, _   = stdaddr.DecodeAddress("SsaJzoa8kcRQEuoy3L4oV65YiioNmDuvdVk", chainParams)
	prefundAmt       = dcrutil.Amount(100000000)
	nonVotingAddr, _ = stdaddr.DecodeAddress("Ssiyo3v8KKDr7zJhi7VBFcrZMh49VarKVLd", chainParams)
	targetAddr, _    = stdaddr.DecodeAddress("SsXiPL51iPAzsxfY84YEs1tQMoo9kfiCj5o", chainParams)

	// revocationTx is filled by genTestChain().
	revocationTx *wire.MsgTx

	debugLevel = flag.String("debuglevel", "info", "logging level. Most common: warn,info,debug")
	appData    = flag.String("appdata", "", "path for root data dir. Use a temp one if empty")
	dbType     = flag.String("dbtype", "badger", "dbtype to use for dcrros tests")
)

func _main() error {
	flag.Parse()
	wg := new(sync.WaitGroup)
	ctx := shutdownListener()

	// Ensure we shut everything down if we exit early and wait for all
	// goroutines to end.
	defer func() {
		if !shutdownRequested(ctx) {
			requestShutdown()
		}
		wg.Wait()
	}()

	// Defer a maximum execution time of 10 minutes.
	go func() {
		select {
		case <-time.After(10 * time.Minute):
			log.Errorf("Test timed out. Cancelling.")
			requestShutdown()
		case <-ctx.Done():
		}
	}()

	dcrdVers, dcrwVers, dcrrosVers, roscliVers, err := procVersions()
	if err != nil {
		return fmt.Errorf("Unable to fetch exe versions: %v", err)
	}

	// App setup.
	rootAppData := *appData
	rootIsTempDir := false
	if rootAppData == "" {
		var err error
		rootAppData, err = ioutil.TempDir("", "dcrros-e2e-")
		if err != nil {
			return err
		}
		rootIsTempDir = true
	}
	if err := cleanRootAppData(rootAppData); err != nil {
		return err
	}
	initLogRotator(filepath.Join(rootAppData, "e2e.log"))
	setLogLevels(*debugLevel)
	if rootIsTempDir {
		log.Infof("App data in temp dir: %s", rootAppData)
	}
	log.Infof("Using dbtype %s", *dbType)
	log.Infof("dcrd version: %s", dcrdVers)
	log.Infof("dcrwallet version: %s", dcrwVers)
	log.Infof("dcrros version: %s", dcrrosVers)
	log.Infof("rosetta-cli version: %s", roscliVers)

	// Run the main dcrd miner.
	log.Infof("Running main dcrd miner")
	miner, err := runMainMiner(ctx, wg, rootAppData)
	if err != nil {
		return fmt.Errorf("Error while running main miner: %v", err)
	}

	// Run the main wallet.
	log.Infof("Running main wallet")
	wallet, err := runMainWallet(ctx, wg, rootAppData)
	if err != nil {
		return fmt.Errorf("Error while running main wallet: %v", err)
	}

	// Generate test chain.
	log.Infof("Generating test chain")
	if err := genTestChain(ctx, miner, wallet); err != nil {
		return fmt.Errorf("Error while generating test chain: %v", err)
	}

	// Run the dcrros instance that is connected to the network ("online").
	log.Infof("Running online dcrros")
	dcrrosOnline, err := runDcrros(ctx, wg, rootAppData, true)
	if err != nil {
		return fmt.Errorf("Error while running online dcrros: %v", err)
	}

	// Run the dcrros instance that is *not* connected to the network
	// ("offline").
	log.Infof("Running offline dcrros")
	dcrrosOffline, err := runDcrros(ctx, wg, rootAppData, false)
	if err != nil {
		return fmt.Errorf("Error while running offline dcrros: %v", err)
	}

	// Wait until the online dcrros is synced to the network and ensure the
	// offline is unsynced.
	log.Infof("Waiting for online dcrros to sync to miner")
	if err := dcrrosOnline.waitSynced(ctx, miner); err != nil {
		return fmt.Errorf("Error while waiting for online dcrros to sync: %v", err)
	}
	log.Debugf("Online dcrros instance synced")
	if err := dcrrosOffline.isUnsynced(ctx); err != nil {
		return fmt.Errorf("Error while checking offline dcrros is unsynced: %v", err)
	}
	log.Debugf("Offline dcrros instance still at genesis")

	// Perform some sanity checks on the online dcrros instance.
	log.Infof("Performing spot checks in online dcrros instance")
	if err := checkTestChain(ctx, dcrrosOnline, miner, wallet); err != nil {
		return fmt.Errorf("Error checking dcrros against test chain: %v", err)
	}

	// Perform the rosetta-cli check:data suite of tests.
	log.Infof("Running check data")
	if err := runRosettaCLICheckData(ctx, rootAppData); err != nil {
		return fmt.Errorf("Error while running rosetta-cli check-data: %v", err)
	}

	// Perform the rosetta-cli check:construction suite of tests.
	log.Infof("Running check construction")
	if err := runRosettaCLICheckConstruction(ctx, rootAppData, miner, wallet); err != nil {
		return fmt.Errorf("Error while running rosetta-cli check:construction: %v", err)
	}

	// Woot!
	return nil
}

func main() {
	startTime := time.Now()
	if err := _main(); err != nil {
		log.Error(err.Error())
		os.Exit(1)
	} else {
		log.Infof("Success! Test took %s", time.Since(startTime).Truncate(100*time.Millisecond))
	}
}
