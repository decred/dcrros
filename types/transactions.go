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
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v3/ecdsa"
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

var (
	errIncorrectOutPointFormat = errors.New("string does not have outpoint format '<hex>:<index>''")
	errShortOutPointHex        = errors.New("hex part of outpoint does not encode 32 bytes")
	errInvalidOutPointHex      = errors.New("invalid hash at start of outpoint string")
	errInvalidOutPointInt      = errors.New("invalid int at end of outpoint string")
)

// decodeOutPoint deserializes the given string into an outpoint.
func decodeOutPoint(s string) (wire.OutPoint, error) {
	split := strings.Split(s, ":")
	var out wire.OutPoint
	if len(split) != 2 {
		return out, errIncorrectOutPointFormat
	}

	if len(split[0]) != 64 {
		return out, errShortOutPointHex
	}

	if err := chainhash.Decode(&out.Hash, split[0]); err != nil {
		return out, errInvalidOutPointHex
	}

	idx, err := strconv.ParseUint(split[1], 10, 32)
	if err != nil {
		return out, errInvalidOutPointInt
	}

	out.Index = uint32(idx)

	return out, nil
}

var (
	errNilCoinChange   = errors.New("coin_change must not be nil")
	errNilCoinChangeId = errors.New("coin_change.coin_identifier must not be nil")
)

// coinChangeToOutPoint decodes the given coin change into a wire OutPoint.
func coinChangeToOutPoint(cc *rtypes.CoinChange) (wire.OutPoint, error) {
	if cc == nil {
		return wire.OutPoint{}, errNilCoinChange
	}
	if cc.CoinIdentifier == nil {
		return wire.OutPoint{}, errNilCoinChangeId
	}
	outp, err := decodeOutPoint(cc.CoinIdentifier.Identifier)
	if err != nil {
		return outp, err
	}
	return outp, nil
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

var (
	errWrongDebitCoinAction = errors.New("debit operation needs a coin_spent coin_action")
)

func rosettaOpToTx(op *rtypes.Operation, tx *wire.MsgTx, chainParams *chaincfg.Params) error {
	m := op.Metadata

	opAmt, err := RosettaToDcrAmount(op.Amount)
	if err != nil {
		return err
	}

	switch OpType(op.Type) {
	case OpTypeDebit:
		// If the amount is negative, reverse it.
		if opAmt < 0 {
			opAmt = -opAmt
		}

		// Debits require an originating coins_spent CoinChange so we can fill
		// PrevousOutPoint.
		prevOut, err := coinChangeToOutPoint(op.CoinChange)
		if err != nil {
			return fmt.Errorf("invalid coin_change in debit op: %w", err)
		}
		if op.CoinChange.CoinAction != rtypes.CoinSpent {
			return errWrongDebitCoinAction
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
			return ErrNilAccount
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
	tx := wire.NewMsgTx()

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

// prevOutputFromDebitOp re-calculates the PrevOutput structure from a given
// debit operation.
func prevOutputFromDebitOp(op *rtypes.Operation, chainParams *chaincfg.Params) (
	*PrevOutput, dcrutil.Address, error) {

	if op.Type != string(OpTypeDebit) {
		return nil, nil, fmt.Errorf("op must be a debit to extract prevOutput")
	}
	if op.Account == nil {
		return nil, nil, fmt.Errorf("account cannot be nil")
	}
	if op.Account.Metadata == nil {
		return nil, nil, fmt.Errorf("account.metadata cannot be nil")
	}

	// Figure out the original version and PkScript given the address.
	var version uint16
	if err := metadataUint16(op.Account.Metadata, "script_version", &version); err != nil {
		return nil, nil, fmt.Errorf("unable to decode script_version: %v", err)
	}

	// Addresses with a version other than zero or encoded in raw format
	// ("0x" prefix) are not standardized, so we don't know how to generate
	// a PkScript for them.
	if version != 0 || strings.HasPrefix(op.Account.Address, "0x") {
		return nil, nil, nil
	}

	addr, err := dcrutil.DecodeAddress(op.Account.Address, chainParams)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to decode address: %v", err)
	}

	// Generate the corresponding pkscript and signature hash.
	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to generate pkscript: %v", err)
	}

	amount, err := RosettaToDcrAmount(op.Amount)
	if err != nil {
		return nil, nil, err
	}
	if amount < 0 {
		amount = -amount
	}

	return &PrevOutput{
		PkScript: pkScript,
		Version:  version,
		Amount:   amount,
	}, addr, nil
}

func extractInputSignPayload(op *rtypes.Operation, tx *wire.MsgTx, idx int,
	chainParams *chaincfg.Params) (*rtypes.SigningPayload, error) {

	if idx >= len(tx.TxIn) {
		return nil, fmt.Errorf("trying to sign inexistent input %d", idx)
	}

	prevOutput, addr, err := prevOutputFromDebitOp(op, chainParams)
	if err != nil {
		return nil, err
	}

	// Addresses with a version other than zero or encoded in raw format
	// ("0x" prefix) are not standardized, so we don't know how to sign
	// them. They might be signable by some other software though (e.g.
	// some other PSDT signer) so we don't error out on those cases but
	// simply signal that those inputs don't produce a signing payload.
	if addr == nil {
		return nil, nil
	}

	// Determine the signature type based on the type of address. Note we
	// only support P2PKH for ecdsa (*not* P2PK).
	var sigType rtypes.SignatureType
	switch addr.(type) {
	case *dcrutil.AddressPubKeyHash:
		sigType = rtypes.Ecdsa
	default:
		// Other unknown address types are not an error, they just
		// don't produce a signing payload we know of.
		return nil, nil
	}

	sigHash, err := txscript.CalcSignatureHash(prevOutput.PkScript,
		sigHashType, tx, idx, nil)
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

	var signers []*rtypes.AccountIdentifier

	var inIdx int
	for i, op := range ops {
		// Only debits (inputs) need to be signed.
		if OpType(op.Type) != OpTypeDebit {
			continue
		}

		if inIdx >= len(tx.TxIn) {
			return nil, fmt.Errorf("debit without corresponding input %d", inIdx)
		}

		// Only extract signer for signed inputs.
		if len(tx.TxIn[inIdx].SignatureScript) > 0 {
			signer, err := extractInputSigner(op, tx, inIdx, chainParams)
			if err != nil {
				return nil, ErrInvalidOp.Msgf("error extracting "+
					"input signer for op %d: %v", i, err)
			}

			// Some inputs are valid but we don't know how to
			// produce a SigningPayload for, so we just skip those.
			if signer != nil {
				signers = append(signers, signer)
			}
		}

		inIdx++
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

		if sig.PublicKey.CurveType != rtypes.Secp256k1 {
			return ErrUnsupportedCurveType
		}

		pubkey, err := secp256k1.ParsePubKey(sig.PublicKey.Bytes)
		if err != nil {
			return ErrInvalidPubKey.Msg(err.Error())
		}

		switch sig.SignatureType {
		case rtypes.Ecdsa:
			if len(sig.Bytes) != 64 {
				return ErrIncorrectSigSize.Msg("Ecdsa signatures need to be 64 bytes long")
			}

			// ECDSA mode returns a sig in compact format (32-byte R + 32-byte S), so
			// decode into an ecdsa.Signature type and re-serialize as a DER
			// signature.
			var r, s secp256k1.ModNScalar
			r.SetByteSlice(sig.Bytes[:32])
			s.SetByteSlice(sig.Bytes[32:])
			ecdsaSig := ecdsa.NewSignature(&r, &s)

			// Verify the sig before we proceed.
			if !ecdsaSig.Verify(sig.SigningPayload.Bytes, pubkey) {
				return ErrInvalidSig.Msgf("sig at index %d failed verification", i)
			}

			// Re-serialize in DER format.
			derSig := ecdsaSig.Serialize()

			// Add the sighash type after the sig.
			derSig = append(derSig, byte(sigHashType))

			// For ecdsa, we only support signing standard version
			// 0 P2PKH scripts, so build the appropriate signature
			// script.
			var b txscript.ScriptBuilder
			b.AddData(derSig)
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

// ExtractPrevOutputsFromOps iterates over all debits of the given list of
// operations and generates a map OutPoint => PrevOutput that can be used to
// later identify the previous outputs of the debits.
func ExtractPrevOutputsFromOps(ops []*rtypes.Operation, chainParams *chaincfg.Params) (
	map[wire.OutPoint]*PrevOutput, error) {

	res := make(map[wire.OutPoint]*PrevOutput)

	for i, op := range ops {
		if op.Type != string(OpTypeDebit) {
			continue
		}

		prevOutput, _, err := prevOutputFromDebitOp(op, chainParams)
		if err != nil {
			return nil, fmt.Errorf("unable to extract prevOutput "+
				"from debit %d: %v", i, err)
		}

		// Ignore unknown script versions and addresses.
		if prevOutput == nil {
			continue
		}

		// Debits require an originating coins_spent CoinChange so we
		// can fill PreviousOutPoint.
		outPoint, err := coinChangeToOutPoint(op.CoinChange)
		if err != nil {
			return nil, fmt.Errorf("debit op %d has invalid "+
				"coin_change: %v", i, err)
		}

		if op.CoinChange.CoinAction != rtypes.CoinSpent {
			return nil, fmt.Errorf("debit op %d needs a coin_spent "+
				"coin_action", i)
		}

		res[outPoint] = prevOutput
	}

	return res, nil
}
