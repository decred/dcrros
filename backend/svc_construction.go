// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"decred.org/dcrros/types"
	rserver "github.com/coinbase/rosetta-sdk-go/server"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrjson/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/wire"
)

const (
	// p2pkhSigScriptSerSize is the maximum serialization size for a standard v0
	// signature script field that redeems a P2PKH output.
	//
	// It is calculated as OP_DATA_73 + [72 byte sig + sig hash type] +
	// OP_DATA_33 + [33 byte pubkey]
	//
	// Total: 108 bytes.
	p2pkhSigScriptSerSize = 108

	// networkFee is the widely used network relay fee in Atoms/kB.
	networkFee = int64(1e4)
)

var _ rserver.ConstructionAPIServicer = (*Server)(nil)

// ConstructionDerive derives a new Decred address from a public key. Several
// different formats for the Decred address are available, according to the
// metadata included in the request.
//
// The following metadata are *REQUIRED*:
//
//   - version (js number): Script Version to generate the address for. Only
//   version 0 is currently supported.
//
// The following metadata are *OPTIONAL*:
//
//   - algo (js string): Either "ecdsa" or "schnorr" for secp256k1 curve keys.
//   If unspecified, version 0 scripts generate an ecdsa key.
//
// NOTE: This is part of the ConstructionAPIServicer interface.
func (s *Server) ConstructionDerive(ctx context.Context,
	req *rtypes.ConstructionDeriveRequest) (*rtypes.ConstructionDeriveResponse, *rtypes.Error) {

	var addr dcrutil.Address

	// Future proof this api endpoint by requiring a version metadata. This
	// ensures that once we move to new address formats, an old dcrros will
	// fail requests from clients using the new format instead of returning
	// an address in an unexpected format.
	version, ok := req.Metadata["script_version"]
	if !ok {
		return nil, types.ErrUnspecifiedAddressVersion.RError()
	}
	var wantVers interface{}
	switch version.(type) {
	case uint16:
		wantVers = uint16(0)
	case float64:
		wantVers = float64(0)
	case int:
		wantVers = int(0)
	}
	if version != wantVers {
		return nil, types.ErrUnsupportedAddressVersion.RError()
	}

	switch req.PublicKey.CurveType {
	case rtypes.Secp256k1:
		if len(req.PublicKey.Bytes) != 33 {
			return nil, types.ErrInvalidSecp256k1PubKey.RError()
		}

		// Ensure the serialized pubkey is in compressed format. We
		// otherwise assume the caller has specified a valid public
		// key.
		switch req.PublicKey.Bytes[0] {
		case 0x02, 0x03:
		default:
			return nil, types.ErrNotCompressedSecp256k1Key.RError()
		}

		var algo dcrec.SignatureType
		switch req.Metadata["algo"] {
		case "schnorr":
			algo = dcrec.STSchnorrSecp256k1
		case "ecdsa", nil:
			algo = dcrec.STEcdsaSecp256k1
		default:
			return nil, types.ErrUnsupportedAddressAlgo.RError()
		}

		// Hash the public key and create the appropriate address.
		pkHash := dcrutil.Hash160(req.PublicKey.Bytes)
		var err error
		addr, err = dcrutil.NewAddressPubKeyHash(pkHash, s.chainParams, algo)
		if err != nil {
			// Shouldn't really happen on the current version of
			// the dcrutil.API since the arguments are correct.
			return nil, types.ErrInvalidSecp256k1PubKey.
				Msgf("NewAddrPubKeyHash error: %v", err).RError()
		}
	default:
		return nil, types.ErrUnsupportedCurveType.RError()
	}

	return &rtypes.ConstructionDeriveResponse{
		AccountIdentifier: &rtypes.AccountIdentifier{
			Address: addr.Address(),
			Metadata: map[string]interface{}{
				"script_version": version,
			},
		},
	}, nil
}

// ConstructionPreprocess returns the options that need to be fetched during a
// ConstructionMetadata call.
//
// Decred doesn't currently need any metadata.
//
// NOTE: This is part of the ConstructionAPIServicer interface.
func (s *Server) ConstructionPreprocess(ctx context.Context,
	req *rtypes.ConstructionPreprocessRequest) (*rtypes.ConstructionPreprocessResponse, *rtypes.Error) {

	// Calculate the required fee for this tx.
	tx, err := types.RosettaOpsToTx(req.Metadata, req.Operations, s.chainParams)
	if err != nil {
		return nil, types.RError(err)
	}

	// Figure out how many inputs are not signed and assume they are
	// standard P2PKH.
	var nbInputs int
	for _, in := range tx.TxIn {
		if len(in.SignatureScript) == 0 {
			nbInputs++
		}
	}
	serSize := nbInputs*p2pkhSigScriptSerSize + tx.SerializeSize()

	return &rtypes.ConstructionPreprocessResponse{
		Options: map[string]interface{}{
			"serialize_size": serSize,
		},
	}, nil
}

// ConstructionMetadata returns metadata required to build a valid Decred
// transaction.
//
// NOTE: This is part of the ConstructionAPIServicer interface.
func (s *Server) ConstructionMetadata(ctx context.Context,
	req *rtypes.ConstructionMetadataRequest) (*rtypes.ConstructionMetadataResponse, *rtypes.Error) {

	serSizeRaw, ok := req.Options["serialize_size"]
	if !ok {
		return nil, types.RError(types.ErrSerializeSizeUnspecified)
	}
	var serSize int64
	switch serSizeRaw := serSizeRaw.(type) {
	case float64:
		serSize = int64(serSizeRaw)
	case int:
		serSize = int64(serSizeRaw)
	default:
		return nil, types.RError(types.ErrSerSizeNotNumber)
	}

	fee := dcrutil.Amount(serSize * networkFee / 1000)

	return &rtypes.ConstructionMetadataResponse{
		Metadata:     map[string]interface{}{},
		SuggestedFee: []*rtypes.Amount{types.DcrAmountToRosetta(fee)},
	}, nil
}

// constructionTx stores transaction info between calls to the Construction API
// endpoints.
//
// Due to the design of the Construction set of Rosetta APIs, additional data
// besides the raw transaction needs to be sent between a few calls. This
// structure stores all such data and can serialize and deserialize using a
// custom binary format.
//
// The underlying binary format is as follow:
//
//     version byte | binary tx | nb of prevouts | prevouts
//
// - [version byte]: 1 byte version
// - [binary tx]: wire tx, serialized in the standard way
// - [nb of prevouts]: varint with number of previous outpoits
// - [prevouts]: variable list of prevouts. Each one is serialized as follows:
//
//     amount | version | pk script len | pkscript
//
// - [amount]: 8 byte uint64 amount
// - [version]: 2 byte uint16 version
// - [pk script len]: varint with size of pkscript slice
// - [pkscript]: slice of bytes
//
// Only version 0x01 is currently supported. The number of prevouts must match
// the number of tx inputs both when serializing and deserializing.
type constructionTx struct {
	tx            *wire.MsgTx
	prevOutPoints map[wire.OutPoint]*types.PrevOutput
}

var (
	errWrongNbPrevOuts     = errors.New("wrong nb of tx inputs and prevouts")
	errInPrevOutNotFound   = errors.New("could not find prevout for input")
	errInvalidVersionRead  = errors.New("invalidt read of ctrtx version")
	errInvalidCtrtxVersion = errors.New("unrecognized ctrtx version")
)

// serialize stores the constructionTx data into a slice of bytes and then
// encodes that as an hex string.
func (ctrtx *constructionTx) serialize() (string, error) {
	// Check some preconditions.
	//
	// Len of input and prevouts must match.
	if len(ctrtx.tx.TxIn) != len(ctrtx.prevOutPoints) {
		return "", fmt.Errorf("%w: %d inputs, %d prevOuts",
			errWrongNbPrevOuts, len(ctrtx.tx.TxIn),
			len(ctrtx.prevOutPoints))
	}

	// Every txin prevout should be specified in the map.
	for i := 0; i < len(ctrtx.tx.TxIn); i++ {
		txInPrevOut := ctrtx.tx.TxIn[i].PreviousOutPoint
		if _, ok := ctrtx.prevOutPoints[txInPrevOut]; !ok {
			return "", fmt.Errorf("%w: input %d prevOut %s ",
				errInPrevOutNotFound, i, txInPrevOut)
		}
	}

	// Helpful vars and functions.
	pver := uint32(0)
	endian := binary.LittleEndian
	var b *bytes.Buffer
	writeUint64 := func(i uint64) error {
		var aux [8]byte
		endian.PutUint64(aux[:], i)
		n, err := b.Write(aux[:])
		if n != 8 {
			return fmt.Errorf("short uint64 write")
		}
		return err
	}
	writeUint16 := func(i uint16) error {
		var aux [2]byte
		endian.PutUint16(aux[:], i)
		n, err := b.Write(aux[:])
		if n != 2 {
			return fmt.Errorf("short uint16 write")
		}
		return err
	}

	// Calculate the serialize size in order to allocate a single byte
	// slice.
	lenPrevOuts := len(ctrtx.prevOutPoints)
	size := 1 + // Version byte
		ctrtx.tx.SerializeSize() + // Tx size
		wire.VarIntSerializeSize(uint64(lenPrevOuts)) // Nb of prevouts
	for _, prev := range ctrtx.prevOutPoints {
		lenPks := len(prev.PkScript)
		size += 8 + 2 + // Value+Version
			wire.VarIntSerializeSize(uint64(lenPks)) + lenPks // PkScript
	}

	// Write the constructionTx version byte.
	b = bytes.NewBuffer(make([]byte, 0, size))
	if _, err := b.Write([]byte{0x01}); err != nil {
		return "", err
	}

	// Write the original tx.
	if err := ctrtx.tx.BtcEncode(b, pver); err != nil {
		return "", err
	}

	// Write the nb of prevous.
	if err := wire.WriteVarInt(b, pver, uint64(lenPrevOuts)); err != nil {
		return "", err
	}

	// Write each prevout in the same sequence as the transaction inputs.
	for _, in := range ctrtx.tx.TxIn {
		prev := ctrtx.prevOutPoints[in.PreviousOutPoint]
		lenPks := len(prev.PkScript)
		if err := writeUint64(uint64(prev.Amount)); err != nil {
			return "", err
		}
		if err := writeUint16(prev.Version); err != nil {
			return "", err
		}
		if err := wire.WriteVarInt(b, pver, uint64(lenPks)); err != nil {
			return "", err
		}
		if _, err := b.Write(prev.PkScript); err != nil {
			return "", err
		}
	}

	return hex.EncodeToString(b.Bytes()), nil
}

// deserialize parses the string into the costructionTx value.
func (ctrtx *constructionTx) deserialize(s string) error {
	// Helpful vars and functions.
	pver := uint32(0)
	endian := binary.LittleEndian
	var b *bytes.Buffer

	readAmount := func() (dcrutil.Amount, error) {
		var aux [8]byte
		n, err := b.Read(aux[:])
		if n != 8 {
			return 0, io.EOF
		}
		return dcrutil.Amount(endian.Uint64(aux[:])), err
	}
	readUint16 := func() (uint16, error) {
		var aux [2]byte
		n, err := b.Read(aux[:])
		if n != 2 {
			return 0, io.EOF
		}
		return endian.Uint16(aux[:]), err
	}

	// Decode from hex into a byte buffer.
	bts, err := hex.DecodeString(s)
	if err != nil {
		return types.ErrInvalidHexString
	}
	b = bytes.NewBuffer(bts)

	// Deserialize and verify encoding version.
	var version [1]byte
	if _, err := b.Read(version[:]); err != nil {
		return fmt.Errorf("%w: %v", errInvalidVersionRead, err)
	}
	if version[0] != 0x01 {
		return fmt.Errorf("%w: %d", errInvalidCtrtxVersion, version[0])
	}

	// Deserialize tx.
	ctrtx.tx = new(wire.MsgTx)
	if err := ctrtx.tx.BtcDecode(b, pver); err != nil {
		return types.ErrInvalidTransaction.Msg(err.Error())
	}

	// Deserialize prevouts.
	var nbPrevOuts uint64
	if nbPrevOuts, err = wire.ReadVarInt(b, pver); err != nil {
		return err
	}
	if nbPrevOuts != uint64(len(ctrtx.tx.TxIn)) {
		return fmt.Errorf("%w: %d inputs, %d prevOuts",
			errWrongNbPrevOuts, len(ctrtx.tx.TxIn),
			nbPrevOuts)
	}
	ctrtx.prevOutPoints = make(map[wire.OutPoint]*types.PrevOutput, nbPrevOuts)
	for i := 0; i < int(nbPrevOuts); i++ {
		outp := ctrtx.tx.TxIn[i].PreviousOutPoint
		prevOut := &types.PrevOutput{}
		if prevOut.Amount, err = readAmount(); err != nil {
			return err
		}
		if prevOut.Version, err = readUint16(); err != nil {
			return err
		}
		var lenPks uint64
		if lenPks, err = wire.ReadVarInt(b, pver); err != nil {
			return err
		}
		prevOut.PkScript = make([]byte, lenPks)
		var n int
		if n, err = b.Read(prevOut.PkScript); err != nil {
			return err
		}
		if uint64(n) != lenPks {
			return io.EOF
		}

		ctrtx.prevOutPoints[outp] = prevOut
	}

	return nil
}

// ConstructionPayloads returns the payloads that need signing so that the
// specified set of operations can be published to the network as a valid
// transaction.
//
// This function only returns payloads for the types of addresses it
// understands (currently only version 0, P2PKH ECDSA addresses). It does *NOT*
// error out on other valid (but otherwise unknown) inputs that need signing so
// that other software (such as other PSDT signers) may be used for the missing
// inputs.
//
// NOTE: This is part of the ConstructionAPIServicer interface.
func (s *Server) ConstructionPayloads(ctx context.Context,
	req *rtypes.ConstructionPayloadsRequest) (*rtypes.ConstructionPayloadsResponse, *rtypes.Error) {
	tx, err := types.RosettaOpsToTx(req.Metadata, req.Operations, s.chainParams)
	if err != nil {
		return nil, types.RError(err)
	}

	payloads, err := types.ExtractSignPayloads(req.Operations, tx, s.chainParams)
	if err != nil {
		return nil, types.RError(err)
	}

	// Convert into and serialize a construction-api tx with additional
	// data.
	ctrtx := new(constructionTx)
	ctrtx.tx = tx
	ctrtx.prevOutPoints, err = types.ExtractPrevOutputsFromOps(req.Operations, s.chainParams)
	if err != nil {
		return nil, types.RError(err)
	}
	unsignedTx, err := ctrtx.serialize()
	if err != nil {
		return nil, types.RError(err)
	}

	return &rtypes.ConstructionPayloadsResponse{
		UnsignedTransaction: unsignedTx,
		Payloads:            payloads,
	}, nil
}

// ConstructionParse parses a serialized Decred transaction (either signed or
// unsigned) into a set of Rosetta operations.
//
// Note that all of the input's previous outpoints MUST have already been mined
// or published to the underlying node's mempool before this is called,
// otherwise this function fails. In other words, it's currently not possible
// to build a sequence of unconfirmed and unpublished transactions.
//
// NOTE: This is part of the ConstructionAPIServicer interface.
func (s *Server) ConstructionParse(ctx context.Context,
	req *rtypes.ConstructionParseRequest) (*rtypes.ConstructionParseResponse, *rtypes.Error) {

	ctrtx := new(constructionTx)
	if err := ctrtx.deserialize(req.Transaction); err != nil {
		return nil, types.RError(err)
	}

	// We use the the MempoolTxToRosetta function to do the conversion
	// since it's likely this tx will be broadcast in a moment.
	fetchPrevOuts := s.makePrevOutsFetcher(ctx, ctrtx.prevOutPoints)
	rtx, err := types.MempoolTxToRosetta(ctrtx.tx, fetchPrevOuts, s.chainParams)
	if err != nil {
		return nil, types.RError(err)
	}

	signers, err := types.ExtractTxSigners(rtx.Operations, ctrtx.tx, s.chainParams)
	if err != nil {
		return nil, types.RError(err)
	}

	// The returned operations on this endpoint must have an empty status,
	// so clear them out here.
	for _, op := range rtx.Operations {
		op.Status = nil
	}

	return &rtypes.ConstructionParseResponse{
		Operations:               rtx.Operations,
		Metadata:                 rtx.Metadata,
		AccountIdentifierSigners: signers,
	}, nil
}

// ConstructionCombine combines signatures to an unsigned transaction creating
// a fully signed transaction.
//
// Currently requests MUST contain all signatures in the exact same sequence as
// the inputs in the transaction, otherwise this fails.
//
// NOTE: This is part of the ConstructionAPIServicer interface.
func (s *Server) ConstructionCombine(ctx context.Context,
	req *rtypes.ConstructionCombineRequest) (*rtypes.ConstructionCombineResponse, *rtypes.Error) {

	ctrtx := new(constructionTx)
	if err := ctrtx.deserialize(req.UnsignedTransaction); err != nil {
		return nil, types.RError(err)
	}

	if err := types.CombineTxSigs(req.Signatures, ctrtx.tx, s.chainParams); err != nil {
		return nil, types.RError(err)
	}

	signedTx, err := ctrtx.serialize()
	if err != nil {
		return nil, types.RError(err)
	}
	return &rtypes.ConstructionCombineResponse{
		SignedTransaction: signedTx,
	}, nil
}

// Construction hash returns the hash of the given signed transaction.
//
// NOTE: This is part of the ConstructionAPIServicer interface.
func (s *Server) ConstructionHash(ctx context.Context,
	req *rtypes.ConstructionHashRequest) (*rtypes.TransactionIdentifierResponse, *rtypes.Error) {

	ctrtx := new(constructionTx)
	if err := ctrtx.deserialize(req.SignedTransaction); err != nil {
		return nil, types.RError(err)
	}

	return &rtypes.TransactionIdentifierResponse{
		TransactionIdentifier: &rtypes.TransactionIdentifier{
			Hash: ctrtx.tx.TxHash().String(),
		},
	}, nil
}

// ConstructionSubmit submits the provided transaction to the Decred network.
//
// NOTE: This is part of the ConstructionAPIServicer interface.
func (s *Server) ConstructionSubmit(ctx context.Context,
	req *rtypes.ConstructionSubmitRequest) (*rtypes.TransactionIdentifierResponse, *rtypes.Error) {

	ctrtx := new(constructionTx)
	if err := ctrtx.deserialize(req.SignedTransaction); err != nil {
		return nil, types.RError(err)
	}

	txh, err := s.c.SendRawTransaction(ctx, ctrtx.tx, false)
	if err != nil {
		// Handle some special cases from dcrd into different error
		// codes.
		if rpcerr, ok := err.(*dcrjson.RPCError); ok {
			if rpcerr.Code == dcrjson.ErrRPCDuplicateTx {
				return nil, types.ErrAlreadyHaveTx.Msg(err.Error()).RError()

			} else if rpcerr.Code == dcrjson.ErrRPCMisc && strings.Contains(rpcerr.Error(), "transaction already exists") {
				return nil, types.ErrTxAlreadyMined.Msg(err.Error()).RError()
			} else if rpcerr.Code == dcrjson.ErrRPCMisc {
				// Generic rule error.
				return nil, types.ErrProcessingTx.Msg(err.Error()).RError()
			}
		}

		return nil, types.RError(err)
	}

	return &rtypes.TransactionIdentifierResponse{
		TransactionIdentifier: &rtypes.TransactionIdentifier{
			Hash: txh.String(),
		},
	}, nil
}
