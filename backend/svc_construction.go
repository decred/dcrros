// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"
	"encoding/hex"
	"strings"

	"decred.org/dcrros/types"
	rserver "github.com/coinbase/rosetta-sdk-go/server"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrjson/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/wire"
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
	if version != float64(0) {
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
func (s *Server) ConstructionPreprocess(context.Context,
	*rtypes.ConstructionPreprocessRequest) (*rtypes.ConstructionPreprocessResponse, *rtypes.Error) {

	return &rtypes.ConstructionPreprocessResponse{}, nil
}

// ConstructionMetadata returns metadata required to build a valid Decred
// transaction.
//
// Decred doesn't currently need any metadata.
//
// NOTE: This is part of the ConstructionAPIServicer interface.
func (s *Server) ConstructionMetadata(context.Context,
	*rtypes.ConstructionMetadataRequest) (*rtypes.ConstructionMetadataResponse, *rtypes.Error) {

	return &rtypes.ConstructionMetadataResponse{
		Metadata: map[string]interface{}{},
	}, nil
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

	bts, err := tx.Bytes()
	if err != nil {
		return nil, types.RError(err)
	}

	return &rtypes.ConstructionPayloadsResponse{
		UnsignedTransaction: hex.EncodeToString(bts),
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

	txBytes, err := hex.DecodeString(req.Transaction)
	if err != nil {
		return nil, types.ErrInvalidHexString.RError()
	}

	tx := wire.NewMsgTx()
	if err := tx.FromBytes(txBytes); err != nil {
		return nil, types.ErrInvalidTransaction.RError()
	}

	// We use the the MempoolTxToRosetta function to do the conversion
	// since it's likely this tx will be broadcast in a moment.
	fetchInputs := s.makeInputsFetcher(ctx, nil)
	rtx, err := types.MempoolTxToRosetta(tx, fetchInputs, s.chainParams)
	if err != nil {
		return nil, types.RError(err)
	}

	signers, err := types.ExtractTxSigners(rtx.Operations, tx, s.chainParams)
	if err != nil {
		return nil, types.RError(err)
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

	txBytes, err := hex.DecodeString(req.UnsignedTransaction)
	if err != nil {
		return nil, types.ErrInvalidHexString.RError()
	}

	tx := &wire.MsgTx{}
	if err := tx.FromBytes(txBytes); err != nil {
		return nil, types.ErrInvalidTransaction.RError()
	}

	if err := types.CombineTxSigs(req.Signatures, tx, s.chainParams); err != nil {
		return nil, types.RError(err)
	}

	bts, err := tx.Bytes()
	if err != nil {
		return nil, types.RError(err)
	}
	return &rtypes.ConstructionCombineResponse{
		SignedTransaction: hex.EncodeToString(bts),
	}, nil
}

// Construction hash returns the hash of the given signed transaction.
//
// NOTE: This is part of the ConstructionAPIServicer interface.
func (s *Server) ConstructionHash(ctx context.Context,
	req *rtypes.ConstructionHashRequest) (*rtypes.TransactionIdentifierResponse, *rtypes.Error) {

	txBytes, err := hex.DecodeString(req.SignedTransaction)
	if err != nil {
		return nil, types.ErrInvalidHexString.RError()
	}

	tx := &wire.MsgTx{}
	if err := tx.FromBytes(txBytes); err != nil {
		return nil, types.ErrInvalidTransaction.RError()
	}

	return &rtypes.TransactionIdentifierResponse{
		TransactionIdentifier: &rtypes.TransactionIdentifier{
			Hash: tx.TxHash().String(),
		},
	}, nil
}

// ConstructionSubmit submits the provided transaction to the Decred network.
//
// NOTE: This is part of the ConstructionAPIServicer interface.
func (s *Server) ConstructionSubmit(ctx context.Context,
	req *rtypes.ConstructionSubmitRequest) (*rtypes.TransactionIdentifierResponse, *rtypes.Error) {

	txBytes, err := hex.DecodeString(req.SignedTransaction)
	if err != nil {
		return nil, types.ErrInvalidHexString.RError()
	}

	tx := new(wire.MsgTx)
	err = tx.FromBytes(txBytes)
	if err != nil {
		return nil, types.ErrInvalidTransaction.RError()
	}

	txh, err := s.c.SendRawTransaction(ctx, tx, false)
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

			return nil, types.RError(err)
		}
	}

	return &rtypes.TransactionIdentifierResponse{
		TransactionIdentifier: &rtypes.TransactionIdentifier{
			Hash: txh.String(),
		},
	}, nil
}
