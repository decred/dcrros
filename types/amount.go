// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package types

import (
	"fmt"
	"strconv"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/dcrutil/v3"
)

var (
	// CurrencySymbol is the global symbol used for DCR amounts.
	currencySymbol = &rtypes.Currency{
		Symbol:   "DCR",
		Decimals: 8,
	}
)

// DcrAmountToRosetta converts a standard dcrutil.Amount into a Rosetta Amount
// value.
func DcrAmountToRosetta(amt dcrutil.Amount) *rtypes.Amount {
	return &rtypes.Amount{
		Value:    strconv.FormatInt(int64(amt), 10),
		Currency: currencySymbol,
	}
}

// RosettaToDcrAmount converts a Rosetta amount into a standard dcrutil Amount.
// Only amounts with the dcrros currency symbol (Symbol: "DCR", Decimals: 8)
// can be converted.
func RosettaToDcrAmount(ramt *rtypes.Amount) (dcrutil.Amount, error) {
	if ramt == nil {
		return 0, fmt.Errorf("nil amount")
	}
	if ramt.Currency == nil {
		return 0, fmt.Errorf("currency not specified")
	}
	if ramt.Currency.Symbol != currencySymbol.Symbol {
		return 0, fmt.Errorf("currency symbol does not match expected %s",
			currencySymbol.Symbol)
	}
	if ramt.Currency.Decimals != currencySymbol.Decimals {
		return 0, fmt.Errorf("currency decimals does not match expected %d",
			currencySymbol.Decimals)
	}
	i, err := strconv.ParseInt(ramt.Value, 10, 64)
	return dcrutil.Amount(i), err
}
