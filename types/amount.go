// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package types

import (
	"errors"
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

var (
	errNilAmount             = errors.New("nil amount")
	errNilCurrency           = errors.New("nil currency")
	errWrongCurrencySymbol   = errors.New("currency symbol does not match required value")
	errWrongCurrencyDecimals = errors.New("currency decimals does not match required value")
	errInvalidAmountInt      = errors.New("invalid int amount")
)

// RosettaToDcrAmount converts a Rosetta amount into a standard dcrutil Amount.
// Only amounts with the dcrros currency symbol (Symbol: "DCR", Decimals: 8)
// can be converted.
func RosettaToDcrAmount(ramt *rtypes.Amount) (dcrutil.Amount, error) {
	if ramt == nil {
		return 0, errNilAmount
	}
	if ramt.Currency == nil {
		return 0, errNilCurrency
	}
	if ramt.Currency.Symbol != currencySymbol.Symbol {
		return 0, errWrongCurrencySymbol
	}
	if ramt.Currency.Decimals != currencySymbol.Decimals {
		return 0, errWrongCurrencyDecimals
	}
	i, err := strconv.ParseInt(ramt.Value, 10, 64)
	if err != nil {
		err = fmt.Errorf("%w: %v", errInvalidAmountInt, err)
	}
	return dcrutil.Amount(i), err
}
