package types

import (
	"context"
	"errors"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
)

type ErrorCode = int32

const (
	ErrRequestCanceled ErrorCode = 1 + iota
)

type ErrorOption = func(*rtypes.Error)

// DcrdError converts errors received as a result of a dcrd rpc client call to
// an appropriate rosetta error struct.
func DcrdError(err error, opts ...ErrorOption) *rtypes.Error {
	e := &rtypes.Error{
		Message: err.Error(),

		// By default all errors are retriable.
		Retriable: true,
	}

	switch {
	case errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):

		e.Code = ErrRequestCanceled
	}

	return e
}
