// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"decred.org/dcrros/backend/backenddb"
	"decred.org/dcrros/types"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/rpcclient/v8"
	"github.com/decred/dcrd/rpctest"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
	"github.com/stretchr/testify/require"
)

// rpctestHarness generates an rpctest harness used for tests.
func rpctestHarness(t *testing.T, net *chaincfg.Params, name string) *rpctest.Harness {
	var handlers *rpcclient.NotificationHandlers

	// Setup the log dir for tests to ease debugging after failures.
	testDir := strings.ReplaceAll(t.Name(), "/", "_")
	logDir := filepath.Join(".dcrdlogs", testDir, name)
	extraArgs := []string{
		"--debuglevel=debug",
		"--rejectnonstd",
		"--logdir=" + logDir,
	}
	info, err := os.Stat(logDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("error stating log dir: %v", err)
	}
	if info != nil {
		if !info.IsDir() {
			t.Fatalf("logdir (%s) is not a dir", logDir)
		}
		err = os.RemoveAll(logDir)
		if err != nil {
			t.Fatalf("error removing logdir: %v", err)
		}
	}

	// Create the rpctest harness and mine outputs for the voting wallet to
	// use.
	hn, err := rpctest.New(t, net, handlers, extraArgs)
	if err != nil {
		t.Fatal(err)
	}
	err = hn.SetUp(context.Background(), false, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hn.TearDown() })
	return hn
}

// rpctestHarnessAndVW generates a new rpctest harness node and voting wallet
// for rpctest-based tests.
//
// name is used to disambiguate when multiple harnesses are used in the same
// test.
func rpctestHarnessAndVW(t *testing.T, net *chaincfg.Params, name string) (*rpctest.Harness, *rpctest.VotingWallet) {
	hn := rpctestHarness(t, net, name)

	// Generate funds for the voting wallet.
	_, err := rpctest.AdjustedSimnetMiner(testCtx(t), hn.Node, 64)
	if err != nil {
		t.Fatal(err)
	}

	// Create the voting wallet.
	vwCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	vw, err := rpctest.NewVotingWallet(vwCtx, hn)
	if err != nil {
		t.Fatalf("unable to create voting wallet for test: %v", err)
	}
	err = vw.Start(vwCtx)
	if err != nil {
		t.Fatalf("unable to setup voting wallet: %v", err)
	}
	vw.SetErrorReporting(func(vwerr error) {
		t.Fatalf("voting wallet errored: %v", vwerr)
	})
	vw.SetMiner(func(ctx context.Context, nb uint32) ([]*chainhash.Hash, error) {
		return rpctest.AdjustedSimnetMiner(ctx, hn.Node, nb)
	})

	return hn, vw
}

// TestCheckDcrdSimnet ensures the exported CheckDcrd() function works when
// connecting to a running dcrd instance.
func TestCheckDcrdSimnet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping rpctest")
	}

	net := chaincfg.SimNetParams()
	hn, _ := rpctestHarnessAndVW(t, net, "main")

	rpcCfg := hn.RPCConfig()
	cfg := &ServerConfig{
		ChainParams: net,
		DcrdCfg:     &rpcCfg,
	}

	// Ensure it works when connecting to a valid node.
	err := CheckDcrd(testCtx(t), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testSimnetBalances(t *testing.T, db backenddb.DB) {
	var svr *Server
	net := chaincfg.SimNetParams()
	defaultFeeRate := dcrutil.Amount(1e4)

	// Create two harnesses and link them. The second one will be used for
	// a reorg test.
	hn, vw := rpctestHarnessAndVW(t, net, "main")
	hnOther, vwOther := rpctestHarnessAndVW(t, net, "other")
	err := rpctest.ConnectNode(testCtx(t), hn, hnOther)
	require.NoError(t, err)

	// Create a privkey and p2pkh addr we control for use in the tests.
	privKey := secp256k1.NewPrivateKey(new(secp256k1.ModNScalar).SetInt(1))
	pubKey := privKey.PubKey().SerializeCompressed()
	pubKeyHash := dcrutil.Hash160(pubKey)
	p2pkhAddr, err := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(pubKeyHash, net)
	if err != nil {
		t.Fatal(err)
	}
	scriptVersion, p2pkhScript := p2pkhAddr.PaymentScript()

	// Helper to assert the balance of the the given address is correct at
	// tip.
	assertTipBalance := func(wantBalance int64) {
		t.Helper()

		accountId := &rtypes.AccountIdentifier{
			Address: p2pkhAddr.String(),
			Metadata: map[string]interface{}{
				"script_version": scriptVersion,
			},
		}
		reqBalance := &rtypes.AccountBalanceRequest{
			AccountIdentifier: accountId,
		}

		// Request the balance continuously until the reply identifies
		// the dcrros node has processed up to the chain tip.
		deadline := time.Now().Add(time.Second * 3)
		for !time.Now().After(deadline) {
			resBalance, rerr := svr.AccountBalance(testCtx(t), reqBalance)
			require.Nil(t, rerr)
			gotBalance, err := strconv.ParseInt(resBalance.Balances[0].Value, 10, 64)
			require.NoError(t, err)

			bestHash, bestHeight, err := hn.Node.GetBestBlock(testCtx(t))
			require.NoError(t, err)

			bi := resBalance.BlockIdentifier
			if bi.Index != bestHeight {
				// Not at tip yet. Wait for a bit.
				time.Sleep(time.Millisecond * 10)
				continue
			}
			if bi.Hash != bestHash.String() {
				t.Fatalf("unexpected tip hash. want=%s got=%s",
					bestHash, bi.Hash)
			}
			if gotBalance != wantBalance {
				t.Fatalf("unexpected balance. want=%d got=%d",
					wantBalance, gotBalance)
			}
			return
		}
		t.Fatalf("timeout waiting for chain tip")
	}

	type utxo struct {
		outp wire.OutPoint
		amt  int64
	}
	assertCoinsMatch := func(wantUtxos map[string]*utxo) {
		t.Helper()

		req := &rtypes.AccountCoinsRequest{
			AccountIdentifier: &rtypes.AccountIdentifier{
				Address: p2pkhAddr.String(),
				Metadata: map[string]interface{}{
					"script_version": uint16(0),
				},
			},
		}

		res, rerr := svr.AccountCoins(testCtx(t), req)
		require.Nil(t, rerr)

		if len(res.Coins) != len(wantUtxos) {
			t.Fatalf("unexpected nb of coins. want=%d got=%d",
				len(wantUtxos), len(res.Coins))
		}
		wantUtxosKeys := make(map[string]struct{}, len(wantUtxos))
		for k := range wantUtxos {
			wantUtxosKeys[k] = struct{}{}
		}
		for _, gotCoin := range res.Coins {
			gotOutp := gotCoin.CoinIdentifier.Identifier
			wantUtxo, ok := wantUtxos[gotOutp]
			if !ok {
				t.Fatalf("could not find returned coin id %s",
					gotCoin.CoinIdentifier.Identifier)
			}
			gotAmt, err := strconv.ParseInt(gotCoin.Amount.Value, 10, 64)
			require.NoError(t, err)

			if gotAmt != wantUtxo.amt {
				t.Fatalf("unexpected utxo amount. want=%d got=%d",
					wantUtxo.amt, gotAmt)
			}
			delete(wantUtxosKeys, gotOutp)
		}
		if len(wantUtxosKeys) > 0 {
			t.Fatalf("some utxos were not returned by the server: %v", wantUtxosKeys)
		}

	}

	wantUtxos := make(map[string]*utxo)
	addWantUtxo := func(txh *chainhash.Hash, index uint32, value int64) {
		outp := wire.OutPoint{Hash: *txh, Index: index}
		wantUtxos[outp.String()] = &utxo{outp: outp, amt: value}
	}
	delWantUtxo := func(outp wire.OutPoint) {
		delete(wantUtxos, outp.String())
	}

	// Send to the given address and mine a few blocks to ensure the server
	// can process an already started blockchain.
	coin := int64(1e8)
	txOut := &wire.TxOut{PkScript: p2pkhScript, Value: coin}
	txh, err := hn.SendOutputs(testCtx(t), []*wire.TxOut{txOut}, defaultFeeRate)
	addWantUtxo(txh, 0, coin)
	require.NoError(t, err)
	_, err = vw.GenerateBlocks(testCtx(t), 5)
	require.NoError(t, err)
	wantBalance := coin

	// Initialize server.
	rpcCfg := hn.RPCConfig()
	cfg := &ServerConfig{
		ChainParams: net,
		DBType:      dbTypePreconfigured,
		DcrdCfg:     &rpcCfg,
		db:          db,
		peerTimeout: -1,
	}
	svr, err = NewServer(cfg)
	require.NoError(t, err)

	// Run the Server on a separate goroutine.
	runCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		if cancel != nil {
			cancel()
		}
	})
	runErr := make(chan error)
	go func() {
		runErr <- svr.Run(runCtx)
	}()

	// Run shouldn't() error.
	select {
	case err := <-runErr:
		t.Fatalf("unexpected Run() error: %v", err)
	case <-time.After(3 * time.Second):
	}

	// Query for the most recent block. It should match the chain tip.
	resStatus, rerr := svr.NetworkStatus(testCtx(t), nil)
	require.Nil(t, rerr)
	bestHash, bestHeight, err := hn.Node.GetBestBlock(testCtx(t))
	require.NoError(t, err)

	cbi := resStatus.CurrentBlockIdentifier
	if cbi.Index != bestHeight {
		t.Fatalf("unexpected tip height. want=%d got=%d",
			bestHeight, cbi.Index)
	}
	if cbi.Hash != bestHash.String() {
		t.Fatalf("unexpected tip hash. want=%s got=%s",
			bestHash, cbi.Hash)
	}

	// The current account balance and coins should match the expected.
	assertTipBalance(wantBalance)
	assertCoinsMatch(wantUtxos)

	// Send more coins to the address and ensure the tx is found in the
	// mempool.
	sentTxHash, err := hn.SendOutputs(testCtx(t), []*wire.TxOut{txOut}, defaultFeeRate)
	require.NoError(t, err)
	resMempool, rerr := svr.Mempool(testCtx(t), &rtypes.NetworkRequest{})
	require.Nil(t, rerr)
	foundTx := false
	for _, txid := range resMempool.TransactionIdentifiers {
		foundTx = foundTx || txid.Hash == sentTxHash.String()
	}
	if !foundTx {
		t.Fatalf("could not find tx %s in mempool", sentTxHash)
	}

	// Balance is still the old amount (mempool does not affect the account
	// balances).
	assertTipBalance(wantBalance)

	// Mine the tx and assert the account amount increased and the new coin
	// is tracked.
	_, err = vw.GenerateBlocks(testCtx(t), 1)
	require.NoError(t, err)
	wantBalance += coin
	assertTipBalance(wantBalance)
	addWantUtxo(sentTxHash, 0, coin)
	assertCoinsMatch(wantUtxos)

	// Spend from this account, mine the tx and verify the balance changed.
	//
	// Prepare and sign the tx.
	spendAmount := coin / 10
	spendTx := wire.NewMsgTx()
	spendTx.AddTxIn(wire.NewTxIn(&wire.OutPoint{Hash: *sentTxHash}, coin, nil))
	spendTx.AddTxOut(wire.NewTxOut(coin-spendAmount, p2pkhScript))
	spendTxSigHash, err := txscript.CalcSignatureHash(p2pkhScript,
		txscript.SigHashAll, spendTx, 0, nil)
	require.NoError(t, err)
	spendTxSig := ecdsa.Sign(privKey, spendTxSigHash).Serialize()
	spendTxSig = append(spendTxSig, byte(txscript.SigHashAll))
	var b txscript.ScriptBuilder
	b.AddData(spendTxSig)
	b.AddData(pubKey)
	spendTx.TxIn[0].SignatureScript, err = b.Script()
	require.NoError(t, err)

	// Disconnect hn and hnOther so we'll test a reorg that will drop
	// spendTx.
	err = rpctest.RemoveNode(testCtx(t), hn, hnOther)
	require.NoError(t, err)

	// Publish and mine the tx.
	spendTxHash, err := hn.Node.SendRawTransaction(testCtx(t), spendTx, true)
	require.NoError(t, err)
	_, err = vw.GenerateBlocks(testCtx(t), 1)
	require.NoError(t, err)
	delWantUtxo(wire.OutPoint{Hash: *sentTxHash})
	addWantUtxo(spendTxHash, 0, coin-spendAmount)

	// Ensure the balance is correct and the utxo set for the account
	// changed correctly.
	wantBalance -= spendAmount
	assertTipBalance(wantBalance)
	assertCoinsMatch(wantUtxos)

	// Generate a reorg and reconnect the nodes to drop spendTx from
	// account balance calcs.
	vwOther.GenerateBlocks(testCtx(t), 3)
	err = rpctest.ConnectNode(testCtx(t), hn, hnOther)
	require.NoError(t, err)
	err = rpctest.JoinNodes(testCtx(t), []*rpctest.Harness{hn, hnOther}, rpctest.Blocks)
	require.NoError(t, err)

	// Ensure the balance reflects the fact that the coins have not been
	// spent anymore.
	wantBalance += spendAmount
	assertTipBalance(wantBalance)

	// Ensure the utxo set for this account was properly updated by
	// removing the second tx and restoring the first one.
	delWantUtxo(wire.OutPoint{Hash: *spendTxHash})
	addWantUtxo(sentTxHash, 0, coin)
	assertCoinsMatch(wantUtxos)
}

// TestSimnetBalances runs an rpctest-based simnet, generates a blockchain that
// exercises a significant amount of dcrros-supported operations and ensures
// the account balances are correct.
func TestSimnetBalances(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping rpctest")
	}

	testDbInstances(t, true, testSimnetBalances)
}

func testSimnetConstructionInteraction(t *testing.T, db backenddb.DB) {
	params := chaincfg.SimNetParams()
	defaultFeeRate := dcrutil.Amount(1e4)
	var svrOnline, svrOffline *Server

	// chainOnline simulates a chain that is connected to the network.
	// chainOffline is a chain that never leaves genesis.
	hnOnline, vw := rpctestHarnessAndVW(t, params, "online")
	hnOffline := rpctestHarness(t, params, "offline")

	// Shorter function names to improve readability.
	coinAmt := int64(1e8)
	spendAmt := coinAmt / 4
	privKeyBytes := mustHex("c387ce35ba8e5d6a566f76c2cf5b055b3a01fe9fd5e5856e93aca8f6d3405696")
	privKeySecp256k1 := secp256k1.PrivKeyFromBytes(privKeyBytes)
	pubKeySecp256k1 := privKeySecp256k1.PubKey()
	pubKeySecp256k1Hash := dcrutil.Hash160(pubKeySecp256k1.SerializeCompressed())
	addrEcdsaSecp256k1, err := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(pubKeySecp256k1Hash,
		params)
	require.NoError(t, err)
	scriptVersion, pksEcdsaSecp256k1 := addrEcdsaSecp256k1.PaymentScript()
	addrSpend, err := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0([]byte{19: 0x00},
		params)
	require.NoError(t, err)

	// Helper to assert the balance of the the given address is correct at
	// tip in the online server.
	assertTipBalance := func(addr string, wantBalance int64) {
		t.Helper()

		accountId := &rtypes.AccountIdentifier{
			Address: addr,
			Metadata: map[string]interface{}{
				"script_version": scriptVersion,
			},
		}
		reqBalance := &rtypes.AccountBalanceRequest{
			AccountIdentifier: accountId,
		}

		// Request the balance continuously until the reply identifies
		// the dcrros node has processed up to the chain tip.
		deadline := time.Now().Add(time.Second * 3)
		for !time.Now().After(deadline) {
			resBalance, rerr := svrOnline.AccountBalance(testCtx(t), reqBalance)
			require.Nil(t, rerr)
			gotBalance, err := strconv.ParseInt(resBalance.Balances[0].Value, 10, 64)
			require.NoError(t, err)

			bestHash, bestHeight, err := hnOnline.Node.GetBestBlock(testCtx(t))
			require.NoError(t, err)

			bi := resBalance.BlockIdentifier
			if bi.Index != bestHeight {
				// Not at tip yet. Wait for a bit.
				time.Sleep(time.Millisecond * 10)
				continue
			}
			if bi.Hash != bestHash.String() {
				t.Fatalf("unexpected tip hash. want=%s got=%s",
					bestHash, bi.Hash)
			}
			if gotBalance != wantBalance {
				t.Fatalf("unexpected balance. want=%d got=%d",
					wantBalance, gotBalance)
			}
			return
		}
		t.Fatalf("timeout waiting for chain tip")
	}

	// Generate outputs that can be spent in the online chain.
	txOutEcdsaSecp256k1 := &wire.TxOut{PkScript: pksEcdsaSecp256k1, Value: coinAmt}
	txhEcdsaSecp256k1, err := hnOnline.SendOutputs(testCtx(t), []*wire.TxOut{txOutEcdsaSecp256k1}, defaultFeeRate)
	require.NoError(t, err)
	outpointEcdsaSecp256k1 := wire.OutPoint{
		Hash:  *txhEcdsaSecp256k1,
		Index: 0,
	}
	scriptVersion0 := uint16(0)

	// Mine in the online chain and assert the offline chain hasn't moved
	// to ensure the premise for the test holds.
	_, err = vw.GenerateBlocks(testCtx(t), 3)
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
	bestHash, bestHeight, err := hnOffline.Node.GetBestBlock(testCtx(t))
	require.NoError(t, err)
	require.Equal(t, int64(0), bestHeight)
	require.Equal(t, params.GenesisHash, *bestHash)

	// Initialize the servers. One is "online" (i.e. has blocks, connected
	// to other peers, etc) the other is offline (no blocks, peers, etc).
	rpcOnlineCfg := hnOnline.RPCConfig()
	svrOnlineCfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		DcrdCfg:     &rpcOnlineCfg,
		db:          db,
		peerTimeout: -1,
	}
	rpcOfflineCfg := hnOffline.RPCConfig()
	svrOfflineCfg := &ServerConfig{
		ChainParams: params,
		DBType:      DBTypeMem,
		DcrdCfg:     &rpcOfflineCfg,
		peerTimeout: -1,
	}
	svrOnline, err = NewServer(svrOnlineCfg)
	require.NoError(t, err)
	svrOffline, err = NewServer(svrOfflineCfg)
	require.NoError(t, err)

	// Run both servers on separate goroutines.
	runCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		if cancel != nil {
			cancel()
		}
	})
	runErrOnline := make(chan error)
	runErrOffline := make(chan error)
	go func() {
		runErrOnline <- svrOnline.Run(runCtx)
	}()
	go func() {
		runErrOffline <- svrOffline.Run(runCtx)
	}()

	// Run shouldn't() error.
	select {
	case err := <-runErrOnline:
		t.Fatalf("unexpected Run() error in online server: %v", err)
	case err := <-runErrOffline:
		t.Fatalf("unexpected Run() error in offline server: %v", err)
	case <-time.After(3 * time.Second):
	}

	// Ensure the online node is caught up with the chain.
	resStatus, rerr := svrOnline.NetworkStatus(testCtx(t), nil)
	require.Nil(t, rerr)
	bestHash, bestHeight, err = hnOnline.Node.GetBestBlock(testCtx(t))
	require.NoError(t, err)
	cbi := resStatus.CurrentBlockIdentifier
	if cbi.Index != bestHeight {
		t.Fatalf("unexpected tip height. want=%d got=%d",
			bestHeight, cbi.Index)
	}
	if cbi.Hash != bestHash.String() {
		t.Fatalf("unexpected tip hash. want=%s got=%s",
			bestHash, cbi.Hash)
	}

	// Now begin testing the construction API.

	// Step 1: Derive the account address for the pubkeys that will spend
	// coins.
	reqDeriveEcdsaSecp256k1 := &rtypes.ConstructionDeriveRequest{
		PublicKey: &rtypes.PublicKey{
			Bytes:     pubKeySecp256k1.SerializeCompressed(),
			CurveType: rtypes.Secp256k1,
		},
		Metadata: map[string]interface{}{
			"script_version": scriptVersion0,
			"algo":           "ecdsa",
		},
	}
	resDeriveEcdsaSecp256k1, rerr := svrOffline.ConstructionDerive(testCtx(t), reqDeriveEcdsaSecp256k1)
	require.Nil(t, rerr)

	// (Client Step): Create the list of ops that will end up in the tx.
	ops := []*rtypes.Operation{{
		Type:     "debit",
		Amount:   types.DcrAmountToRosetta(dcrutil.Amount(coinAmt)),
		Metadata: map[string]interface{}{},
		Account:  resDeriveEcdsaSecp256k1.AccountIdentifier,
		CoinChange: &rtypes.CoinChange{
			CoinIdentifier: &rtypes.CoinIdentifier{
				Identifier: outpointEcdsaSecp256k1.String(),
			},
			CoinAction: rtypes.CoinSpent,
		},
	}, {
		Type:    "credit",
		Amount:  types.DcrAmountToRosetta(dcrutil.Amount(coinAmt - spendAmt)),
		Account: resDeriveEcdsaSecp256k1.AccountIdentifier,
	}, {
		Type:   "credit",
		Amount: types.DcrAmountToRosetta(dcrutil.Amount(spendAmt)),
		Account: &rtypes.AccountIdentifier{
			Address: addrSpend.String(),
			Metadata: map[string]interface{}{
				"script_version": uint16(0),
			},
		},
	}}
	txMeta := map[string]interface{}{}

	// Step 2: Preprocess the operations in offline dcrros node to fetch
	// required options.
	reqPreprocess := &rtypes.ConstructionPreprocessRequest{
		Operations: ops,
		Metadata:   txMeta,
	}
	resPreprocess, rerr := svrOffline.ConstructionPreprocess(testCtx(t), reqPreprocess)
	require.Nil(t, rerr)

	// Step 3: Request metadata from online dcrros node.
	reqMetadata := &rtypes.ConstructionMetadataRequest{
		Options: resPreprocess.Options,
	}
	resMetadata, rerr := svrOnline.ConstructionMetadata(testCtx(t), reqMetadata)
	require.Nil(t, rerr)

	// (Client Step): Subtract the fee from the first output (simulates the
	// first output being a change output).
	fee, err := strconv.ParseInt(resMetadata.SuggestedFee[0].Value, 10, 64)
	require.NoError(t, err)
	ops[1].Amount.Value = strconv.FormatInt(coinAmt-spendAmt-fee, 10)

	// Step 4: Generate the payloads that need to be signed.
	reqPayloads := &rtypes.ConstructionPayloadsRequest{
		Metadata:   txMeta,
		Operations: ops,
	}
	resPayloads, rerr := svrOffline.ConstructionPayloads(testCtx(t), reqPayloads)
	require.Nil(t, rerr)

	// Step 5: Parse unsigned tx to confirm correctness.
	reqParse := &rtypes.ConstructionParseRequest{
		Transaction: resPayloads.UnsignedTransaction,
	}
	resParse, rerr := svrOffline.ConstructionParse(testCtx(t), reqParse)
	require.Nil(t, rerr)
	require.Len(t, resParse.Operations, 3)

	// (Client Step): Sign the payloads.
	//
	// For ECDSA over secp256k1, generate the compact signature and remove
	// the recovery code (first byte of the sig).
	payloadEcdsaSecp256k1 := resPayloads.Payloads[0].Bytes
	sigEcdsaSecp256k1 := ecdsa.SignCompact(privKeySecp256k1,
		payloadEcdsaSecp256k1, true)[1:]

	// Step 5: Combine signatures to generate final signed tx.
	reqCombine := &rtypes.ConstructionCombineRequest{
		UnsignedTransaction: resPayloads.UnsignedTransaction,
		Signatures: []*rtypes.Signature{{
			SigningPayload: resPayloads.Payloads[0],
			PublicKey:      reqDeriveEcdsaSecp256k1.PublicKey,
			SignatureType:  rtypes.Ecdsa,
			Bytes:          sigEcdsaSecp256k1,
		}},
	}
	resCombine, rerr := svrOffline.ConstructionCombine(testCtx(t), reqCombine)
	require.Nil(t, rerr)

	// Step 6: Parse signed tx to confirm correctness.
	reqParseSigned := &rtypes.ConstructionParseRequest{
		Transaction: resCombine.SignedTransaction,
		Signed:      true,
	}
	resParseSigned, rerr := svrOffline.ConstructionParse(testCtx(t), reqParseSigned)
	require.Nil(t, rerr)
	require.Len(t, resParseSigned.Operations, 3)
	ctrtx := new(constructionTx)
	if err := ctrtx.deserialize(resCombine.SignedTransaction); err != nil {
		t.Fatalf("unable to deserialize signed tx: %v", err)
	}
	signedTx := ctrtx.tx
	signedTxHash := signedTx.TxHash()

	// Step 7: Get hash of signed transaction.
	reqHash := &rtypes.ConstructionHashRequest{
		SignedTransaction: resCombine.SignedTransaction,
	}
	resHash, rerr := svrOffline.ConstructionHash(testCtx(t), reqHash)
	require.Nil(t, rerr)
	require.Equal(t, signedTxHash.String(), resHash.TransactionIdentifier.Hash)

	// Step 8: Submit the tx to the network.
	reqSubmit := &rtypes.ConstructionSubmitRequest{
		SignedTransaction: resCombine.SignedTransaction,
	}
	resSubmit, rerr := svrOnline.ConstructionSubmit(testCtx(t), reqSubmit)
	require.Nil(t, rerr)
	require.Equal(t, resHash.TransactionIdentifier.Hash, resSubmit.TransactionIdentifier.Hash)

	// Mine it.
	blockHashes, err := vw.GenerateBlocks(testCtx(t), 1)
	require.NoError(t, err)

	// Ensure the tx was actually mined.
	block, err := hnOnline.Node.GetBlock(testCtx(t), blockHashes[0])
	require.NoError(t, err)
	found := false
	for _, tx := range block.Transactions {
		if tx.TxHash() == signedTxHash {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Could not find signed tx %s in block transactions",
			signedTxHash)
	}

	// Assert the account balance is correct.
	assertTipBalance(addrEcdsaSecp256k1.String(), coinAmt-spendAmt-fee)
}

// TestSimnetConstructionInteraction tests a full interaction of the
// Construction APIs in order to build and submit a transaction using the
// rpctest dcrd facility.
func TestSimnetConstructionInteraction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping rpctest")
	}

	testDbInstances(t, true, testSimnetConstructionInteraction)
}

func TestMain(m *testing.M) {
	rpctest.SetPathToDCRD("dcrd")
	os.Exit(m.Run())
}
