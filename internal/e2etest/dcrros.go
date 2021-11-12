// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"decred.org/dcrros/types"
	"github.com/coinbase/rosetta-sdk-go/client"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/dcrutil/v3"
)

type dcrrosProc struct {
	network *rtypes.NetworkIdentifier
	c       *client.APIClient
}

// isUnsynced returna nil caso essa instancia do dcrros ainda esteja no bloco
// genesis.
func (dcrros *dcrrosProc) isUnsynced(ctx context.Context) error {
	req := &rtypes.NetworkRequest{
		NetworkIdentifier: dcrros.network,
	}
	status, rosettaErr, err := dcrros.c.NetworkAPI.NetworkStatus(ctx, req)
	if err != nil {
		return err
	}
	if rosettaErr != nil {
		return types.RErrorAsError(rosettaErr)
	}

	curBlock := status.CurrentBlockIdentifier
	if curBlock.Hash == chainParams.GenesisHash.String() {
		return nil
	}

	return fmt.Errorf("dcrros not at genesis")
}

func (dcrros *dcrrosProc) waitSynced(ctx context.Context, miner *dcrdProc) error {
	done := func() bool {
		select {
		case <-ctx.Done():
			return true
		default:
			return false
		}
	}

	for !done() {
		req := &rtypes.NetworkRequest{
			NetworkIdentifier: dcrros.network,
		}
		status, rosettaErr, err := dcrros.c.NetworkAPI.NetworkStatus(ctx, req)
		if err != nil {
			return err
		}
		if rosettaErr != nil {
			return types.RErrorAsError(rosettaErr)
		}

		bestBlockHash, bestHeight, err := miner.c.GetBestBlock(ctx)
		if err != nil {
			return err
		}

		curBlock := status.CurrentBlockIdentifier
		if curBlock.Hash == bestBlockHash.String() {
			return nil
		}
		log.Debugf("Waiting for dcrros sync (target height %d "+
			"target hash %s, current height %d current hash %s)",
			bestHeight, bestBlockHash, curBlock.Index, curBlock.Hash)
	}

	return fmt.Errorf("dcrros not synced ")
}

func (dcrros *dcrrosProc) addrBalance(ctx context.Context, addr string) (dcrutil.Amount, error) {
	req := &rtypes.AccountBalanceRequest{
		NetworkIdentifier: dcrros.network,
		AccountIdentifier: &rtypes.AccountIdentifier{
			Address: addr,
			Metadata: map[string]interface{}{
				"script_version": uint8(0),
			},
		},
	}
	res, rosettaErr, err := dcrros.c.AccountAPI.AccountBalance(ctx, req)
	if err != nil {
		return 0, err
	}
	if rosettaErr != nil {
		return 0, types.RErrorAsError(rosettaErr)
	}
	if len(res.Balances) != 1 {
		return 0, fmt.Errorf("no balances returned")
	}
	balance, err := strconv.ParseInt(res.Balances[0].Value, 10, 64)
	if err != nil {
		return 0, err
	}

	return dcrutil.Amount(balance), nil
}

func runDcrros(ctx context.Context, wg *sync.WaitGroup, rootAppData string, connectToMiner bool) (*dcrrosProc, error) {
	listenPort := 20100
	dcrdP2pPort := 20101
	dcrdRpcPort := 20102
	name := "dcrros-online"
	if !connectToMiner {
		listenPort += 3
		dcrdP2pPort += 3
		dcrdRpcPort += 3
		name = "dcrros-offline"
	}

	genRosettaCLICfg(rootAppData)
	rootDcrros := filepath.Join(rootAppData, name)
	rootDcrd := filepath.Join(rootDcrros, "dcrd")
	args := []string{
		"--simnet",
		"--dbtype=" + *dbType,
		"--appdata=" + rootDcrros,
		fmt.Sprintf("--listen=127.0.0.1:%d", listenPort),
		"--dcrdcertpath=" + filepath.Join(rootDcrd, "rpc.cert"),
		"--rundcrd=dcrd",
		"--dcrduser=USER",
		"--dcrdpass=PASSWORD",
		fmt.Sprintf("--dcrdconnect=127.0.0.1:%d", dcrdRpcPort),
		"--dcrdextraarg=\"--appdata=" + rootDcrd + "\"",
		fmt.Sprintf("--dcrdextraarg=\"--listen=127.0.0.1:%d\"", dcrdP2pPort),
	}

	if connectToMiner {
		args = append(args, "--dcrdextraarg=\"--connect=127.0.0.1:19555\"")
	} else {
		args = append(args, "--offline")
	}

	dcrrosOutputFile, err := os.Create(filepath.Join(rootAppData, name+".log"))
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "dcrros", args...)
	cmd.Stdout = dcrrosOutputFile
	cmd.Stderr = dcrrosOutputFile
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	wg.Add(1)
	go func() {
		<-ctx.Done()
		cmd.Process.Signal(os.Interrupt)
		cmd.Wait()
		wg.Done()
	}()

	time.Sleep(100 * time.Millisecond)

	// Connect to the dcrros instance.
	clientCfg := client.NewConfiguration(
		fmt.Sprintf("http://127.0.0.1:%d", listenPort),
		"e2etesting",
		&http.Client{
			Timeout: 10 * time.Second,
		},
	)

	var (
		c *client.APIClient
	)

	network := &rtypes.NetworkIdentifier{
		Blockchain: "decred",
		Network:    chainParams.Name,
	}

	// Try to connect for up to 10 seconds.
	for i := 0; i < 10; i++ {
		time.Sleep(time.Second)

		c = client.NewAPIClient(clientCfg)

		// Assert it's in the correct network.
		var networkList *rtypes.NetworkListResponse
		var rosettaErr *rtypes.Error
		networkList, rosettaErr, err = c.NetworkAPI.NetworkList(
			ctx,
			&rtypes.MetadataRequest{},
		)
		if rosettaErr != nil && err != nil {
			err = types.RErrorAsError(rosettaErr)
		}
		if err != nil {
			continue
		}

		if len(networkList.NetworkIdentifiers) == 0 {
			return nil, fmt.Errorf("no networks specified")
		}

		gotNet := networkList.NetworkIdentifiers[0].Network
		if gotNet != chainParams.Name {
			return nil, fmt.Errorf("dcrros not on %s network (got %s)",
				chainParams.Name, gotNet)
		}
	}
	if err != nil {
		return nil, err
	}

	return &dcrrosProc{c: c, network: network}, nil
}
