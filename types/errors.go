package types

import (
	"context"
	"errors"
	"fmt"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/dcrjson/v3"
)

type ErrorCode int32

const (
	ErrRequestCanceled ErrorCode = 1 + iota
	ErrInvalidChainHash
	ErrInvalidArgument
	ErrBlockNotFound
	ErrTxNotFound
	ErrUnimplemented
)

var errorCodeMsgs = map[ErrorCode]string{
	ErrRequestCanceled:  "request canceled",
	ErrInvalidChainHash: "invalid chain hash",
	ErrInvalidArgument:  "invalid argument",
	ErrBlockNotFound:    "block not found",
	ErrTxNotFound:       "tx not found",
	ErrUnimplemented:    "unimplemented",
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

type ErrorOption struct {
	f func(orig error, err *rtypes.Error)
}

func MapRpcErrCode(rpcErrCode dcrjson.RPCErrorCode, rosettaErrCode ErrorCode) ErrorOption {
	return ErrorOption{
		f: func(orig error, err *rtypes.Error) {
			fmt.Println("mapping", orig, err)
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
