// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package types

import (
	"math"
	"testing"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/dcrutil/v4"
)

// TestDcrAmtToRosetta tests that converting a dcrutil.Amount to a Rosetta and
// back works as intended.
func TestDcrAmtToRosetta(t *testing.T) {
	mkAmt := func(v string) *rtypes.Amount {
		return &rtypes.Amount{Value: v}
	}
	tests := []struct {
		name string
		amt  dcrutil.Amount
		want *rtypes.Amount
	}{{
		name: "0",
		amt:  0,
		want: mkAmt("0"),
	}, {
		name: "max supply",
		amt:  21e6 * 1e8,
		want: mkAmt("2100000000000000"),
	}, {
		name: "max int64",
		amt:  math.MaxInt64,
		want: mkAmt("9223372036854775807"),
	}, {
		name: "-1",
		amt:  -1,
		want: mkAmt("-1"),
	}, {
		name: "negative max supply",
		amt:  -21e6 * 1e8,
		want: mkAmt("-2100000000000000"),
	}, {
		name: "min int64",
		amt:  math.MinInt64,
		want: mkAmt("-9223372036854775808"),
	}}

	for _, tc := range tests {
		tc := tc
		ok := t.Run(tc.name, func(t *testing.T) {
			got := DcrAmountToRosetta(tc.amt)
			if got.Value != tc.want.Value {
				t.Fatalf("incorrect conversion. want=%s got=%s",
					tc.want.Value, got.Value)
			}

			gotAmt, err := RosettaToDcrAmount(got)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if gotAmt != tc.amt {
				t.Fatalf("incorrect second conversion. want=%d got=%d",
					tc.amt, gotAmt)
			}
		})

		if !ok {
			break
		}
	}
}

// TestRosettaToDcrAmt tests that converting a Rosetta amount to dcrutil.AMount
// works as intended.
func TestRosettaToDcrAmt(t *testing.T) {
	mkAmt := func(v string) *rtypes.Amount {
		return &rtypes.Amount{Value: v, Currency: currencySymbol}
	}
	mkWrong := func(v string) *rtypes.Amount {
		return &rtypes.Amount{Value: v, Currency: &rtypes.Currency{}}
	}

	tests := []struct {
		name  string
		amt   *rtypes.Amount
		valid bool
		want  dcrutil.Amount
	}{{
		name:  "0",
		amt:   mkAmt("0"),
		valid: true,
		want:  0,
	}, {
		name:  "max supply",
		amt:   mkAmt("2100000000000000"),
		valid: true,
		want:  21e6 * 1e8,
	}, {
		name:  "max int64",
		amt:   mkAmt("9223372036854775807"),
		valid: true,
		want:  math.MaxInt64,
	}, {
		name:  "-1",
		amt:   mkAmt("-1"),
		valid: true,
		want:  -1,
	}, {
		name:  "negative max supply",
		amt:   mkAmt("-2100000000000000"),
		valid: true,
		want:  -21e6 * 1e8,
	}, {
		name:  "min int64",
		amt:   mkAmt("-9223372036854775808"),
		valid: true,
		want:  math.MinInt64,
	}, {
		name:  "wrong currency",
		amt:   mkWrong("0"),
		valid: false,
	}, {
		name:  "no currency",
		amt:   &rtypes.Amount{Value: "0"},
		valid: false,
	}, {
		name: "wrong decimals",
		amt: &rtypes.Amount{
			Value: "0",
			Currency: &rtypes.Currency{
				Symbol:   "DCR",
				Decimals: 3,
			}},
		valid: false,
	}, {
		name:  "nil value",
		amt:   nil,
		valid: false,
	}}

	for _, tc := range tests {
		tc := tc
		ok := t.Run(tc.name, func(t *testing.T) {
			gotAmt, err := RosettaToDcrAmount(tc.amt)
			gotValid := err == nil
			if tc.valid != gotValid {
				t.Fatalf("unexpected validity. want=%v got=%v",
					tc.valid, err)
			}

			if gotAmt != tc.want {
				t.Fatalf("incorrect conversion. want=%d got=%d",
					tc.want, gotAmt)
			}
		})

		if !ok {
			break
		}
	}
}
