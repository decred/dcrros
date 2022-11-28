// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"decred.org/dcrros/types"
	walletjson "decred.org/dcrwallet/v2/rpc/jsonrpc/types"
	"github.com/decred/dcrd/blockchain/stake/v5"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"
)

func sendToTreasury(ctx context.Context, wallet *dcrwProc, amt dcrutil.Amount) (string, error) {
	var res string
	err := wallet.c.Call(ctx, "sendtotreasury", &res, float64(amt)/1e8)
	log.Debugf("TAdd generated: %s", res)
	return res, err
}

func sendFromTreasury(ctx context.Context, wallet *dcrwProc, addr string, amt dcrutil.Amount) (string, error) {
	var res string
	key := "02a36b785d584555696b69d1b2bbeff4010332b301e3edd316d79438554cacb3e7"
	amounts := map[string]float64{
		addr: float64(amt) / 1e8,
	}
	err := wallet.c.Call(ctx, "sendfromtreasury", &res, key, amounts)
	log.Debugf("TSpend generated: %s", res)
	return res, err
}

func sendToMultisig(ctx context.Context, wallet *dcrwProc) (string, error) {
	var res walletjson.SendToMultiSigResult
	amount := float64(1)
	pubkeys := []string{
		"Sse143dKKXu6LQm5TNfpjcAYBbuuLQ3DKCj",
		"SkQmYXQXBTJAvexVaQPixjW88St66zv5vN7DTUuuqpPJtC5fzMF37",
	}
	err := wallet.c.Call(ctx, "sendtomultisig", &res, "default", amount, pubkeys)
	log.Debugf("Multisig tx generated: %s (addr %s)", res.TxHash, res.Address)

	return res.TxHash, err
}

func redeemMultisig(ctx context.Context, wallet *dcrwProc, miner *dcrdProc, txh string) (string, error) {
	var res walletjson.RedeemMultiSigOutResult
	err := wallet.c.Call(ctx, "redeemmultisigout", &res, txh, 0, 0)
	if err != nil {
		return "", err
	}

	tx := &wire.MsgTx{}
	bts, err := hex.DecodeString(res.Hex)
	if err != nil {
		return "", err
	}
	if err := tx.FromBytes(bts); err != nil {
		return "", err
	}
	resHash, err := miner.c.SendRawTransaction(ctx, tx, true)
	if err != nil {
		return "", err
	}
	log.Debugf("Multisig redeem tx generated: %s", resHash)

	return resHash.String(), err
}

func setTreasuryPolicy(ctx context.Context, wallet *dcrwProc) error {
	key := "02a36b785d584555696b69d1b2bbeff4010332b301e3edd316d79438554cacb3e7"
	return wallet.c.Call(ctx, "settreasurypolicy", nil, key, "yes")
}

func isMined(ctx context.Context, miner *dcrdProc, txh string) (bool, error) {
	hash, err := chainhash.NewHashFromStr(txh)
	if err != nil {
		return false, err
	}
	tx, err := miner.c.GetRawTransactionVerbose(ctx, hash)
	if err != nil {
		return false, err
	}
	mined := tx.BlockHash != ""
	log.Debugf("Mined %s at height %d (hash %s)", txh, tx.BlockHeight,
		tx.BlockHash)
	return mined, nil
}

// ensureTicketRevoked mines blocks until the given ticket is spent and then
// verifies the spending tx is a revocation. This assumes the autorevoke agenda
// has passed.
func ensureTicketRevoked(ctx context.Context, miner *dcrdProc, wallet *dcrwProc,
	ticket *chainhash.Hash, mine bool) error {

	// First ensure the ticket hasn't been spent yet.
	res, err := miner.c.GetTxOut(ctx, ticket, 0, 0, false)
	if err != nil {
		return err
	}
	if res == nil {
		log.Debugf("Ticket %s already spent. Checking to see if it's "+
			"a revocation", ticket)

		// Ticket already spent, figure out if it was by a revocation.
		// Note: This is a crappy way of finding this out (iterating
		// between when the ticket was mined to current tip).

		// Find out the block the ticket was mined.
		tx, err := miner.c.GetRawTransactionVerbose(ctx, ticket)
		if err != nil {
			return err
		}

		_, tipHeight, err := miner.c.GetBestBlock(ctx)
		if err != nil {
			return err
		}

		// Search all blocks from then until the tip.
		for i := tx.BlockHeight; i <= tipHeight; i++ {
			bh, err := miner.c.GetBlockHash(ctx, i)
			if err != nil {
				return err
			}
			block, err := miner.c.GetBlock(ctx, bh)
			if err != nil {
				return err
			}
			for _, stx := range block.STransactions {
				if !stake.IsSSRtx(stx) {
					continue
				}

				if stx.TxIn[0].PreviousOutPoint.Hash == *ticket {
					revocationTx = stx
					return nil
				}
			}

		}
	}
	log.Debugf("Ticket %s unspent. Mining %d until revoked", ticket,
		chainParams.TicketExpiry)

	for i := 0; i < int(chainParams.TicketExpiry+1); i++ {
		// Mine a block.
		if err := mineAndSyncWallet(ctx, miner, wallet, 1); err != nil {
			return err
		}

		// See if ticket submission was spent.
		res, err := miner.c.GetTxOut(ctx, ticket, 0, 0, false)
		if err != nil {
			return err
		}
		if res != nil {
			// Not yet.
			continue
		}

		lastBlockHash, lastBlockHeight, err := miner.c.GetBestBlock(ctx)
		if err != nil {
			return err
		}
		log.Debugf("Found ticket spent at height %d (%s)", lastBlockHeight,
			lastBlockHash)

		// Mine an additional block (because the revocation might be in
		// the mempool).
		if err := mineAndSyncWallet(ctx, miner, wallet, 1); err != nil {
			return err
		}

		// Ticket spent! Find the revocation in the last block.
		lastBlockHash, _, err = miner.c.GetBestBlock(ctx)
		if err != nil {
			return err
		}
		lastBlock, err := miner.c.GetBlock(ctx, lastBlockHash)
		if err != nil {
			return err
		}
		for _, stx := range lastBlock.STransactions {
			if !stake.IsSSRtx(stx) {
				continue
			}

			if stx.TxIn[0].PreviousOutPoint.Hash == *ticket {
				return nil
			}
		}
		return fmt.Errorf("ticket %s was not revoked after being spent", ticket)
	}

	return fmt.Errorf("ticket %s was not revoked", ticket)
}

func addrAndAmountFromTx(tx *wire.MsgTx, out int) (string, dcrutil.Amount, error) {
	txout := tx.TxOut[out]

	typ, addrs := stdscript.ExtractAddrs(0, txout.PkScript, chainParams)
	if typ == stdscript.STNonStandard || len(addrs) != 1 {
		return "", 0, fmt.Errorf("no addresses in the given pkscript")
	}

	return addrs[0].String(), dcrutil.Amount(txout.Value), nil
}

// addrAndAmountFromTxh fetches the specified transaction output (txh and
// output index) and returns the address and amount.
func addrAndAmountFromTxh(ctx context.Context, miner *dcrdProc, txh string, out int) (string, dcrutil.Amount, error) {
	hash, err := chainhash.NewHashFromStr(txh)
	if err != nil {
		return "", 0, err
	}
	tx, err := miner.c.GetRawTransaction(ctx, hash)
	if err != nil {
		return "", 0, err
	}

	return addrAndAmountFromTx(tx.MsgTx(), out)
}

func mineAndSyncWallet(ctx context.Context, miner *dcrdProc, wallet *dcrwProc, nb uint32) error {
	_, height, err := miner.c.GetBestBlock(ctx)
	if err != nil {
		return err
	}

	for i := uint32(0); i < nb; i++ {
		now := time.Now()
		if _, err := miner.mine(ctx, 1); err != nil {
			return nil
		}
		height += 1

		if err := wallet.waitSynced(ctx, miner); err != nil {
			return err
		}

		var lenMempool int
		for i := 0; i < 20; i++ {
			mempool, err := miner.c.GetRawMempool(ctx, chainjson.GRMAll)
			if err != nil {
				return err
			}
			lenMempool = len(mempool)
			if height < chainParams.StakeValidationHeight {
				break
			} else if lenMempool >= 5 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		if i%10 == 0 {
			if err := wallet.selfTransferMany(ctx, 15); err != nil {
				return err
			}
			time.Sleep(200 * time.Millisecond)
		}

		pool, _ := miner.nbLiveTickets(ctx)
		if pool < 200 {
			log.Tracef("Purchasing more tickets due to small size (%d)", pool)
			nbTickets := 15
			_, err = wallet.c.PurchaseTicket(ctx, "default", 100e8, nil, nil, &nbTickets,
				nil, nil, nil, nil, nil)
			if err != nil {
				return err
			}
		}

		//utxos, _ := wallet.nbUtxos(ctx)
		utxos := 0
		log.Tracef("Took %s to mine and sync (height %d, mempool %d, utxos %d, live tickets %d)",
			time.Since(now).Truncate(time.Millisecond),
			height, lenMempool, utxos, pool)
	}

	return nil

}

// genTestChain generates a comprehensive simnet test chain to assert correct
// behavior under check:data.
func genTestChain(ctx context.Context, miner *dcrdProc, wallet *dcrwProc) error {

	// Generate many utxos in the wallet.
	if err := wallet.genManyUtxos(ctx, miner); err != nil {
		return fmt.Errorf("unable to self-transfer many: %v", err)
	}
	log.Debugf("Generated utxos")

	// Send coins to an address multiple times to be deeply confirmed.
	err := wallet.sendToAddr(ctx, targetAddr, 1e8)
	if err != nil {
		return err
	}
	err = wallet.sendToAddr(ctx, targetAddr, 2e8)
	if err != nil {
		return err
	}

	// Purchase a ticket that will never vote, thus requiring a revocation.
	tickets, err := wallet.c.PurchaseTicket(ctx, "default", 100e8, nil, nonVotingAddr, nil,
		nil, nil, nil, nil, nil)
	if err != nil {
		return err
	}
	nonVotingTicketHash := tickets[0]

	// Mine past SVH.
	_, curHeight, err := miner.c.GetBestBlock(ctx)
	if err != nil {
		return err
	}
	nbBlocks := chainParams.StakeValidationHeight - curHeight + 8
	log.Debugf("Mining %d blocks to pass SVH", nbBlocks)
	if err := mineAndSyncWallet(ctx, miner, wallet, uint32(nbBlocks)); err != nil {
		return err
	}

	// Generate a TAdd and TSpend and vote to approve the TSpend.
	log.Debugf("Generating treasury transactions")
	tspendKey := "PsUUktzTqNKDRudiz3F4Chh5CKqqmp5W3ckRDhwECbwrSuWZ9m5fk"
	if err := wallet.importPrivKey(ctx, tspendKey); err != nil {
		return err
	}
	if err := setTreasuryPolicy(ctx, wallet); err != nil {
		return err
	}
	taddTxh, err := sendToTreasury(ctx, wallet, 5e8)
	if err != nil {
		return err
	}
	tspendTxh, err := sendFromTreasury(ctx, wallet, "Ssg3b2pAqa3zJGCk8skQdspvzgMzoHiH361", 3e8)
	if err != nil {
		return err
	}

	// Generate 2 P2SH multisig txs and spend one of them.
	multisigTxh, err := sendToMultisig(ctx, wallet)
	if err != nil {
		return err
	}
	multisigTxh2, err := sendToMultisig(ctx, wallet)
	if err != nil {
		return err
	}
	multisigRedeemTxh, err := redeemMultisig(ctx, wallet, miner, multisigTxh)
	if err != nil {
		return err
	}

	// Mine to approve tspend.
	log.Debugf("Mining to approve tspend")
	if err := mineAndSyncWallet(ctx, miner, wallet, 48*3); err != nil {
		return err
	}
	wallet.logSpendableUtxos(ctx)

	// Ensure all test txs are mined so that future checks behave as
	// expected.
	if minedTspend, err := isMined(ctx, miner, tspendTxh); !minedTspend || err != nil {
		return fmt.Errorf("tspend was not mined (err=%v)", err)
	}
	if minedTadd, err := isMined(ctx, miner, taddTxh); !minedTadd || err != nil {
		return fmt.Errorf("tadd was not mined (err=%v)", err)
	}
	if mined, err := isMined(ctx, miner, nonVotingTicketHash.String()); !mined || err != nil {
		return fmt.Errorf("non-voting ticket was not mined (err=%v)", err)
	}
	if mined, err := isMined(ctx, miner, multisigTxh); !mined || err != nil {
		return fmt.Errorf("multisig tx was not mined (err=%v)", err)
	}
	if mined, err := isMined(ctx, miner, multisigTxh2); !mined || err != nil {
		return fmt.Errorf("multisig tx was not mined (err=%v)", err)
	}
	if mined, err := isMined(ctx, miner, multisigRedeemTxh); !mined || err != nil {
		return fmt.Errorf("multisig redeem tx was not mined (err=%v)", err)
	}

	// Ensure non-voting ticket is revoked.
	log.Debugf("Attempting to mine ticket until revoked: %s", nonVotingTicketHash)
	if err := ensureTicketRevoked(ctx, miner, wallet, nonVotingTicketHash, true); err != nil {
		return err
	}
	log.Debugf("Ticket %s was revoked", nonVotingTicketHash)

	// Ensure there's at least one ticket in the last test block. We mine
	// one block after sending this PurchaseTicket request in order to mine
	// the split tx.
	txhs, err := wallet.c.PurchaseTicket(ctx, "default", 100e8, nil, nil, nil,
		nil, nil, nil, nil, nil)
	if err != nil {
		return err
	}
	log.Debugf("Last purchased ticket: %s", txhs[0])
	time.Sleep(100 * time.Millisecond)
	if _, err := miner.mine(ctx, 1); err != nil {
		return err
	}
	if err := miner.waitTxInMempool(ctx, txhs[0].String()); err != nil {
		return err
	}

	// Send more coins just before mining the last test block.
	err = wallet.sendToAddr(ctx, targetAddr, 3e8)
	if err != nil {
		return err
	}

	// Mine the last test block.
	err = miner.c.RegenTemplate(ctx)
	if err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := miner.mine(ctx, 1); err != nil {
		return err
	}
	// Send coins that will remain in the mempool.
	err = wallet.sendToAddr(ctx, targetAddr, 5e8)
	if err != nil {
		return err
	}

	_, curHeight, err = miner.c.GetBestBlock(ctx)
	if err != nil {
		return err
	}
	wallet.logSpendableUtxos(ctx)
	log.Infof("Finished preparing test chain at height %d", curHeight)

	return nil
}

// checkTestChain performs some spot checks in the online dcrros instance to
// assert it correctly catpures details from the generated test chain.
func checkTestChain(ctx context.Context, dcrros *dcrrosProc, miner *dcrdProc, wallet *dcrwProc) error {

	// Ensure the target address has the expected balance.
	wantBalance := dcrutil.Amount(6e8)
	gotBalance, err := dcrros.addrBalance(ctx, targetAddr.String())
	if err != nil {
		return err
	}
	if gotBalance != wantBalance {
		return fmt.Errorf("target addr does not have expected balance. "+
			"want=%s got=%s", wantBalance, gotBalance)
	}
	log.Debugf("Balance for target addr is %s", gotBalance)

	// Ensure the multisig address has the expected balance.
	wantBalance = dcrutil.Amount(1e8)
	gotBalance, err = dcrros.addrBalance(ctx, "ScbHxeQjnRmfZSW33B4DF3RGYCrsrMMdcaT")
	if err != nil {
		return err
	}
	if gotBalance != wantBalance {
		return fmt.Errorf("multisig addr does not have expected balance. "+
			"want=%s got=%s", wantBalance, gotBalance)
	}
	log.Debugf("Balance for multisig is %s", gotBalance)

	// Ensure the tspend target address has the expected balance.
	wantBalance = 3e8
	gotBalance, err = dcrros.addrBalance(ctx, "Ssg3b2pAqa3zJGCk8skQdspvzgMzoHiH361")
	if err != nil {
		return err
	}
	if gotBalance != wantBalance {
		return fmt.Errorf("tspend target addr does not have expected balance. "+
			"want=%s got=%s", wantBalance, gotBalance)
	}
	log.Debugf("Balance for tspend target is %s", gotBalance)

	// Ensure the treasury has the expected balance.
	wantBalance, err = miner.treasuryBalance(ctx)
	if err != nil {
		return err
	}
	gotBalance, err = dcrros.addrBalance(ctx, types.TreasuryAccountAdddress)
	if err != nil {
		return err
	}
	if gotBalance != wantBalance {
		return fmt.Errorf("unexpected treasury balance. want=%s got=%s",
			wantBalance, gotBalance)
	}
	log.Debugf("Treasury balance is %s", gotBalance)

	lastBlockHash, _, err := miner.c.GetBestBlock(ctx)
	if err != nil {
		return err
	}
	lastBlock, err := miner.c.GetBlock(ctx, lastBlockHash)
	if err != nil {
		return err
	}

	// Verify the balance for the account of a revoked ticket.
	if revocationTx == nil {
		return fmt.Errorf("could not find revocation in last block")
	}
	var addr string
	addr, wantBalance, err = addrAndAmountFromTx(revocationTx, 0)
	if err != nil {
		return err
	}

	gotBalance, err = dcrros.addrBalance(ctx, addr)
	if err != nil {
		return err
	}
	if gotBalance != wantBalance {
		return fmt.Errorf("unexpected revocation balance. want=%s got=%s",
			wantBalance, gotBalance)
	}

	// Ensure the balance for the ticket that was revoked is zeroed.
	wantBalance = 0
	gotBalance, err = dcrros.addrBalance(ctx, nonVotingAddr.String())
	if err != nil {
		return err
	}
	if gotBalance != wantBalance {
		return fmt.Errorf("unexpected revoked ticket balance. want=%s got=%s",
			wantBalance, gotBalance)
	}

	// Find a ticket purchased in the last block and verify its balance is
	// correct.
	foundTicket := false
	for _, tx := range lastBlock.STransactions {
		if !stake.IsSStx(tx) {
			continue
		}

		foundTicket = true
		var addr string
		addr, wantBalance, err = addrAndAmountFromTx(tx, 0)
		if err != nil {
			return err
		}

		gotBalance, err = dcrros.addrBalance(ctx, addr)
		if err != nil {
			return err
		}
		if gotBalance != wantBalance {
			return fmt.Errorf("unexpected ticket balance. want=%s got=%s",
				wantBalance, gotBalance)
		}

		break
	}
	if !foundTicket {
		return fmt.Errorf("could not find ticket in last block")
	}

	// Find a vote in the last block and verify its balance is correct.
	foundVote := false
	for _, tx := range lastBlock.STransactions {
		if !stake.IsSSGen(tx) {
			continue
		}

		foundVote = true
		var addr string
		addr, wantBalance, err = addrAndAmountFromTx(tx, 2)
		if err != nil {
			return err
		}

		gotBalance, err = dcrros.addrBalance(ctx, addr)
		if err != nil {
			return err
		}
		if gotBalance != wantBalance {
			return fmt.Errorf("unexpected vote balance. want=%s got=%s",
				wantBalance, gotBalance)
		}

		// Additionally, check that the balance for the vote's
		// originating ticket is zero.
		ticketHash := tx.TxIn[1].PreviousOutPoint.Hash.String()
		addr, _, err = addrAndAmountFromTxh(ctx, miner, ticketHash, 0)
		if err != nil {
			return err
		}
		wantBalance = 0
		gotBalance, err = dcrros.addrBalance(ctx, addr)
		if err != nil {
			return err
		}
		if gotBalance != wantBalance {
			return fmt.Errorf("unexpected vote balance. want=%s got=%s",
				wantBalance, gotBalance)
		}

		break
	}
	if !foundVote {
		return fmt.Errorf("could not find vote in last block")
	}

	return nil
}
