// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func runRosettaCLICheckData(ctx context.Context, rootAppData string) error {
	cliOutputFile, err := os.Create(filepath.Join(rootAppData, "check-data.log"))
	if err != nil {
		return err
	}

	args := []string{
		"--configuration-file=" + filepath.Join(rootAppData, "rosetta-cli.json"),
		"check:data",
	}
	cmd := exec.CommandContext(ctx, "rosetta-cli", args...)
	cmd.Stdout = cliOutputFile
	cmd.Stderr = cliOutputFile
	if err := cmd.Start(); err != nil {
		return err
	}

	waitCheckData := make(chan error)
	go func() {
		waitCheckData <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-waitCheckData:
		return err
	}
}

func runRosettaCLICheckConstruction(ctx context.Context, rootAppData string,
	miner *dcrdProc, wallet *dcrwProc) error {

	cliOutputFile, err := os.Create(filepath.Join(rootAppData, "check-construction.log"))
	if err != nil {
		return err
	}

	args := []string{
		"--configuration-file=" + filepath.Join(rootAppData, "rosetta-cli.json"),
		"check:construction",
	}
	cmd := exec.CommandContext(ctx, "rosetta-cli", args...)
	cmd.Stdout = cliOutputFile
	cmd.Stderr = cliOutputFile
	if err := cmd.Start(); err != nil {
		return err
	}

	// How many outputs to send to the prefund address for
	// check:construction to do its tests.
	nbOutputs := 5

	// Start slow mining blocks, which is needed for the check:construction
	// tests.
	constructionDone := make(chan struct{})
	defer func() { close(constructionDone) }()
	go func() {
		time.Sleep(time.Millisecond * 100)

		// Create the necessary outputs in the prefunded account.
		for i := 0; i < nbOutputs; i++ {
			if err := wallet.sendToAddr(ctx, prefundAddr, prefundAmt); err != nil {
				log.Errorf("Error while prefunding construction addr: %v", err)
			}
		}

		// Slow mine so check:construction will build, broadcast and
		// verify the txs were mined.
		for {
			start := time.Now()
			err := mineAndSyncWallet(ctx, miner, wallet, 1)
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Errorf("Error while trying to mine in "+
					"check:construction: %v", err)
			}

			sleepAmt := time.Second - time.Since(start)
			if sleepAmt <= 0 {
				continue
			}

			select {
			case <-constructionDone:
				return
			case <-time.After(time.Second):
			}
		}
	}()

	waitCheckConstruction := make(chan error)
	go func() {
		waitCheckConstruction <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-waitCheckConstruction:
		return err
	}
}
