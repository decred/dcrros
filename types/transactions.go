// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package types

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
)

// sigHashType is the only sig hash type supported for signing transacions at
// the moment. This MUST be SigHashAll.
const sigHashType = txscript.SigHashAll

type errKeyNotFound string

func (e errKeyNotFound) Error() string {
	return fmt.Sprintf("key %s does not exist", string(e))
}

// decodeOutPoint deserializes the given string into an outpoint.
func decodeOutPoint(s string) (wire.OutPoint, error) {
	split := strings.Split(s, ":")
	var out wire.OutPoint
	if len(split) != 2 {
		return out, fmt.Errorf("string does not have outpoint format '<hex>:<index>''")
	}

	if len(split[0]) != 64 {
		return out, fmt.Errorf("hex part of outpoint does not encode 32 bytes")
	}

	if err := chainhash.Decode(&out.Hash, split[0]); err != nil {
		return out, fmt.Errorf("invalid hash at start of outpoint string")
	}

	idx, err := strconv.ParseUint(split[1], 10, 32)
	if err != nil {
		return out, fmt.Errorf("invalid int at end of outpoint string")
	}

	out.Index = uint32(idx)

	return out, nil
}

func metadataInt8(m map[string]interface{}, k string, i *int8) error {
	v, ok := m[k]
	if !ok {
		return errKeyNotFound(k)
	}

	switch v := v.(type) {
	case float64:
		if v != float64(int8(v)) {
			return fmt.Errorf("float64 value '%g' is not a valid int8", v)
		}

		*i = int8(v)

	case int8:
		*i = v

	default:
		return fmt.Errorf("unconvertable type %T to int8", v)
	}

	return nil
}

func metadataUint16(m map[string]interface{}, k string, i *uint16) error {
	v, ok := m[k]
	if !ok {
		return errKeyNotFound(k)
	}

	switch v := v.(type) {
	case float64:
		if v != float64(uint16(v)) {
			return fmt.Errorf("float64 value '%g' is not a valid uint16", v)
		}

		*i = uint16(v)

	case uint16:
		*i = v

	default:
		return fmt.Errorf("unconvertable type %T to uint16", v)
	}

	return nil
}

func metadataUint32(m map[string]interface{}, k string, i *uint32) error {
	v, ok := m[k]
	if !ok {
		return errKeyNotFound(k)
	}

	switch v := v.(type) {
	case float64:
		if v != float64(uint32(v)) {
			return fmt.Errorf("float64 value '%g' is not a valid uint32", v)
		}

		*i = uint32(v)

	case uint32:
		*i = v

	default:
		return fmt.Errorf("unconvertable type %T to uint32", v)
	}

	return nil
}

func metadataHex(m map[string]interface{}, k string, b *[]byte) error {
	v, ok := m[k]
	if !ok {
		return errKeyNotFound(k)
	}

	switch v := v.(type) {
	case string:
		bts, err := hex.DecodeString(v)
		if err != nil {
			return err
		}

		*b = bts
	default:
		return fmt.Errorf("unconvertable type %T to chainhash.Hash", v)
	}

	return nil
}

func rosettaOpToTx(op *rtypes.Operation, tx *wire.MsgTx, chainParams *chaincfg.Params) error {
	m := op.Metadata

	opAmt, err := RosettaToDcrAmount(op.Amount)
	if err != nil {
		return fmt.Errorf("unable to decode op value: %v", err)
	}

	switch OpType(op.Type) {
	case OpTypeDebit:
		var prevOut wire.OutPoint

		// Debits require an originating coins_spent CoinChange so we can fill
		// PrevousOutPoint.
		if op.CoinChange == nil {
			return fmt.Errorf("debit operation needs to have a filled coin_change")
		}
		if op.CoinChange.CoinIdentifier == nil {
			return fmt.Errorf("debit operation needs to have a filled coin_change.coin_identifier")
		}
		if op.CoinChange.CoinAction != rtypes.CoinSpent {
			return fmt.Errorf("debit operation needs a coin_spent coin_action")
		}
		if prevOut, err = decodeOutPoint(op.CoinChange.CoinIdentifier.Identifier); err != nil {
			return fmt.Errorf("unable to decode outpoint: %v", err)
		}

		// Ignore if prev_tree is not found (defaults to regular tree).
		if err := metadataInt8(m, "prev_tree", &prevOut.Tree); err != nil && !errors.Is(err, errKeyNotFound("prev_tree")) {
			return fmt.Errorf("unable to decode prev_tree: %v", err)
		}

		in := wire.NewTxIn(&prevOut, int64(opAmt), nil)

		if err := metadataUint32(m, "sequence", &in.Sequence); err != nil && !errors.Is(err, errKeyNotFound("sequence")) {
			return fmt.Errorf("unable to decode sequence: %v", err)
		}
		if err := metadataUint32(m, "block_height", &in.BlockHeight); err != nil && !errors.Is(err, errKeyNotFound("block_height")) {
			return fmt.Errorf("unable to decode block_height: %v", err)
		}
		if err := metadataUint32(m, "block_index", &in.BlockIndex); err != nil && !errors.Is(err, errKeyNotFound("block_index")) {
			return fmt.Errorf("unable to decode block_index: %v", err)
		}
		if err := metadataHex(m, "signature_script", &in.SignatureScript); err != nil && !errors.Is(err, errKeyNotFound("signature_script")) {
			return fmt.Errorf("unable to decode signatureScript: %v", err)
		}
		tx.TxIn = append(tx.TxIn, in)
	case OpTypeCredit:
		out := &wire.TxOut{
			Value: int64(opAmt),
		}

		if op.Account == nil {
			return fmt.Errorf("nil account")
		}

		var err error
		out.Version, out.PkScript, err = rosettaAccountToPkScript(op.Account, chainParams)
		if err != nil {
			return fmt.Errorf("unable to decode account into pkscript: %v", err)
		}

		tx.TxOut = append(tx.TxOut, out)

	default:
		return ErrUnkownOpType
	}

	return nil
}

// RosettaOpsToTx converts the given tx metadata and list of Rosetta operations
// into a Decred transaction. The transaction may be signed or unsigned
// depending on the content of the operations.
func RosettaOpsToTx(txMeta map[string]interface{}, ops []*rtypes.Operation, chainParams *chaincfg.Params) (*wire.MsgTx, error) {
	tx := &wire.MsgTx{}

	// We ignore errKeyNotFound in the next ones so that the tx defaults to the
	// standard ones.
	if err := metadataUint16(txMeta, "version", &tx.Version); err != nil && !errors.Is(err, errKeyNotFound("version")) {
		return nil, ErrInvalidTxMetadata.Msgf("unable to decode tx version: %v", err)
	}
	if err := metadataUint32(txMeta, "expiry", &tx.Expiry); err != nil && !errors.Is(err, errKeyNotFound("expiry")) {
		return nil, ErrInvalidTxMetadata.Msgf("unable to decode expiry: %v", err)
	}
	if err := metadataUint32(txMeta, "locktime", &tx.LockTime); err != nil && !errors.Is(err, errKeyNotFound("locktime")) {
		return nil, ErrInvalidTxMetadata.Msgf("unable to decode locktime: %v", err)
	}

	for i, op := range ops {
		if err := rosettaOpToTx(op, tx, chainParams); err != nil {
			return nil, ErrInvalidOp.Msgf("error converting op %d to tx: %v",
				i, err)
		}
	}

	return tx, nil
}

func extractInputSignPayload(op *rtypes.Operation, tx *wire.MsgTx, idx int,
	chainParams *chaincfg.Params) (*rtypes.SigningPayload, error) {

	if idx >= len(tx.TxIn) {
		return nil, fmt.Errorf("trying to sign inexistent input %d", idx)
	}

	if op.Account == nil {
		return nil, fmt.Errorf("account cannot be nil")
	}
	if op.Account.Metadata == nil {
		return nil, fmt.Errorf("account.metadata cannot be nil")
	}

	// TODO: Use the prefix hash (tx.TxHash()) to speed up calculations?

	// Figure out the original version and PkScript given the
	// address.
	var version uint16
	if err := metadataUint16(op.Account.Metadata, "script_version", &version); err != nil {
		return nil, fmt.Errorf("unable to decode script_version: %v", err)
	}

	// Addresses with a version other than zero or encoded in raw format
	// ("0x" prefix) are not standardized, so we don't know how to sign
	// them. They might be signable by some other software though (e.g.
	// some other PSDT signer) so we don't error out on those cases but
	// simply signal that those inputs don't produce a signing payload.
	if version != 0 || strings.HasPrefix(op.Account.Address, "0x") {
		return nil, nil
	}

	// Determine the signature type based on the type of address. Note we
	// only support P2PKH for ecdsa (*not* P2PK).
	var sigType rtypes.SignatureType
	addr, err := dcrutil.DecodeAddress(op.Account.Address, chainParams)
	if err != nil {
		return nil, fmt.Errorf("unable to decode address: %v", err)
	}
	switch addr.(type) {
	case *dcrutil.AddressPubKeyHash:
		sigType = rtypes.Ecdsa
	default:
		// Other unknown address types are not an error, they just
		// don't produce a signing payload we know of.
		return nil, nil
	}

	// Generate the corresponding pkscript and signature hash.
	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, fmt.Errorf("unable to generate pkscript: %v", err)
	}

	sigHash, err := txscript.CalcSignatureHash(pkScript, sigHashType, tx, idx, nil)
	if err != nil {
		return nil, fmt.Errorf("error during calcSigHash: %v", err)
	}

	return &rtypes.SigningPayload{
		AccountIdentifier: op.Account,
		Bytes:             sigHash,
		SignatureType:     sigType,
	}, nil
}

// ExtractSignPayloads extracts the signing payloads from the given list of
// Rosetta operations realized as the specified transaction (likely generated
// from RosettaOpsToTx).
//
// If the transaction does not actually correspond to the list of operations,
// the results are undefined.
func ExtractSignPayloads(ops []*rtypes.Operation, tx *wire.MsgTx,
	chainParams *chaincfg.Params) ([]*rtypes.SigningPayload, error) {

	payloads := make([]*rtypes.SigningPayload, 0, len(tx.TxIn))

	var inIdx int
	for i, op := range ops {
		// Only debits (inputs) need to be signed.
		if OpType(op.Type) != OpTypeDebit {
			continue
		}

		payload, err := extractInputSignPayload(op, tx, inIdx, chainParams)
		if err != nil {
			return nil, ErrInvalidOp.Msgf("error generating "+
				"payload for op %d: %v", i, err)

		}
		inIdx++

		// Some inputs are valid but we don't know how to produce a
		// SigningPayload for, so we just skip those.
		if payload != nil {
			payloads = append(payloads, payload)
		}
	}

	return payloads, nil
}

func extractInputSigner(op *rtypes.Operation, tx *wire.MsgTx, idx int,
	chainParams *chaincfg.Params) (*rtypes.AccountIdentifier, error) {
	if idx >= len(tx.TxIn) {
		return nil, fmt.Errorf("trying to sign inexistent input %d", idx)
	}

	if op.Account == nil {
		return nil, fmt.Errorf("nil account")
	}
	if op.Account.Metadata == nil {
		return nil, fmt.Errorf("nil metadata")
	}

	// Figure out the original version and PkScript given the address.
	var version uint16
	if err := metadataUint16(op.Account.Metadata, "script_version", &version); err != nil {
		return nil, fmt.Errorf("unable to decode script_version: %v", err)
	}

	// Addresses with a version other than zero or encoded in raw format
	// ("0x" prefix) are not standardized, so we don't know how to sign
	// them. They might be signable by some other software though (e.g.
	// some other PSDT signer) so we don't error out on those cases but
	// simply signal that those inputs don't produce a signing payload.
	if version != 0 || strings.HasPrefix(op.Account.Address, "0x") {
		return nil, nil
	}

	// The signer is the original account address.
	return op.Account, nil
}

// ExtractTxSigners returns the list of signers from the given set of Rosetta
// operations realized as the given Decred transaction.
//
// If the operations do not correspond to the transaction, the results are
// undefined.
func ExtractTxSigners(ops []*rtypes.Operation, tx *wire.MsgTx,
	chainParams *chaincfg.Params) ([]*rtypes.AccountIdentifier, error) {

	signers := make([]*rtypes.AccountIdentifier, 0, len(tx.TxIn))

	var inIdx int
	for i, op := range ops {
		// Only debits (inputs) need to be signed.
		if OpType(op.Type) != OpTypeDebit {
			continue
		}

		signer, err := extractInputSigner(op, tx, inIdx, chainParams)
		if err != nil {
			return nil, ErrInvalidOp.Msgf("error extracting "+
				"input signer for op %d: %v", i, err)

		}
		inIdx++

		// Some inputs are valid but we don't know how to produce a
		// SigningPayload for, so we just skip those.
		if signer != nil {
			signers = append(signers, signer)
		}
	}

	return signers, nil

}

// CombineTxSigs copies the specified signatures to the transaction.
//
// The signature count and order MUST correspond to the inputs of the existing
// transaction, otherwise results are undefined and the transaction is unlikely
// to be valid.
func CombineTxSigs(sigs []*rtypes.Signature, tx *wire.MsgTx,
	chainParams *chaincfg.Params) error {

	if len(sigs) != len(tx.TxIn) {
		return ErrIncorrectSigCount
	}

	for i, sig := range sigs {
		var sigScript []byte

		switch sig.SignatureType {
		case rtypes.Ecdsa:
			// Make a copy of the sigScript so we can tack in the
			// sig hash type at the end.
			rs := make([]byte, len(sig.Bytes)+1)
			copy(rs[:], sig.Bytes)
			rs[len(rs)-1] = byte(sigHashType)

			// For ecdsa, we only support signing standard version
			// 0 P2PKH scripts, so build the appropriate signature
			// script.
			var b txscript.ScriptBuilder
			b.AddData(rs)
			b.AddData(sig.PublicKey.Bytes)
			var err error
			sigScript, err = b.Script()
			if err != nil {
				return err
			}
		default:
			return ErrUnsupportedSignatureType
		}

		tx.TxIn[i].SignatureScript = sigScript
	}

	return nil
}
