package types

import (
	"math"
	"testing"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/wire"
)

func TestTxToBlockOps(t *testing.T) {
	regular := wire.TxTreeRegular
	stake := wire.TxTreeStake
	chainParams := chaincfg.RegNetParams()

	// P2PKH address.
	pks1 := mustHex("76a914a5a7f924934685fbca3008c9524dae1cea9f9d3488ac")
	addr1 := "RsPSidp9af5pbGBBQYb3VcRLGzHaPma1Xpv"
	txOut1 := &wire.TxOut{Value: 1, PkScript: pks1}

	// P2SH address.
	pks2 := mustHex("a914d585cd7426d25b4ea5faf1e6987aacfeda3db94287")
	addr2 := "RcaJVhnU11HaKVy95dGaPRMRSSWrb3KK2u1"
	txOut2 := &wire.TxOut{Value: 2, PkScript: pks2}

	// Ticket Submission (voting address).
	pksTicketSubmission := mustHex("6a1ed251f3a9ff398efbd95738a40efe0ddbd18e764234ebf69a000000000058")
	addrTicketSubmission := "0x00006a1ed251f3a9ff398efbd95738a40efe0ddbd18e764234ebf69a000000000058"
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
	tests := []struct {
		name    string
		tree    int8
		txIndex int
		status  OpStatus
		tx      *wire.MsgTx
		ops     []Op
	}{{
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
		txIndex: 1,
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
		txIndex: 1,
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

	for _, tc := range tests {
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

				opIdx++
				return nil
			}

			err := iterateBlockOpsInTx(op, fetchInputs, applyOp, chainParams)
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
