// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package types

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/txscript/v4/stdscript"
)

// TreasuryAccountAddress is the address used to represent the special treasury
// account.
const TreasuryAccountAdddress = "*treasury"

// pkScriptToRawAccountAddr converts a given PkScript byte slice into a "raw"
// dcrros address. A raw address is an hex-encoded string with the following
// format 0x[2-byte-version][pkscript]
func pkScriptToRawAccountAddr(version uint16, pkScript []byte) string {
	addrBytes := make([]byte, 2+2*2+2*len(pkScript))
	addrBytes[0] = 0x30 // "0"
	addrBytes[1] = 0x78 // "x"
	versionBytes := []byte{byte(version >> 8), byte(version)}
	hex.Encode(addrBytes[2:6], versionBytes)
	hex.Encode(addrBytes[6:], pkScript)
	return string(addrBytes)
}

// rawAccountAddrToPkScript converts the given version and raw address string
// into a byte slice. The version must match the encoded version, otherwise
// this fails.
func rawAccountAddrToPkScript(version uint16, addr string) ([]byte, error) {
	if len(addr) < 6 {
		return nil, fmt.Errorf("raw address too small")
	}
	if !strings.HasPrefix(addr, "0x") {
		return nil, fmt.Errorf("raw address does not have 0x prefix")
	}
	rawVersion, err := strconv.ParseInt(addr[2:6], 16, 16)
	if err != nil || rawVersion < 0 {
		return nil, fmt.Errorf("invalid version int: %v", err)
	}
	if uint16(rawVersion) != version {
		return nil, fmt.Errorf("incorrect version %d (expected %d)", rawVersion,
			version)
	}

	return hex.DecodeString(addr[6:])
}

// dcrPkScriptToAccountAddr converts a given pkscript and version to an
// address.
//
// PkScripts of the standard formats (P2PKH, P2SH, etc) are converted to their
// standard string encoding and unrecognized scripts are converted to raw
// format.
func dcrPkScriptToAccountAddr(version uint16, pkScript []byte, chainParams *chaincfg.Params) (string, error) {
	typ, addrs := stdscript.ExtractAddrs(version, pkScript, chainParams)
	if typ == stdscript.STNonStandard || len(addrs) != 1 {
		// The only current exception where typ != STNonStandard and
		// len(addrs) > 1 is for 'bare' (non-P2SH) multisig, where it
		// would be hard to determine which address has a given
		// balance, so we also return a raw script in this case.
		return pkScriptToRawAccountAddr(version, pkScript), nil
	}

	saddr := addrs[0].String()
	return saddr, nil
}

var (
	errNilAccountMetadata        = errors.New("nil account metadata")
	errCannotDecodeScriptVersion = errors.New("unable to decode script_version")
)

// rosettaAccountToPkScript converts the given Rosetta account and version
// information into a PkScript.
func rosettaAccountToPkScript(account *rtypes.AccountIdentifier,
	chainParams *chaincfg.Params) (uint16, []byte, error) {

	if account.Metadata == nil {
		return 0, nil, errNilAccountMetadata
	}

	var version uint16
	if err := metadataUint16(account.Metadata, "script_version", &version); err != nil {
		return 0, nil, fmt.Errorf("%w: %v", errCannotDecodeScriptVersion, err)
	}

	var pkscript []byte
	var err error

	// Versions other than 0 aren't standardized yet, so account addresses
	// using that should be decoded using that version or with an 0x prefix
	// need are raw pk scripts.
	if version != 0 || strings.HasPrefix(account.Address, "0x") {
		pkscript, err = rawAccountAddrToPkScript(version, account.Address)
	} else {
		var addr stdaddr.Address
		addr, err = stdaddr.DecodeAddress(account.Address, chainParams)
		if err == nil {
			_, pkscript = addr.PaymentScript()
		}
	}

	return version, pkscript, err
}

// CheckRosettaAccount returns nil if the given account can be decoded by
// dcrros.
func CheckRosettaAccount(account *rtypes.AccountIdentifier,
	chainParams *chaincfg.Params) error {

	_, _, err := rosettaAccountToPkScript(account, chainParams)
	return err
}
