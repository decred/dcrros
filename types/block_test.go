package types

import (
	"fmt"
	"math"
	"testing"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
)

type testOp struct {
	Type       OpType
	IOIndex    int
	Out        *wire.TxOut
	Account    string
	Amount     dcrutil.Amount
	In         *wire.TxIn
	CoinChange *rtypes.CoinChange
}

type txToRosettaTestCase struct {
	name    string
	tree    int8
	txIndex int
	status  OpStatus
	tx      *wire.MsgTx
	txHash  *chainhash.Hash
	ops     []testOp
}

type txToRosettaTestCtx struct {
	testCases     []*txToRosettaTestCase
	chainParams   *chaincfg.Params
	fetchPrevOuts PrevOutputsFetcher
}

// txToRosettaTestCases builds a set of test cases for converting decred
// transactions to a list of Rosetta transacitons.
func txToRosettaTestCases() *txToRosettaTestCtx {
	chainParams := chaincfg.RegNetParams()
	regular := wire.TxTreeRegular
	stake := wire.TxTreeStake

	// A dummy tx hash.
	txhh := chainhash.HashH([]byte{1, 2, 3})
	txh := &txhh

	// P2PKH address.
	pks1 := mustHex("76a914a5a7f924934685fbca3008c9524dae1cea9f9d3488ac")
	addr1 := "RsPSidp9af5pbGBBQYb3VcRLGzHaPma1Xpv"
	txOut1 := &wire.TxOut{Value: 1, PkScript: pks1}

	// P2SH address.
	pks2 := mustHex("a914d585cd7426d25b4ea5faf1e6987aacfeda3db94287")
	addr2 := "RcaJVhnU11HaKVy95dGaPRMRSSWrb3KK2u1"
	txOut2 := &wire.TxOut{Value: 2, PkScript: pks2}

	// Ticket Submission (voting address).
	pksTicketSubmission := mustHex("ba76a9143f8ab3606760cc0aad241d3b61294b8020c106f988ac")
	addrTicketSubmission := "RsE8neMcziiKdZEGMCUuLVsUh24bVBrtvpg"
	txOutTicketSubmission := &wire.TxOut{Value: 10, PkScript: pksTicketSubmission}

	// Ticket Commitment (reward address).
	pksTicketCommitment := mustHex("6a1e4ed3c44f910a6eaf2a783954829e9f44176851a9a9384b4d010000000058")
	addrTicketCommitment := "0x00006a1e4ed3c44f910a6eaf2a783954829e9f44176851a9a9384b4d010000000058"
	txOutTicketCommitment := &wire.TxOut{Value: 0, PkScript: pksTicketCommitment}

	// The address isn't currently used due to ticket commitments having
	// 0-valued outputs.
	_ = addrTicketCommitment

	// Ticket Change (return address on ticket purchase).
	pksTicketChange := mustHex("bd76a914000000000000000000000000000000000000000088ac")
	addrTicketChange := "Rs8LovBHZfZmC4ShUicmExNWaivPm5cBtNN"
	txOutTicketChange := &wire.TxOut{Value: 20, PkScript: pksTicketChange}

	// Stakegen (reward address on vote).
	pksStakegen := mustHex("bb76a9149ca5b6bd7c71635f1e9156c33a2d36e42705234a88ac")
	addrStakegen := "RsNd5r6tfLCReURsnA3JCEc348LRp9k4x7o"
	txOutStakegen := &wire.TxOut{Value: 30, PkScript: pksStakegen}

	// Vote parent block hash output (OP_RETURN)
	pksVoteBlockHash := mustHex("6a2454c6e01922c7099a7d7b416938a2dfaea32988f0412b75a73b07355c77000000e23f0700")
	txOutVoteBlockHash := &wire.TxOut{PkScript: pksVoteBlockHash}

	// Vote bits output (OP_RETURN)
	pksVoteBits := mustHex("6a06050008000000")
	txOutVoteBits := &wire.TxOut{PkScript: pksVoteBits}

	// Stakerevoke (return address on revocation)
	pksRevoke := mustHex("bc76a91464d4dfc7b9e2c9d1e5c2cc537f546942d2195f7e88ac")
	txOutRevoke := &wire.TxOut{Value: 9, PkScript: pksRevoke}
	addrRevoke := "RsHXxW7fvSuCbnv5azixFrGW6t5xJhRVv7z"

	// Treasury base outputs.
	pksTBase := mustHex("c1")
	txOutTBase := &wire.TxOut{Value: 15, PkScript: pksTBase}

	// Treasury spend outputs.
	pksTSpendOpRet := mustHex("6a2054c6e01922c7099a7d7b416938a2dfaea32988f0412b75a73b07355c77000000")
	txOutTSpendOptRet := &wire.TxOut{PkScript: pksTSpendOpRet}
	pksTSpendOut := append([]byte{txscript.OP_TGEN}, pks1...)
	txOutTSpend := &wire.TxOut{Value: 16, PkScript: pksTSpendOut}

	// Two generic inputs, one from the regular tx tree one from the stake
	// tx tree.
	prevOut1 := &wire.OutPoint{Hash: *txh, Index: 1, Tree: regular}
	txIn1 := &wire.TxIn{PreviousOutPoint: *prevOut1}
	prevOut2 := &wire.OutPoint{Hash: *txh, Index: 2, Tree: stake}
	txIn2 := &wire.TxIn{PreviousOutPoint: *prevOut2}

	// TxIn that spends a ticket submission.
	prevOutTicketSubmission := &wire.OutPoint{Hash: *txh, Index: 0, Tree: stake}
	txInTicketSubmission := &wire.TxIn{PreviousOutPoint: *prevOutTicketSubmission}

	// TxIn that spends from the treasury.
	txInTSpendSigScript := []byte{
		0:  txscript.OP_DATA_64,
		65: txscript.OP_DATA_33,
		66: 0x02,
		99: byte(txscript.OP_TSPEND),
	}
	txInTSpend := wire.NewTxIn(&wire.OutPoint{}, 33, txInTSpendSigScript)

	// TxIn with a null outpoint.
	nullOutPoint := &wire.OutPoint{Index: math.MaxUint32}
	txInNullOutpoint := wire.NewTxIn(nullOutPoint, 0, nil)

	// Stakebase input.
	txInStakebase := &wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Index: math.MaxUint32},
		BlockIndex:       wire.NullBlockIndex,
	}

	// fetchPrevOuts only needs to return the prevOutputs we use.
	fetchPrevOuts := func(...*wire.OutPoint) (map[wire.OutPoint]*PrevOutput, error) {
		return map[wire.OutPoint]*PrevOutput{
			*prevOut1: {
				PkScript: pks1,
				Amount:   1,
				Version:  0,
			},
			*prevOut2: {
				PkScript: pks2,
				Amount:   2,
				Version:  0,
			},
			*prevOutTicketSubmission: {
				PkScript: pksTicketSubmission,
				Amount:   10,
				Version:  0,
			},
		}, nil
	}

	// Aux closures to create CoinChange structures.
	coinCreated := func(txh *chainhash.Hash, index int) *rtypes.CoinChange {
		return &rtypes.CoinChange{
			CoinIdentifier: &rtypes.CoinIdentifier{
				Identifier: fmt.Sprintf("%s:%d", txh, index),
			},
			CoinAction: rtypes.CoinCreated,
		}
	}
	coinSpent := func(txh *chainhash.Hash, index int) *rtypes.CoinChange {
		return &rtypes.CoinChange{
			CoinIdentifier: &rtypes.CoinIdentifier{
				Identifier: fmt.Sprintf("%s:%d", txh, index),
			},
			CoinAction: rtypes.CoinSpent,
		}
	}

	// Test Transacions.

	coinbaseWithTreasury := &wire.MsgTx{
		TxIn: []*wire.TxIn{
			{}, // Coinbase
		},
		TxOut: []*wire.TxOut{
			txOut1, // Treasury
			{},     // OP_RETURN
			txOut2, // PoW reward
		},
	}

	regularTx := &wire.MsgTx{
		TxIn: []*wire.TxIn{
			txIn1,
			txIn2,
		},
		TxOut: []*wire.TxOut{
			txOut1,
			txOut2,
		},
	}

	ticket := &wire.MsgTx{
		TxIn: []*wire.TxIn{
			txIn1,
			txIn2,
		},
		TxOut: []*wire.TxOut{
			txOutTicketSubmission,
			txOutTicketCommitment,
			txOutTicketChange,
			txOutTicketCommitment,
			txOutTicketChange,
		},
	}

	vote := &wire.MsgTx{
		TxIn: []*wire.TxIn{
			txInStakebase, // Stakebase
			txInTicketSubmission,
		},
		TxOut: []*wire.TxOut{
			txOutVoteBlockHash, // Parent block hash OP_RETURN
			txOutVoteBits,      // Vote Bits OP_RETURN
			txOutStakegen,
			txOutStakegen,
		},
	}

	revocation := &wire.MsgTx{
		TxIn: []*wire.TxIn{
			txInTicketSubmission,
		},
		TxOut: []*wire.TxOut{
			txOutRevoke,
			txOutRevoke,
		},
	}

	tbase := &wire.MsgTx{
		Version: 3,
		TxIn: []*wire.TxIn{
			txInNullOutpoint, // Coinbase
		},
		TxOut: []*wire.TxOut{
			txOutTBase, // Treasury
			{PkScript: mustHex("6a0c020000003d45dc9f2d6d6b25")}, // OP_RETURN
		},
	}

	tadd := &wire.MsgTx{
		Version: 3,
		TxIn: []*wire.TxIn{
			txIn1,
		},
		TxOut: []*wire.TxOut{
			txOutTBase, // Treasury
			txOutTicketChange,
		},
	}

	tspend := &wire.MsgTx{
		Version: 3,
		TxIn: []*wire.TxIn{
			txInTSpend,
		},
		TxOut: []*wire.TxOut{
			txOutTSpendOptRet, // height OP_RETURN
			txOutTSpend,       // TSpend Output
		},
	}

	// Each test case lists the op values that need to be configured prior
	// to calling iterateBlockOpsInTx, the Decred tx and the expected
	// resulting list of Rosetta operations.
	//
	// The order of these test cases is with all tests that are reversed
	// first so that block tests can easily test both approved and
	// disapproved blocks.
	testCases := []*txToRosettaTestCase{{
		name:    "reversed coinbase with treasury output",
		tree:    regular,
		txIndex: 0,
		status:  OpStatusReversed,
		tx:      coinbaseWithTreasury,
		txHash:  txh,
		ops: []testOp{{ // Treasury
			Type:       OpTypeCredit,
			IOIndex:    0,
			Out:        txOut1,
			Account:    addr1,
			Amount:     -1,
			CoinChange: coinSpent(coinbaseWithTreasury.CachedTxHash(), 0),
		}, { // PoW reward
			Type:       OpTypeCredit,
			IOIndex:    2,
			Out:        txOut2,
			Account:    addr2,
			Amount:     -2,
			CoinChange: coinSpent(coinbaseWithTreasury.CachedTxHash(), 2),
		}},
	}, {
		name:    "reversed standard regular tx",
		tree:    regular,
		txIndex: 1,
		status:  OpStatusReversed,
		tx:      regularTx,
		txHash:  txh,
		ops: []testOp{{ // TxOut[0]
			Type:       OpTypeCredit,
			IOIndex:    0,
			Out:        txOut1,
			Account:    addr1,
			Amount:     -1,
			CoinChange: coinSpent(regularTx.CachedTxHash(), 0),
		}, { // TxOut[1]
			Type:       OpTypeCredit,
			IOIndex:    1,
			Out:        txOut2,
			Account:    addr2,
			Amount:     -2,
			CoinChange: coinSpent(regularTx.CachedTxHash(), 1),
		}, { // TxIn[0]
			Type:       OpTypeDebit,
			IOIndex:    0,
			In:         txIn1,
			Account:    addr1,
			Amount:     1,
			CoinChange: coinCreated(txh, 1),
		}, { // TxIn[1]
			Type:       OpTypeDebit,
			IOIndex:    1,
			In:         txIn2,
			Account:    addr2,
			Amount:     2,
			CoinChange: coinCreated(txh, 2),
		}},
	}, {
		name:    "coinbase with treasury output",
		tree:    regular,
		txIndex: 0,
		status:  OpStatusSuccess,
		tx:      coinbaseWithTreasury,
		txHash:  txh,
		ops: []testOp{{ // Treasury
			Type:       OpTypeCredit,
			IOIndex:    0,
			Out:        txOut1,
			Account:    addr1,
			Amount:     1,
			CoinChange: coinCreated(coinbaseWithTreasury.CachedTxHash(), 0),
		}, { // PoW reward
			Type:       OpTypeCredit,
			IOIndex:    2,
			Out:        txOut2,
			Account:    addr2,
			Amount:     2,
			CoinChange: coinCreated(coinbaseWithTreasury.CachedTxHash(), 2),
		}},
	}, {
		name:    "standard regular tx",
		tree:    regular,
		txIndex: 1,
		status:  OpStatusSuccess,
		tx:      regularTx,
		txHash:  txh,
		ops: []testOp{{ // TxIn[0]
			Type:       OpTypeDebit,
			IOIndex:    0,
			In:         txIn1,
			Account:    addr1,
			Amount:     -1,
			CoinChange: coinSpent(txh, 1),
		}, { // TxIn[1]
			Type:       OpTypeDebit,
			IOIndex:    1,
			In:         txIn2,
			Account:    addr2,
			Amount:     -2,
			CoinChange: coinSpent(txh, 2),
		}, { // TxOut[0]
			Type:       OpTypeCredit,
			IOIndex:    0,
			Out:        txOut1,
			Account:    addr1,
			Amount:     1,
			CoinChange: coinCreated(regularTx.CachedTxHash(), 0),
		}, { // TxOut[1]
			Type:       OpTypeCredit,
			IOIndex:    1,
			Out:        txOut2,
			Account:    addr2,
			Amount:     2,
			CoinChange: coinCreated(regularTx.CachedTxHash(), 1),
		}},
	}, {
		name:    "tbase", // Needs to be the first stake tx test case.
		tree:    stake,
		txIndex: 0,
		status:  OpStatusSuccess,
		tx:      tbase,
		txHash:  txh,
		ops: []testOp{{ // Treasury
			Type:       OpTypeCredit,
			IOIndex:    0,
			Out:        txOutTBase,
			Account:    TreasuryAccountAdddress,
			Amount:     15,
			CoinChange: nil, // Treasury account is not utxo based.
		}},
	},
		{
			name:    "ticket with 2 commitments",
			tree:    stake,
			txIndex: 0,
			status:  OpStatusSuccess,
			tx:      ticket,
			txHash:  txh,
			ops: []testOp{{ // TxIn[0]
				Type:       OpTypeDebit,
				IOIndex:    0,
				In:         txIn1,
				Account:    addr1,
				Amount:     -1,
				CoinChange: coinSpent(txh, 1),
			}, { // TxIn[1]
				Type:       OpTypeDebit,
				IOIndex:    1,
				In:         txIn2,
				Account:    addr2,
				Amount:     -2,
				CoinChange: coinSpent(txh, 2),
			}, { // TxOut[0] - ticket submission
				Type:       OpTypeCredit,
				IOIndex:    0,
				Out:        txOutTicketSubmission,
				Account:    addrTicketSubmission,
				Amount:     10,
				CoinChange: coinCreated(ticket.CachedTxHash(), 0),
			}, { // TxOut[2] - ticket change
				Type:       OpTypeCredit,
				IOIndex:    2,
				Out:        txOutTicketChange,
				Account:    addrTicketChange,
				Amount:     20,
				CoinChange: coinCreated(ticket.CachedTxHash(), 2),
			}, { // TxOut[4] - ticket change
				Type:       OpTypeCredit,
				IOIndex:    4,
				Out:        txOutTicketChange,
				Account:    addrTicketChange,
				Amount:     20,
				CoinChange: coinCreated(ticket.CachedTxHash(), 4),
			}},
		}, {
			name:    "vote with 2 rewards",
			tree:    stake,
			txIndex: 1,
			status:  OpStatusSuccess,
			tx:      vote,
			txHash:  txh,
			ops: []testOp{{ // TxIn[1]
				Type:       OpTypeDebit,
				IOIndex:    1,
				In:         txInTicketSubmission,
				Account:    addrTicketSubmission,
				Amount:     -10,
				CoinChange: coinSpent(txh, 0),
			}, { // TxOut[2] - stakegen
				Type:       OpTypeCredit,
				IOIndex:    2,
				Out:        txOutStakegen,
				Account:    addrStakegen,
				Amount:     30,
				CoinChange: coinCreated(vote.CachedTxHash(), 2),
			}, { // TxOut[3] - stakegen
				Type:       OpTypeCredit,
				IOIndex:    3,
				Out:        txOutStakegen,
				Account:    addrStakegen,
				Amount:     30,
				CoinChange: coinCreated(vote.CachedTxHash(), 3),
			}},
		}, {
			name:    "revocation with 2 returns",
			tree:    stake,
			txIndex: 2,
			status:  OpStatusSuccess,
			tx:      revocation,
			ops: []testOp{{ // TxIn[1]
				Type:       OpTypeDebit,
				IOIndex:    0,
				In:         txInTicketSubmission,
				Account:    addrTicketSubmission,
				Amount:     -10,
				CoinChange: coinSpent(txh, 0),
			}, { // TxOut[0] - stakrevoke
				Type:       OpTypeCredit,
				IOIndex:    0,
				Out:        txOutRevoke,
				Account:    addrRevoke,
				Amount:     9,
				CoinChange: coinCreated(revocation.CachedTxHash(), 0),
			}, { // TxOut[1] - stakerevoke
				Type:       OpTypeCredit,
				IOIndex:    1,
				Out:        txOutRevoke,
				Account:    addrRevoke,
				Amount:     9,
				CoinChange: coinCreated(revocation.CachedTxHash(), 1),
			}},
		}, {
			name:    "tadd",
			tree:    stake,
			txIndex: 1,
			status:  OpStatusSuccess,
			tx:      tadd,
			txHash:  txh,
			ops: []testOp{{ // TxIn[0]
				Type:       OpTypeDebit,
				IOIndex:    0,
				In:         txIn1,
				Account:    addr1,
				Amount:     -1,
				CoinChange: coinSpent(txh, 1),
			}, { // Treasury output
				Type:       OpTypeCredit,
				IOIndex:    0,
				Out:        txOutTBase,
				Account:    TreasuryAccountAdddress,
				Amount:     15,
				CoinChange: nil, // Treasury account is not utxo based.
			}, { // Change
				Type:       OpTypeCredit,
				IOIndex:    1,
				Out:        txOutTicketChange,
				Account:    addrTicketChange,
				Amount:     20,
				CoinChange: coinCreated(tadd.CachedTxHash(), 1),
			}},
		}, {
			name:    "tspend",
			tree:    stake,
			txIndex: 1,
			status:  OpStatusSuccess,
			tx:      tspend,
			txHash:  txh,
			ops: []testOp{{ // TxIn[0]
				Type:       OpTypeDebit,
				IOIndex:    0,
				In:         txInTSpend,
				Account:    TreasuryAccountAdddress,
				Amount:     -33,
				CoinChange: nil, // Treasury account is not utxo based.
			}, { // TSpend output
				Type:       OpTypeCredit,
				IOIndex:    1,
				Out:        txOutTSpend,
				Account:    addr1,
				Amount:     16,
				CoinChange: coinCreated(tspend.CachedTxHash(), 1),
			}},
		}}

	return &txToRosettaTestCtx{
		testCases:     testCases,
		chainParams:   chainParams,
		fetchPrevOuts: fetchPrevOuts,
	}
}

// assertTestCaseOpsMatch asserts that the given operation matches the
// operation opi of the given test case.
func assertTestCaseOpsMatch(t *testing.T, txi, opi int, status OpStatus,
	op *rtypes.Operation, tc *txToRosettaTestCase) {

	// OperationIndentifier.Index must be monotonically
	// increasing (i.e. it matches the index in the slice).
	if op.OperationIdentifier.Index != int64(opi) {
		t.Fatalf("tx %d op %d: unexpected OpId.Index. "+
			"want=%d got=%d", txi, opi,
			opi, op.OperationIdentifier.Index)
	}

	// All operations should have the appropriate status.
	if OpStatus(*op.Status) != status {
		t.Fatalf("tx %d op %d: unexpected OpStatus. "+
			"want=%s got=%s", txi, opi,
			status, *op.Status)
	}

	// Verify key properties between test case and
	// transaction.

	// Amount.
	opAmt, err := RosettaToDcrAmount(op.Amount)
	if err != nil {
		t.Fatalf("unexpected conversion error: %v", err)
	}
	if opAmt != tc.ops[opi].Amount {
		t.Fatalf("tx %d op %d unexpected amount. want=%d "+
			"got=%d", txi, opi, tc.ops[opi].Amount,
			opAmt)
	}

	// Account Address.
	if op.Account.Address != tc.ops[opi].Account {
		t.Fatalf("tx %d op %d unexpected account. want=%s "+
			"got=%s", txi, opi, tc.ops[opi].Account,
			op.Account.Address)
	}

	// OpType.
	if OpType(op.Type) != tc.ops[opi].Type {
		t.Fatalf("tx %d op %d unexpected OpType. want=%s "+
			"got=%s", txi, opi, tc.ops[opi].Type,
			op.Type)
	}

	wantCC := tc.ops[opi].CoinChange
	gotCC := op.CoinChange
	wantNilCC := wantCC == nil
	gotNilCC := gotCC == nil
	if wantNilCC != gotNilCC {
		t.Fatalf("tx %d op %d unexpected coinchage. want=%v got=%v",
			txi, opi, wantNilCC, gotNilCC)
	}

	if wantNilCC {
		// Nothing else to check as CoinChange is nil as expected.
		return
	}

	if wantCC.CoinIdentifier.Identifier != gotCC.CoinIdentifier.Identifier {
		t.Fatalf("tx %d op %d unexpected CoinChange.CoinIdentifier.Identifier. "+
			"want=%s got=%s", txi, opi, wantCC.CoinIdentifier.Identifier,
			gotCC.CoinIdentifier.Identifier)
	}

	if wantCC.CoinAction != gotCC.CoinAction {
		t.Fatalf("tx %d op %d unexpected CoinChange.CoinAction. "+
			"want=%s got=%s", txi, opi, wantCC.CoinAction,
			gotCC.CoinAction)
	}

}

// TestTxToBlockOps tests that converting a list a transaction to a list of
// Rosetta ops works as intended.
func TestTxToBlockOps(t *testing.T) {

	tests := txToRosettaTestCases()

	for _, tc := range tests.testCases {
		tc := tc
		ok := t.Run(tc.name, func(t *testing.T) {
			txh := tc.tx.CachedTxHash()
			op := &Op{
				Tree:    tc.tree,
				Tx:      tc.tx,
				TxHash:  *txh,
				TxIndex: tc.txIndex,
				Status:  tc.status,
			}

			var opIdx int
			applyOp := func(op *Op) error {
				if opIdx >= len(tc.ops) {
					t.Fatalf("too few ops in test case: "+
						"want=%d, got=%d", opIdx+1, len(tc.ops))
				}

				if op.OpIndex != int64(opIdx) {
					t.Fatalf("incorrect OpIndex. want=%d "+
						"got=%d", opIdx, op.OpIndex)
				}
				wop := tc.ops[opIdx] // wop = wantOp

				if wop.Type != op.Type {
					t.Fatalf("op %d incorrect opType. "+
						"want=%s, got=%s", opIdx, wop.Type,
						op.Type)
				}

				if wop.IOIndex != op.IOIndex {
					t.Fatalf("op %d incorrect IOIndex. "+
						"want=%d, got=%d", opIdx,
						wop.IOIndex, op.IOIndex)
				}

				if wop.Out != op.Out {
					t.Fatalf("op %d incorrect Out. "+
						"want=%v, got=%v", opIdx,
						wop.Out, op.Out)
				}

				if wop.In != op.In {
					t.Fatalf("op %d incorrect In. "+
						"want=%v, got=%v", opIdx,
						wop.In, op.In)
				}

				if wop.Account != op.Account {
					t.Fatalf("op %d incorrect Account. "+
						"want=%s, got=%s", opIdx,
						wop.Account, op.Account)
				}

				if wop.Amount != op.Amount {
					t.Fatalf("op %d incorrect Amount. "+
						"want=%d, got=%d", opIdx,
						wop.Amount, op.Amount)
				}

				// TODO: enable test after rewriting PrevOutput.
				/*
					if wop.PrevOutput != op.PrevOutput {
						t.Fatalf("op %d incorrect PrevOutput. "+
							"want=%v, got=%v", opIdx,
							wop.PrevOutput, op.PrevOutput)
					}
				*/

				// Also test that the op converted to a Rosetta
				// structure has the key information set.
				rop := op.ROp()
				assertTestCaseOpsMatch(t, 0, opIdx, tc.status,
					rop, tc)

				opIdx++
				return nil
			}

			err := iterateBlockOpsInTx(op, tests.fetchPrevOuts,
				applyOp, tests.chainParams)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if opIdx < len(tc.ops) {
				t.Fatalf("not enough ops generated. want=%d "+
					"got=%d", len(tc.ops), opIdx)
			}
		})

		if !ok {
			break
		}
	}

}

// assertTestCaseTxMatches asserts that the given Rosetta transaction matches
// what was expected for a given test case.
func assertTestCaseTxMatches(t *testing.T, status OpStatus, txi, tci int,
	tx *rtypes.Transaction, tc *txToRosettaTestCase, prevBlockHash *chainhash.Hash) {

	// The number of operations for this tx should match the one expected
	// for this test case.
	if len(tc.ops) != len(tx.Operations) {
		t.Fatalf("block tx %d vs test case %d: mismatched nb of "+
			"ops. want=%d got=%d", txi, tci, len(tc.ops),
			len(tx.Operations))
	}

	wantTxId := tc.tx.CachedTxHash().String()
	if status == OpStatusReversed {
		wantTxId = prevBlockHash.String() + ":" + wantTxId
	}
	if tx.TransactionIdentifier.Hash != wantTxId {
		t.Fatalf("block tx %d vs test case %d: mismatched tx indentifier. "+
			"want=%s got=%s", txi, tci, wantTxId,
			tx.TransactionIdentifier.Hash)
	}

	for opi, op := range tx.Operations {
		assertTestCaseOpsMatch(t, txi, opi, status, op, tc)
	}
}

// TestBlockToRosetta tests that converting a Decred block to a Rosetta block
// works as expected.
func TestBlockToRosetta(t *testing.T) {
	regular := wire.TxTreeRegular
	stake := wire.TxTreeStake

	// Build the test and previous block. The previous block contains the
	// transactions of the testing context (test cases) that are listed as
	// reversed.
	b := &wire.MsgBlock{
		Header: wire.BlockHeader{
			Height: 1,
		},
	}
	prev := &wire.MsgBlock{}
	prevBlockHash := prev.BlockHash()

	tctx := txToRosettaTestCases()
	for _, tc := range tctx.testCases {
		switch {
		case tc.tree == regular && tc.status == OpStatusSuccess:
			b.Transactions = append(b.Transactions, tc.tx)

		case tc.tree == regular && tc.status == OpStatusReversed:
			prev.Transactions = append(prev.Transactions, tc.tx)

		case tc.tree == stake:
			b.STransactions = append(b.STransactions, tc.tx)
		}
	}

	// Add a dummy stx in prev to ensure it doesn't get reversed.
	prev.STransactions = append(prev.STransactions, b.STransactions[0])

	// First test case: b approves prev.
	t.Run("approves parent", func(t *testing.T) {
		b.Header.VoteBits = 0x01
		rb, err := WireBlockToRosetta(b, prev, tctx.fetchPrevOuts, tctx.chainParams)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// There should be as many txs as Transactions+STransactions.
		wantTxCount := len(b.Transactions) + len(b.STransactions)
		if wantTxCount != len(rb.Transactions) {
			t.Fatalf("unexpected number of txs. want=%d got=%d",
				wantTxCount, len(rb.Transactions))
		}

		// Verify transaction data.
		var tci int // tci == test case index
		for txi, tx := range rb.Transactions {
			// Since this first test only checks non-reversed transactions,
			// skip testcases with wrong op status.
			for tctx.testCases[tci].status != OpStatusSuccess {
				tci++
			}
			tc := tctx.testCases[tci]

			// Verify the transaction data matches the expected.
			assertTestCaseTxMatches(t, OpStatusSuccess, txi, tci,
				tx, tc, &prevBlockHash)

			// Pass on to the next test case.
			tci++
		}
	})

	// Second test case: b disapproves prev.
	t.Run("disapproves parent", func(t *testing.T) {
		b.Header.VoteBits = 0x00
		rb, err := WireBlockToRosetta(b, prev, tctx.fetchPrevOuts, tctx.chainParams)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// There should be as many txs as Transactions + STransactions
		// + prev.Transaction
		wantTxCount := len(b.Transactions) + len(b.STransactions) +
			len(prev.Transactions)
		if wantTxCount != len(rb.Transactions) {
			t.Fatalf("unexpected number of txs. want=%d got=%d",
				wantTxCount, len(rb.Transactions))
		}

		// Verify transaction data.
		var tci int // tci == test case index
		for txi, tx := range rb.Transactions {
			tc := tctx.testCases[tci]

			// Verify the transaction data matches the expected.
			t.Run(tc.name, func(t *testing.T) {
				assertTestCaseTxMatches(t, tc.status, txi, tci,
					tx, tc, &prevBlockHash)
			})

			// Pass on to the next test case.
			tci++
		}
	})

	// Third test: b disapproves prev but we haven't provided prev
	// (function should error).
	t.Run("disapproves parent with error", func(t *testing.T) {
		b.Header.VoteBits = 0x00
		_, err := WireBlockToRosetta(b, nil, tctx.fetchPrevOuts, tctx.chainParams)
		if err != ErrNeedsPreviousBlock {
			t.Fatalf("unexpected error. want=%v got=%v", ErrNeedsPreviousBlock, err)
		}
	})
}

// TestMempoolTxToRosetta tests that converting a mempool tx to a Rosetta
// transaction works.
func TestMempoolTxToRosetta(t *testing.T) {
	regular := wire.TxTreeRegular
	stake := wire.TxTreeStake
	tctx := txToRosettaTestCases()

	for tci, tc := range tctx.testCases {
		// Mempool only returns successful ops so no point in trying
		// test cases that expect a reversed tx.
		if tc.status != OpStatusSuccess {
			continue
		}

		// Skip coinbase since it's never found in the mempool.
		if tc.tree == regular && tc.txIndex == 0 {
			continue
		}

		// Skip treasurybase since it's never found in the mempool.
		if tc.tree == stake && tc.txIndex == 0 {
			continue
		}

		tc := tc
		tci := tci
		ok := t.Run(tc.name, func(t *testing.T) {
			tx, err := MempoolTxToRosetta(tc.tx, tctx.fetchPrevOuts,
				tctx.chainParams)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify the transaction data matches the expected.
			assertTestCaseTxMatches(t, tc.status, 0, tci, tx, tc, nil)
		})
		if !ok {
			break
		}
	}
}
