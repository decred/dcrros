// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/decred/dcrd/rpcclient/v7"
	"github.com/decred/dcrd/rpctest"
)

type dcrdProc struct {
	c *rpcclient.Client
}

func (d *dcrdProc) waitTxInMempool(ctx context.Context, txh string) error {
	for i := 0; i < 10; i++ {
		mempool, err := d.c.GetRawMempool(ctx, chainjson.GRMAll)
		if err != nil {
			return err
		}
		for _, memtx := range mempool {
			if memtx.String() == txh {
				err = d.c.RegenTemplate(ctx)
				if err != nil {
					return err
				}

				return nil
			}
		}
		time.Sleep(time.Second)
	}

	return fmt.Errorf("tx %s not found in mempool", txh)
}

func (d *dcrdProc) mine(ctx context.Context, nb uint32) ([]*chainhash.Hash, error) {
	blocks := make([]*chainhash.Hash, nb)
	for i := uint32(0); i < nb; i++ {
		bl, err := rpctest.AdjustedSimnetMiner(ctx, d.c, 1)
		if err != nil {
			return nil, err
		}

		blocks = append(blocks, bl[0])
		time.Sleep(100 * time.Millisecond)
	}

	return blocks, nil
}

func (d *dcrdProc) treasuryBalance(ctx context.Context) (dcrutil.Amount, error) {
	res, err := d.c.GetTreasuryBalance(ctx, nil, true)
	if err != nil {
		return 0, err
	}

	balance := dcrutil.Amount(res.Balance)

	// Now, sum up all treasury bases that happened in the last
	// CoinbaseMaturity blocks, since dcrros does not keep track of
	// maturing balances.
	height := res.Height
	for i := 0; i <= int(chainParams.CoinbaseMaturity); i++ {
		for _, up := range res.Updates {
			balance += dcrutil.Amount(up)
		}

		height = height - 1
		hash, err := d.c.GetBlockHash(ctx, height)
		if err != nil {
			return 0, err
		}

		res, err = d.c.GetTreasuryBalance(ctx, hash, true)
		if err != nil {
			return 0, err
		}
	}

	return balance, nil
}

func (dcrd *dcrdProc) nbLiveTickets(ctx context.Context) (int, error) {
	liveTickets, err := dcrd.c.LiveTickets(ctx)
	return len(liveTickets), err
}

func runMainMiner(ctx context.Context, wg *sync.WaitGroup, rootAppData string) (*dcrdProc, error) {
	rosPipeRx, dcrPipeTx, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("unable to create dcrd ipc pipes: %v", err)
	}

	args := []string{
		"--simnet",
		"--appdata=" + filepath.Join(rootAppData, "main-dcrd"),
		"--rpcuser=USER",
		"--rpcpass=PASSWORD",
		"--listen=127.0.0.1:19555",
		"--rpclisten=127.0.0.1:19556",
		"--nobanning",
		"--pipetx=3",
		"--debuglevel=debug",
		"--lifetimeevents",
		"--miningaddr=Sse143dKKXu6LQm5TNfpjcAYBbuuLQ3DKCj",
		"--norelaypriority",
		"--minrelaytxfee=0.0000001",
		"--txindex",
	}

	dcrdOutputFile, err := os.Create(filepath.Join(rootAppData, "main-dcrd.log"))
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("dcrd", args...)
	cmd.ExtraFiles = []*os.File{dcrPipeTx}
	cmd.Stdout = dcrdOutputFile
	cmd.Stderr = dcrdOutputFile
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Create the goroutine that watches over the dcrd process.
	dcrdWaitChan := make(chan error)
	go func() {
		dcrdWaitChan <- cmd.Wait()
	}()
	wg.Add(1)
	go func() {
		<-ctx.Done()
		cmd.Process.Signal(os.Interrupt)
	}()

	// Channel that will be signalled when dcrd's startup is done.
	dcrdStartupDoneChan := make(chan struct{})

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
		return nil, fmt.Errorf("dcrd never signalled startup done")
	case errWait := <-dcrdWaitChan:
		return nil, fmt.Errorf("dcrd errored while waiting: %v", errWait)
	case <-dcrdStartupDoneChan:
		log.Debugf("dcrd startup signal received")
		// Startup can now proceed.
	}

	go func() {
		err := <-dcrdWaitChan
		switch {
		case err != nil:
			log.Warnf("Main dcrd process finished with error: %v", err)
		default:
			log.Debugf("Main dcrd process finished")
		}
		dcrdOutputFile.Close()
		wg.Done()
	}()

	cert, err := ioutil.ReadFile(filepath.Join(rootAppData, "main-dcrd", "rpc.cert"))
	if err != nil {
		return nil, err
	}
	connCfg := &rpcclient.ConnConfig{
		Host:         "127.0.0.1:19556",
		Endpoint:     "ws",
		User:         "USER",
		Pass:         "PASSWORD",
		Certificates: cert,
	}
	client, err := rpcclient.New(connCfg, nil)
	if err != nil {
		return nil, err
	}

	// Generate enough blocks to get to Stake Enable Height.
	if _, err := rpctest.AdjustedSimnetMiner(ctx, client, 33); err != nil {
		return nil, err
	}

	dcrd := &dcrdProc{c: client}
	return dcrd, nil
}
