// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package types

import (
	"context"
	"errors"
	"fmt"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
)

// ErrorCode represents the numerical error Code returned on a Rosetta Error
// structure.
//
// It implements the standard Go error interface and the RosettaError interface
// and has full support for errors.Is and errors.As functions.
type ErrorCode int32

// RosettaError is an interface that defines types that can convert themselves
// into a Rosetta Error response structure.
type RosettaError interface {
	RError() *rtypes.Error
}

const (
	// NOTE: after adding a new type, also modify errCodeMsgs.

	ErrUnknown ErrorCode = iota
	ErrRequestCanceled
	ErrInvalidChainHash
	ErrInvalidArgument
	ErrBlockNotFound
	ErrTxNotFound
	ErrUnimplemented
	ErrInvalidTransaction
	ErrInvalidHexString
	ErrAlreadyHaveTx
	ErrTxAlreadyMined
	ErrProcessingTx
	ErrInvalidAccountIdAddr
	ErrBlockIndexAfterTip
	ErrUnsupportedCurveType
	ErrInvalidSecp256k1PubKey
	ErrNotCompressedSecp256k1Key
	ErrUnspecifiedAddressVersion
	ErrUnsupportedAddressVersion
	ErrUnsupportedAddressAlgo
	ErrUnkownOpType
	ErrInvalidOp
	ErrInvalidTxMetadata
	ErrIncorrectSigCount
	ErrUnsupportedSignatureType
	ErrSerializeSizeUnspecified
	ErrSerSizeNotNumber
	ErrIncorrectSigSize

	// This MUST be the last member.
	nbErrorCodes
)

// errorCodeMsgs are the default error message strings for the corresponding
// predefined error codes.
var errorCodeMsgs = map[ErrorCode]string{
	ErrUnknown:                   "unknown error",
	ErrRequestCanceled:           "request canceled",
	ErrInvalidChainHash:          "invalid chain hash",
	ErrInvalidArgument:           "invalid argument",
	ErrBlockNotFound:             "block not found",
	ErrTxNotFound:                "tx not found",
	ErrUnimplemented:             "unimplemented",
	ErrInvalidTransaction:        "invalid transaction",
	ErrInvalidHexString:          "invalid hex string",
	ErrAlreadyHaveTx:             "already have transaction",
	ErrTxAlreadyMined:            "tx already mined",
	ErrProcessingTx:              "error processing tx",
	ErrInvalidAccountIdAddr:      "invalid address in account identifier",
	ErrBlockIndexAfterTip:        "block index after current mainchain tip",
	ErrUnsupportedCurveType:      "unsupported curve type",
	ErrInvalidSecp256k1PubKey:    "invalid secp256k1 pubkey",
	ErrNotCompressedSecp256k1Key: "secp256k1 pubkey is not in compressed format",
	ErrUnspecifiedAddressVersion: "address version was not specified",
	ErrUnsupportedAddressVersion: "unsupported address version",
	ErrUnsupportedAddressAlgo:    "unsupported address algo",
	ErrUnkownOpType:              "unknown op type",
	ErrInvalidOp:                 "invalid op",
	ErrInvalidTxMetadata:         "invalid tx metadata",
	ErrIncorrectSigCount:         "incorrect signature count",
	ErrUnsupportedSignatureType:  "unsupported signature type",
	ErrSerializeSizeUnspecified:  "serialize_size was not specified",
	ErrSerSizeNotNumber:          "serialize_size is not a number",
	ErrIncorrectSigSize:          "incorrect signature size",
}

// Error returns the default error message for the given error code.
//
// NOTE: this is part of the standard Go error interface.
func (err ErrorCode) Error() string {
	return errorCodeMsgs[err]
}

// Is returns true if the target error is also an error code, an Error value
// with the same error code as err or a RosettaError with the same Code as this
// err.
func (err ErrorCode) Is(target error) bool {
	switch target := target.(type) {
	case ErrorCode:
		return target == err

	case Error:
		return target.code == err

	case RosettaError:
		rerr := target.RError()
		if rerr != nil {
			return ErrorCode(rerr.Code) == err
		}
	}

	return false
}

// AsError converts this ErrorCode into an Error with the default message.
func (err ErrorCode) AsError() Error {
	return Error{
		code: err,
	}
}

// Retriable returns a new Error with this ErrorCode and default message and
// the Retriable flag set.
func (err ErrorCode) Retriable() Error {
	return err.AsError().Retriable()
}

// Msg returns a new Error with this ErrorCode and the given message as error
// message.
func (err ErrorCode) Msg(m string) Error {
	return err.AsError().Msg(m)
}

// Msgf returns a new Error with this ErrorCode and the given formatted message
// as error message.
func (err ErrorCode) Msgf(format string, args ...interface{}) Error {
	return err.AsError().Msgf(format, args...)
}

// RError returns a Rosetta error with this error code and default error
// message.
//
// This is is part of the RosettaError interface.
func (err ErrorCode) RError() *rtypes.Error {
	return err.AsError().RError()
}

// Error is a fully specified error structure that is convertible to a Rosetta
// Error value and also fulfills Go's standard error interface.
type Error struct {
	code      ErrorCode
	msg       string
	retriable bool
}

// Error returns the stored error message.
//
// NOTE: this is part of Go's standard error interface.
func (err Error) Error() string {
	if err.msg != "" {
		return err.msg
	}
	return errorCodeMsgs[err.code]
}

// Is returns true if the target is either an ErrorCode with the same code as
// this error, an Error with the same code as this error or a
// RosettaError-implementing value with the same code as this error.
func (err Error) Is(target error) bool {
	switch target := target.(type) {
	case ErrorCode:
		return target == err.code

	case Error:
		return target.code == err.code

	case RosettaError:
		rerr := target.RError()
		if rerr != nil {
			return ErrorCode(rerr.Code) == err.code
		}
	}

	return false
}

// Unwrap returns the error code as the underlying error.
//
// This makes the Error function usable by the stdlib's As routine.
func (err Error) Unwrap() error {
	return err.code
}

// Retriable returns a new Error equal to err but with the retriable flag set
// to true.
func (err Error) Retriable() Error {
	return Error{
		code:      err.code,
		msg:       err.msg,
		retriable: true,
	}
}

// Msg returns a new Error equal to err but with the specified error message.
func (err Error) Msg(m string) Error {
	return Error{
		code:      err.code,
		msg:       m,
		retriable: err.retriable,
	}
}

// Msgf returns a new Error equal to err but with the specified formatted
// string as error message.
func (err Error) Msgf(format string, args ...interface{}) Error {
	return Error{
		code:      err.code,
		msg:       fmt.Sprintf(format, args...),
		retriable: err.retriable,
	}
}

// RError returns the error as a Rosetta Error value.
//
// NOTE: this is part of the RosettaError interface.
func (err Error) RError() *rtypes.Error {
	var details map[string]interface{}
	if err.msg != "" {
		details = make(map[string]interface{}, 1)
		details["error"] = err.msg
	}

	return &rtypes.Error{
		Code:      int32(err.code),
		Message:   errorCodeMsgs[err.code],
		Retriable: err.retriable,
		Details:   details,
	}
}

// AllErrors returns all known error codes in a format suitable for inclusion
// in an Allow response.
func AllErrors() []*rtypes.Error {
	errs := make([]*rtypes.Error, 0, nbErrorCodes)
	for i := ErrorCode(0); i < nbErrorCodes; i++ {
		errs = append(errs, i.RError())
	}
	return errs
}

// RError is a helper function that can convert any standard Go error into a
// Rosetta Error value.
//
// ErrorCode and Error values from this package as well as values that
// implement the RosettaError interface are converted directly. Errors that can
// be unwrapped into an Error or ErrorCode value are also handled
// appropriately.
//
// A special case is also made for errors from the context package. The
// context.Canceled and context.DeadlineExceeded errors are converted to
// ErrRequestCanceled.
//
// Other errors are converted to a Rosetta Error with error type ErrUnknown.
func RError(err error) *rtypes.Error {
	switch err := err.(type) {
	case ErrorCode:
		return err.RError()

	case Error:
		return err.RError()

	case RosettaError:
		if err != nil {
			return err.RError()
		}
	}

	switch {
	case err == context.Canceled || err == context.DeadlineExceeded:
		return ErrRequestCanceled.RError()
	}

	var e Error
	if errors.As(err, &e) {
		return e.RError()
	}

	var ec ErrorCode
	if errors.As(err, &ec) {
		return ec.RError()
	}

	return ErrUnknown.Msg(err.Error()).RError()
}
