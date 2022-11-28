// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	dcrwrpcclient "decred.org/dcrwallet/v2/rpc/client/dcrwallet"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/jrick/wsrpc/v2"
)

type dcrwProc struct {
	c *dcrwrpcclient.Client
}

func (dcrw *dcrwProc) sendToAddr(ctx context.Context, addr stdaddr.Address, amount dcrutil.Amount) error {
	_, err := dcrw.c.SendToAddress(ctx, addr, amount)
	return err
}

func (dcrw *dcrwProc) importPrivKey(ctx context.Context, wif string) error {
	w, err := dcrutil.DecodeWIF(wif, chainParams.PrivateKeyID)
	if err != nil {
		return err
	}
	return dcrw.c.ImportPrivKey(ctx, w)
}

func (dcrw *dcrwProc) nbUtxos(ctx context.Context) (int, error) {
	utxos, err := dcrw.c.ListUnspentMin(ctx, 1)
	if err != nil {
		return 0, err
	}
	var spendable int
	for _, utxo := range utxos {
		if !utxo.Spendable {
			continue
		}
		if utxo.TxType == 1 {
			continue
		}
		spendable++
	}

	return spendable, nil
}

func (dcrw *dcrwProc) logSpendableUtxos(ctx context.Context) {
	nb, err := dcrw.nbUtxos(ctx)
	if err != nil {
		return
	}
	log.Debugf("Wallet has %d spendable utxos", nb)

}

func (dcrw *dcrwProc) waitSynced(ctx context.Context, miner *dcrdProc) error {
	_, tipHeight, err := miner.c.GetBestBlock(ctx)
	if err != nil {
		return err
	}

	for i := 0; i < 20; i++ {
		info, err := dcrw.c.GetInfo(ctx)
		if err != nil {
			return err
		}
		if int64(info.Blocks) >= tipHeight {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("wallet still unsynced after timeout")
}

func (dcrw *dcrwProc) selfTransferMany(ctx context.Context, nb int) error {
	addrs := make(map[stdaddr.Address]dcrutil.Amount, 5)
	for j := 0; j < nb; j++ {
		addr, err := dcrw.c.GetNewAddress(ctx, "default")
		if err != nil {
			return err
		}
		addrs[addr] = 2e8
	}

	_, err := dcrw.c.SendMany(ctx, "default", addrs)
	if err != nil {
		return err
	}

	return nil
}

func (dcrw *dcrwProc) genManyUtxos(ctx context.Context, miner *dcrdProc) error {
	// Generate some initial utxos.
	if err := dcrw.waitSynced(ctx, miner); err != nil {
		return err
	}
	dcrw.logSpendableUtxos(ctx)
	if _, err := miner.mine(ctx, 10); err != nil {
		return err
	}
	if err := dcrw.waitSynced(ctx, miner); err != nil {
		return err
	}
	dcrw.logSpendableUtxos(ctx)

	// From those, self-transfer to many different addresses.
	for i := 0; i < 15; i++ {
		if err := dcrw.selfTransferMany(ctx, i+1); err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)

		if _, err := miner.mine(ctx, 1); err != nil {
			return err
		}
	}

	dcrw.logSpendableUtxos(ctx)

	return nil
}

func runMainWallet(ctx context.Context, wg *sync.WaitGroup, rootAppData string) (*dcrwProc, error) {
	args := []string{
		"--simnet",
		"--appdata=" + filepath.Join(rootAppData, "main-dcrw"),
		"--username=USER",
		"--password=PASSWORD",
		"--nogrpc",
		"--rpclisten=127.0.0.1:19557",
		"--pass=123",
		"--enablevoting",
		//	"--enableticketbuyer",
		//"--ticketbuyer.limit=5",
		"--cafile=" + filepath.Join(rootAppData, "main-dcrd", "rpc.cert"),
	}

	dcrwOutputFile, err := os.Create(filepath.Join(rootAppData, "main-dcrw.log"))
	if err != nil {
		return nil, err
	}

	createWalletPipeRx, createWalletPipeTx, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("unable to create dcrw pipes: %v", err)
	}

	// Create the wallet.
	args = append(args, "--create")

	cmd := exec.CommandContext(ctx, "dcrwallet", args...)
	cmd.Stdout = dcrwOutputFile
	cmd.Stderr = dcrwOutputFile
	cmd.Stdin = createWalletPipeRx
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	walletWrite := func(s string) {
		b := []byte(s)
		var n int
		var err error
		for n, err = createWalletPipeTx.Write(b); n < len(b) && err != nil; {
			b = b[n:]
		}
		if err != nil {
			panic(err)
		}
	}
	walletWrite("y\n")                              // Use pass from config.
	walletWrite("n\n")                              // No additional encryption.
	walletWrite("y\n")                              // Have seed.
	walletWrite("00000000000000000000000000000000") // Seed.
	walletWrite("00000000000000000000000000000000\n\n")

	// Wait for the wallet to end.
	createWalletWait := make(chan error)
	go func() {
		createWalletWait <- cmd.Wait()
	}()
	select {
	case err := <-createWalletWait:
		if err != nil {
			return nil, err
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Drop --create from the args.
	args = args[:len(args)-1]

	// Start the wallet again.
	cmd = exec.Command("dcrwallet", args...)
	cmd.Stdout = dcrwOutputFile
	cmd.Stderr = dcrwOutputFile
	cmd.Stdin = createWalletPipeRx
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Command the wallet to stop once the context is done.
	go func() {
		<-ctx.Done()
		cmd.Process.Signal(os.Interrupt)
	}()

	// Monitor for the wallet to finish.
	wg.Add(1)
	go func() {
		err := cmd.Wait()
		switch {
		case err != nil:
			log.Warnf("Main wallet process finished with error: %v", err)
		default:
			log.Debugf("Main wallet process finished")
		}
		wg.Done()
	}()

	// Wait for it to startup and get synced.
	time.Sleep(time.Millisecond * 100)

	// Connect to the wallet.
	cert, err := ioutil.ReadFile(filepath.Join(rootAppData, "main-dcrw", "rpc.cert"))
	if err != nil {
		return nil, err
	}

	opts := make([]wsrpc.Option, 0, 5)
	opts = append(opts, wsrpc.WithBasicAuth("USER", "PASSWORD"))
	opts = append(opts, wsrpc.WithoutPongDeadline())
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(cert)
	tc := &tls.Config{
		MinVersion:       tls.VersionTLS12,
		CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256},
		CipherSuites: []uint16{ // Only applies to TLS 1.2. TLS 1.3 ciphersuites are not configurable.
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
		RootCAs: pool,
	}
	opts = append(opts, wsrpc.WithTLSConfig(tc))
	client, err := wsrpc.Dial(ctx, "wss://127.0.0.1:19557/ws", opts...)
	if err != nil {
		return nil, err
	}

	c := dcrwrpcclient.NewClient(client, chainParams)

	return &dcrwProc{c: c}, nil
}
