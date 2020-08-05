package types

import (
	"math"
	"testing"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/wire"
)

type txToRosettaTestCase struct {
	name    string
	tree    int8
	txIndex int
	status  OpStatus
	tx      *wire.MsgTx
	ops     []Op
}

type txToRosettaTestCtx struct {
	testCases   []*txToRosettaTestCase
	chainParams *chaincfg.Params
	fetchInputs PrevInputsFetcher
}

// txToRosettaTestCases builds a set of test cases for converting decred
// transactions to a list of Rosetta transacitons.
func txToRosettaTestCases() *txToRosettaTestCtx {
	chainParams := chaincfg.RegNetParams()
	regular := wire.TxTreeRegular
	stake := wire.TxTreeStake

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

	// Two generic inputs, one from the regular tx tree one from the stake
	// tx tree.
	prevOut1 := &wire.OutPoint{Index: 1, Tree: regular}
	txIn1 := &wire.TxIn{PreviousOutPoint: *prevOut1}
	prevOut2 := &wire.OutPoint{Index: 2, Tree: stake}
	txIn2 := &wire.TxIn{PreviousOutPoint: *prevOut2}

	// TxIn that spends a ticket submission.
	prevOutTicketSubmission := &wire.OutPoint{Index: 0, Tree: stake}
	txInTicketSubmission := &wire.TxIn{PreviousOutPoint: *prevOutTicketSubmission}

	// Stakebase input.
	txInStakebase := &wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Index: math.MaxUint32},
		BlockIndex:       wire.NullBlockIndex,
	}

	// fetchInputs only needs to return the prevInputs we use.
	fetchInputs := func(...*wire.OutPoint) (map[wire.OutPoint]*PrevInput, error) {
		return map[wire.OutPoint]*PrevInput{
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
		tx: &wire.MsgTx{
			TxIn: []*wire.TxIn{
				{}, // Coinbase
			},
			TxOut: []*wire.TxOut{
				txOut1, // Treasury
				{},     // OP_RETURN
				txOut2, // PoW reward
			},
		},
		ops: []Op{{ // Treasury
			Type:    OpTypeCredit,
			IOIndex: 0,
			Out:     txOut1,
			Account: addr1,
			Amount:  -1,
		}, { // PoW reward
			Type:    OpTypeCredit,
			IOIndex: 2,
			Out:     txOut2,
			Account: addr2,
			Amount:  -2,
		}},
	}, {
		name:    "reversed standard regular tx",
		tree:    regular,
		txIndex: 1,
		status:  OpStatusReversed,
		tx: &wire.MsgTx{
			TxIn: []*wire.TxIn{
				txIn1,
				txIn2,
			},
			TxOut: []*wire.TxOut{
				txOut1,
				txOut2,
			},
		},
		ops: []Op{{ // TxOut[0]
			Type:    OpTypeCredit,
			IOIndex: 0,
			Out:     txOut1,
			Account: addr1,
			Amount:  -1,
		}, { // TxOut[1]
			Type:    OpTypeCredit,
			IOIndex: 1,
			Out:     txOut2,
			Account: addr2,
			Amount:  -2,
		}, { // TxIn[0]
			Type:    OpTypeDebit,
			IOIndex: 0,
			In:      txIn1,
			Account: addr1,
			Amount:  1,
		}, { // TxIn[1]
			Type:    OpTypeDebit,
			IOIndex: 1,
			In:      txIn2,
			Account: addr2,
			Amount:  2,
		}},
	}, {
		name:    "coinbase with treasury output",
		tree:    regular,
		txIndex: 0,
		status:  OpStatusSuccess,
		tx: &wire.MsgTx{
			TxIn: []*wire.TxIn{
				{}, // Coinbase
			},
			TxOut: []*wire.TxOut{
				txOut1, // Treasury
				{},     // OP_RETURN
				txOut2, // PoW reward
			},
		},
		ops: []Op{{ // Treasury
			Type:    OpTypeCredit,
			IOIndex: 0,
			Out:     txOut1,
			Account: addr1,
			Amount:  1,
		}, { // PoW reward
			Type:    OpTypeCredit,
			IOIndex: 2,
			Out:     txOut2,
			Account: addr2,
			Amount:  2,
		}},
	}, {
		name:    "standard regular tx",
		tree:    regular,
		txIndex: 1,
		status:  OpStatusSuccess,
		tx: &wire.MsgTx{
			TxIn: []*wire.TxIn{
				txIn1,
				txIn2,
			},
			TxOut: []*wire.TxOut{
				txOut1,
				txOut2,
			},
		},
		ops: []Op{{ // TxIn[0]
			Type:    OpTypeDebit,
			IOIndex: 0,
			In:      txIn1,
			Account: addr1,
			Amount:  -1,
		}, { // TxIn[1]
			Type:    OpTypeDebit,
			IOIndex: 1,
			In:      txIn2,
			Account: addr2,
			Amount:  -2,
		}, { // TxOut[0]
			Type:    OpTypeCredit,
			IOIndex: 0,
			Out:     txOut1,
			Account: addr1,
			Amount:  1,
		}, { // TxOut[1]
			Type:    OpTypeCredit,
			IOIndex: 1,
			Out:     txOut2,
			Account: addr2,
			Amount:  2,
		}},
	}, {
		name:    "ticket with 2 commitments",
		tree:    stake,
		txIndex: 0,
		status:  OpStatusSuccess,
		tx: &wire.MsgTx{
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
		},
		ops: []Op{{ // TxIn[0]
			Type:    OpTypeDebit,
			IOIndex: 0,
			In:      txIn1,
			Account: addr1,
			Amount:  -1,
		}, { // TxIn[1]
			Type:    OpTypeDebit,
			IOIndex: 1,
			In:      txIn2,
			Account: addr2,
			Amount:  -2,
		}, { // TxOut[0] - ticket submission
			Type:    OpTypeCredit,
			IOIndex: 0,
			Out:     txOutTicketSubmission,
			Account: addrTicketSubmission,
			Amount:  10,
		}, { // TxOut[2] - ticket change
			Type:    OpTypeCredit,
			IOIndex: 2,
			Out:     txOutTicketChange,
			Account: addrTicketChange,
			Amount:  20,
		}, { // TxOut[4] - ticket change
			Type:    OpTypeCredit,
			IOIndex: 4,
			Out:     txOutTicketChange,
			Account: addrTicketChange,
			Amount:  20,
		}},
	}, {
		name:    "vote with 2 rewards",
		tree:    stake,
		txIndex: 1,
		status:  OpStatusSuccess,
		tx: &wire.MsgTx{
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
		},
		ops: []Op{{ // TxIn[1]
			Type:    OpTypeDebit,
			IOIndex: 1,
			In:      txInTicketSubmission,
			Account: addrTicketSubmission,
			Amount:  -10,
		}, { // TxOut[2] - stakegen
			Type:    OpTypeCredit,
			IOIndex: 2,
			Out:     txOutStakegen,
			Account: addrStakegen,
			Amount:  30,
		}, { // TxOut[3] - stakegen
			Type:    OpTypeCredit,
			IOIndex: 3,
			Out:     txOutStakegen,
			Account: addrStakegen,
			Amount:  30,
		}},
	}, {
		name:    "revocation with 2 returns",
		tree:    stake,
		txIndex: 2,
		status:  OpStatusSuccess,
		tx: &wire.MsgTx{
			TxIn: []*wire.TxIn{
				txInTicketSubmission,
			},
			TxOut: []*wire.TxOut{
				txOutRevoke,
				txOutRevoke,
			},
		},
		ops: []Op{{ // TxIn[1]
			Type:    OpTypeDebit,
			IOIndex: 0,
			In:      txInTicketSubmission,
			Account: addrTicketSubmission,
			Amount:  -10,
		}, { // TxOut[2] - stakrevoke
			Type:    OpTypeCredit,
			IOIndex: 0,
			Out:     txOutRevoke,
			Account: addrRevoke,
			Amount:  9,
		}, { // TxOut[3] - stakerevoke
			Type:    OpTypeCredit,
			IOIndex: 1,
			Out:     txOutRevoke,
			Account: addrRevoke,
			Amount:  9,
		}},
	}}

	return &txToRosettaTestCtx{
		testCases:   testCases,
		chainParams: chainParams,
		fetchInputs: fetchInputs,
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
	if OpStatus(op.Status) != status {
		t.Fatalf("tx %d op %d: unexpected OpStatus. "+
			"want=%s got=%s", txi, opi,
			OpStatusSuccess, op.Status)
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
}

// TestTxToBlockOps tests that converting a list a transaction to a list of
// Rosetta ops works as intended.
func TestTxToBlockOps(t *testing.T) {

	tests := txToRosettaTestCases()

	for _, tc := range tests.testCases {
		tc := tc
		ok := t.Run(tc.name, func(t *testing.T) {
			op := &Op{
				Tree:    tc.tree,
				Tx:      tc.tx,
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

				// TODO: enable test after rewriting PrevInput.
				/*
					if wop.PrevInput != op.PrevInput {
						t.Fatalf("op %d incorrect PrevInput. "+
							"want=%v, got=%v", opIdx,
							wop.PrevInput, op.PrevInput)
					}
				*/

				// Also test that the op converted to a Rosetta
				// structure has the key information set.
				assertTestCaseOpsMatch(t, 0, opIdx, tc.status,
					op.ROp(), tc)

				opIdx++
				return nil
			}

			err := iterateBlockOpsInTx(op, tests.fetchInputs,
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
	tx *rtypes.Transaction, tc *txToRosettaTestCase) {

	// The number of operations for this tx should match the one expected
	// for this test case.
	if len(tc.ops) != len(tx.Operations) {
		t.Fatalf("block tx %d vs test case %d: mismatched nb of "+
			"ops. want=%d got=%d", txi, tci, len(tc.ops),
			len(tx.Operations))
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
		rb, err := WireBlockToRosetta(b, prev, tctx.fetchInputs, tctx.chainParams)
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
			assertTestCaseTxMatches(t, OpStatusSuccess, txi, tci, tx, tc)

			// Pass on to the next test case.
			tci++
		}
	})

	// Second test case: b disapproves prev.
	t.Run("disapproves parent", func(t *testing.T) {
		b.Header.VoteBits = 0x00
		rb, err := WireBlockToRosetta(b, prev, tctx.fetchInputs, tctx.chainParams)
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
			assertTestCaseTxMatches(t, tc.status, txi, tci, tx, tc)

			// Pass on to the next test case.
			tci++
		}
	})

	// Third test: b disapproves prev but we haven't provided prev
	// (function should error).
	t.Run("disapproves parent with error", func(t *testing.T) {
		b.Header.VoteBits = 0x00
		_, err := WireBlockToRosetta(b, nil, tctx.fetchInputs, tctx.chainParams)
		if err != ErrNeedsPreviousBlock {
			t.Fatalf("unexpected error. want=%v got=%v", ErrNeedsPreviousBlock, err)
		}
	})
}

// TestMempoolTxToRosetta tests that converting a mempool tx to a Rosetta
// transaction works.
func TestMempoolTxToRosetta(t *testing.T) {
	regular := wire.TxTreeRegular
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

		tc := tc
		tci := tci
		ok := t.Run(tc.name, func(t *testing.T) {
			tx, err := MempoolTxToRosetta(tc.tx, tctx.fetchInputs,
				tctx.chainParams)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify the transaction data matches the expected.
			assertTestCaseTxMatches(t, tc.status, 0, tci, tx, tc)
		})
		if !ok {
			break
		}
	}
}
