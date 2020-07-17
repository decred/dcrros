// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package types

import (
	"context"
	"errors"
	"reflect"
	"testing"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
)

// rerror is a mock RosettaError
type rerror struct {
	e *rtypes.Error
}

func (err rerror) RError() *rtypes.Error {
	return err.e
}

func (err rerror) Error() string {
	return err.e.Message
}

// TestAllErrorsHaveDefaultMsgs ensures that all defined error constants have a
// corresponding error message.
func TestAllErrorsHaveDefaultMsgs(t *testing.T) {
	for i := ErrorCode(0); i < nbErrorCodes; i++ {
		if _, ok := errorCodeMsgs[i]; !ok {
			t.Errorf("Code %d does not have a corresponding message", i)
		}
	}
}

// TestAllErrorsMsgsHaveCode ensures that all defined error messages have a
// corresponding error code constant.
func TestAllErrorsMsgsHaveCode(t *testing.T) {
	for i, v := range errorCodeMsgs {
		if i < 0 || i >= nbErrorCodes {
			t.Errorf("errorCodeMsg %d (%s) outside valid range", i, v)
		}
	}
}

// TestNoDupeErrorMsgs ensures there are no duplicate error messages.
func TestNoDupeErrorMsgs(t *testing.T) {
	m := make(map[string]struct{})
	for _, v := range errorCodeMsgs {
		if _, ok := m[v]; ok {
			t.Errorf("duplicate error msg '%s'", v)
		}
		m[v] = struct{}{}
	}
}

// TestErrorCodeIsAs ensures ErrorCode can be used with the standard lib's
// errors.Is/errors.As functions.
func TestErrorsIsAs(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		target    error
		wantMatch bool
		wantAs    ErrorCode
	}{{
		name:      "ErrBlockNotFound == ErrBlockNotFound",
		err:       ErrBlockNotFound,
		target:    ErrBlockNotFound,
		wantMatch: true,
		wantAs:    ErrBlockNotFound,
	}, {
		name:      "Error.ErrBlockNotFound == ErrBlockNotFound",
		err:       Error{code: ErrBlockNotFound},
		target:    ErrBlockNotFound,
		wantMatch: true,
		wantAs:    ErrBlockNotFound,
	}, {
		name:      "ErrBlockNotFound == Error.ErrBlockNotFound",
		err:       ErrBlockNotFound,
		target:    Error{code: ErrBlockNotFound},
		wantMatch: true,
		wantAs:    ErrBlockNotFound,
	}, {
		name:      "Error.ErrBlockNotFound == Error.ErrBlockNotFound",
		err:       Error{code: ErrBlockNotFound},
		target:    Error{code: ErrBlockNotFound},
		wantMatch: true,
		wantAs:    ErrBlockNotFound,
	}, {
		name:      "ErrBlockNotFound == rerror.ErrBlockNotFound",
		err:       ErrBlockNotFound,
		target:    rerror{&rtypes.Error{Code: int32(ErrBlockNotFound)}},
		wantMatch: true,
		wantAs:    ErrBlockNotFound,
	}, {
		name:      "ErrBlockNotFound != ErrTxNotFound",
		err:       ErrBlockNotFound,
		target:    ErrTxNotFound,
		wantMatch: false,
		wantAs:    ErrBlockNotFound,
	}, {
		name:      "Error.ErrBlockNotFound != ErrTxNotFound",
		err:       Error{code: ErrBlockNotFound},
		target:    ErrTxNotFound,
		wantMatch: false,
		wantAs:    ErrBlockNotFound,
	}, {
		name:      "ErrBlockNotFound != Error.ErrTxNotFound",
		err:       ErrBlockNotFound,
		target:    Error{code: ErrTxNotFound},
		wantMatch: false,
		wantAs:    ErrBlockNotFound,
	}}

	for _, test := range tests {
		// Ensure the error matches or not depending on the expected
		// result.
		result := errors.Is(test.err, test.target)
		if result != test.wantMatch {
			t.Errorf("%s: incorrect error identification -- got %v, want %v",
				test.name, result, test.wantMatch)
			continue
		}

		// Ensure the underlying error code can be unwrapped and is the
		// expected code.
		var code ErrorCode
		if !errors.As(test.err, &code) {
			t.Errorf("%s: unable to unwrap to error code", test.name)
			continue
		}
		if code != test.wantAs {
			t.Errorf("%s: unexpected unwrapped error code -- got %v, want %v",
				test.name, code, test.wantAs)
			continue
		}
	}
}

// TestErrorsFluentAPI tests that the ErrorCode and Error fluent API works as
// intended.
func TestErrorsFluentAPI(t *testing.T) {
	tests := []struct {
		name   string
		err    Error
		target Error
	}{{
		name:   "ErrorCode.AsError",
		err:    ErrBlockNotFound.AsError(),
		target: Error{code: ErrBlockNotFound},
	}, {
		name:   "ErrorCode.Retriable",
		err:    ErrBlockNotFound.Retriable(),
		target: Error{code: ErrBlockNotFound, retriable: true},
	}, {
		name:   "ErrorCode.Msg",
		err:    ErrBlockNotFound.Msg("new msg"),
		target: Error{code: ErrBlockNotFound, msg: "new msg"},
	}, {
		name:   "ErrorCode.Msgf",
		err:    ErrBlockNotFound.Msgf("one %d", 2),
		target: Error{code: ErrBlockNotFound, msg: "one 2"},
	}, {
		name:   "ErrorCode.Msg.Msgf.Retriable",
		err:    ErrBlockNotFound.Msg("ignored").Msgf("one %d", 2).Retriable(),
		target: Error{code: ErrBlockNotFound, msg: "one 2", retriable: true},
	}, {
		name:   "Error.Retriable",
		err:    Error{}.Retriable(),
		target: Error{retriable: true},
	}, {
		name:   "Error.Msg",
		err:    Error{}.Msg("new msg"),
		target: Error{msg: "new msg"},
	}, {
		name:   "Error.Msgf",
		err:    Error{}.Msgf("one %d", 2),
		target: Error{msg: "one 2"},
	}, {
		name:   "Error.Msg.Msgf.Retriable",
		err:    Error{}.Msg("ignored").Msgf("one %d", 2).Retriable(),
		target: Error{msg: "one 2", retriable: true},
	}}

	for _, tc := range tests {
		tc := tc
		ok := t.Run(tc.name, func(t *testing.T) {
			if tc.err != tc.target {
				t.Fatalf("%#v not equal to %#v", tc.err, tc.target)
			}
		})

		if !ok {
			break
		}
	}
}

// TestErrorsToRError ensures ErrorCode and Error values can be converted to
// appropriate Rosetta Error values.
func TestErrorsToRError(t *testing.T) {
	tests := []struct {
		name   string
		err    RosettaError
		target rtypes.Error
	}{{
		name:   "ErrorCode",
		err:    ErrBlockNotFound,
		target: rtypes.Error{Code: int32(ErrBlockNotFound), Message: errorCodeMsgs[ErrBlockNotFound]},
	}, {
		name:   "Error.Msg",
		err:    Error{}.Msg("blah"),
		target: rtypes.Error{Message: errorCodeMsgs[0], Details: map[string]interface{}{"error": "blah"}},
	}, {
		name:   "Error.Retriable",
		err:    Error{}.Retriable(),
		target: rtypes.Error{Message: errorCodeMsgs[0], Retriable: true},
	}}

	for _, tc := range tests {
		tc := tc
		ok := t.Run(tc.name, func(t *testing.T) {
			rerr := tc.err.RError()
			if rerr.Code != tc.target.Code {
				t.Fatalf("unexpected code. want=%d got=%d",
					tc.target.Code, rerr.Code)
			}
			if rerr.Message != tc.target.Message {
				t.Fatalf("unexpected message. want=%s got=%s",
					tc.target.Message, rerr.Message)
			}
			if rerr.Retriable != tc.target.Retriable {
				t.Fatalf("unexpected retriable. want=%v got=%v",
					tc.target.Retriable, rerr.Retriable)
			}
			if !reflect.DeepEqual(tc.target.Details, rerr.Details) {
				t.Fatalf("unexpected details. want=%v got=%v",
					tc.target.Details, rerr.Details)
			}
		})

		if !ok {
			break
		}
	}
}

// unwrappableEC is an error that unwraps into an ErrorCode
type unwrappableEC struct {
	c ErrorCode
}

func (err unwrappableEC) Error() string {
	return ""
}
func (err unwrappableEC) Unwrap() error {
	return err.c
}

// unwrappableError is an error that unwraps into an Error
type unwrappableErr struct {
	e Error
}

func (err unwrappableErr) Error() string {
	return ""
}
func (err unwrappableErr) Unwrap() error {
	return err.e
}

// TestRErrorFunc ensures the RError convencience function works as intended.
func TestRErrorFunc(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target rtypes.Error
	}{{
		name:   "ErrorCode",
		err:    ErrBlockNotFound,
		target: rtypes.Error{Code: int32(ErrBlockNotFound), Message: errorCodeMsgs[ErrBlockNotFound]},
	}, {
		name:   "Error",
		err:    Error{code: ErrBlockNotFound},
		target: rtypes.Error{Code: int32(ErrBlockNotFound), Message: errorCodeMsgs[ErrBlockNotFound]},
	}, {
		name:   "rerror",
		err:    rerror{&rtypes.Error{Code: 0xffff, Message: "blah"}},
		target: rtypes.Error{Code: 0xffff, Message: "blah"},
	}, {
		name:   "unwrappableEC",
		err:    unwrappableEC{ErrBlockNotFound},
		target: rtypes.Error{Code: int32(ErrBlockNotFound), Message: errorCodeMsgs[ErrBlockNotFound]},
	}, {
		name:   "unwrappableErr",
		err:    unwrappableErr{Error{code: ErrBlockNotFound}},
		target: rtypes.Error{Code: int32(ErrBlockNotFound), Message: errorCodeMsgs[ErrBlockNotFound]},
	}, {
		name:   "Unknown Error",
		err:    errors.New("blah"),
		target: rtypes.Error{Code: int32(ErrUnknown), Message: errorCodeMsgs[ErrUnknown], Details: map[string]interface{}{"error": "blah"}},
	}, {
		name:   "context.Canceled",
		err:    context.Canceled,
		target: rtypes.Error{Code: int32(ErrRequestCanceled), Message: errorCodeMsgs[ErrRequestCanceled]},
	}, {
		name:   "context.DeadlineExceeded",
		err:    context.DeadlineExceeded,
		target: rtypes.Error{Code: int32(ErrRequestCanceled), Message: errorCodeMsgs[ErrRequestCanceled]}},
	}

	for _, tc := range tests {
		tc := tc
		ok := t.Run(tc.name, func(t *testing.T) {
			rerr := RError(tc.err)
			if rerr.Code != tc.target.Code {
				t.Fatalf("unexpected code. want=%d got=%d",
					tc.target.Code, rerr.Code)
			}
			if rerr.Message != tc.target.Message {
				t.Fatalf("unexpected message. want=%s got=%s",
					tc.target.Message, rerr.Message)
			}
			if rerr.Retriable != tc.target.Retriable {
				t.Fatalf("unexpected retriable. want=%v got=%v",
					tc.target.Retriable, rerr.Retriable)
			}
			if !reflect.DeepEqual(tc.target.Details, rerr.Details) {
				t.Fatalf("unexpected details. want=%v got=%v",
					tc.target.Details, rerr.Details)
			}
		})

		if !ok {
			break
		}
	}

	// Ensure two different generic errors don't generate two different
	// messages.
	err1 := errors.New("first")
	err2 := errors.New("second")
	if RError(err1).Message != RError(err2).Message {
		t.Fatalf("Two different generic errors should not generate " +
			"different messages")
	}
}
