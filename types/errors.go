package types

import (
	"context"
	"errors"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/dcrjson/v3"
)

type ErrorCode int32

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

	// This MUST be the last member.
	nbErrorCodes
)

var errorCodeMsgs = map[ErrorCode]string{
	ErrUnknown:              "unknown error",
	ErrRequestCanceled:      "request canceled",
	ErrInvalidChainHash:     "invalid chain hash",
	ErrInvalidArgument:      "invalid argument",
	ErrBlockNotFound:        "block not found",
	ErrTxNotFound:           "tx not found",
	ErrUnimplemented:        "unimplemented",
	ErrInvalidTransaction:   "invalid transaction",
	ErrInvalidHexString:     "invalid hex string",
	ErrAlreadyHaveTx:        "already have transaction",
	ErrTxAlreadyMined:       "tx already mined",
	ErrProcessingTx:         "error processing tx",
	ErrInvalidAccountIdAddr: "invalid address in account identifier",
	ErrBlockIndexAfterTip:   "block index after current mainchain tip",
}

func (err ErrorCode) Error() string {
	return errorCodeMsgs[err]
}

func (err ErrorCode) Is(target error) bool {
	if target, ok := target.(ErrorCode); ok {
		return target == err
	}

	return false
}

func (err ErrorCode) AsError() Error {
	return Error{
		code: err,
		msg:  errorCodeMsgs[err],
	}
}

func (err ErrorCode) Retriable() Error {
	return err.AsError().Retriable()
}

func (err ErrorCode) Msg(m string) Error {
	return err.AsError().Msg(m)
}

func (err ErrorCode) RError() *rtypes.Error {
	return err.AsError().RError()
}

type Error struct {
	code      ErrorCode
	msg       string
	retriable bool
}

func (err Error) Error() string {
	return err.msg
}

func (err Error) Is(target error) bool {
	switch target := target.(type) {
	case Error:
		return target.code == err.code
	case ErrorCode:
		return target == err.code
	}
	return false
}

func (err Error) Unwrap() error {
	return err.code
}

func (err Error) Retriable() Error {
	return Error{
		code:      err.code,
		msg:       err.msg,
		retriable: true,
	}
}

func (err Error) Msg(m string) Error {
	return Error{
		code:      err.code,
		msg:       m,
		retriable: err.retriable,
	}
}

func (err Error) RError() *rtypes.Error {
	return &rtypes.Error{
		Code:      int32(err.code),
		Message:   err.msg,
		Retriable: err.retriable,
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

type ErrorOption struct {
	f func(orig error, err *rtypes.Error)
}

func MapRpcErrCode(rpcErrCode dcrjson.RPCErrorCode, rosettaErrCode ErrorCode) ErrorOption {
	return ErrorOption{
		f: func(orig error, err *rtypes.Error) {
			if rpcerr, ok := orig.(*dcrjson.RPCError); ok && rpcerr.Code == rpcErrCode {
				err.Code = int32(rosettaErrCode)
			}
		},
	}
}

// DcrdError converts errors received as a result of a dcrd rpc client call to
// an appropriate rosetta error struct.
func DcrdError(err error, opts ...ErrorOption) *rtypes.Error {
	e := &rtypes.Error{
		Message: err.Error(),

		// By default all dcrd errors are retriable.
		Retriable: true,
	}

	switch {
	case errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):

		e.Code = int32(ErrRequestCanceled)
	}

	for _, opt := range opts {
		opt.f(err, e)
	}

	return e
}

func RError(err error) *rtypes.Error {
	var e Error
	if errors.As(err, &e) {
		return e.RError()
	}

	return &rtypes.Error{
		Message: err.Error(),
	}
}
