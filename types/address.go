// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package types

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
)

// TreasuryAccountAddress is the address used to represent the special treasury
// account.
const TreasuryAccountAdddress = "*treasury"

// pkScriptToRawAccountAddr converts a given PkScript byte slice into a "raw"
// dcrros address. A raw address is an hex-encoded string with the following
// format 0x[2-byte-version][pktscript]
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
	if version != 0 {
		// Versions other than 0 aren't standardized yet, so return as
		// a raw hex string with a "0x" prefix.
		return pkScriptToRawAccountAddr(version, pkScript), nil
	}

	_, addrs, _, err := txscript.ExtractPkScriptAddrs(version, pkScript, chainParams, true)
	if err != nil {
		// Currently the only possible error is due to version != 0,
		// which is handled above, but err on the side of caution.
		return "", err
	}

	if len(addrs) != 1 {
		// TODO: support 'bare' (non-p2sh) multisig?
		return pkScriptToRawAccountAddr(version, pkScript), nil
	}

	saddr := addrs[0].Address()
	return saddr, nil
}

// rosettaAccountToPkScript converts the given Rosetta account and version
// information into a PkScript.
func rosettaAccountToPkScript(account *rtypes.AccountIdentifier,
	chainParams *chaincfg.Params) (uint16, []byte, error) {

	if account.Metadata == nil {
		return 0, nil, fmt.Errorf("nil account metadata")
	}

	var version uint16
	if err := metadataUint16(account.Metadata, "script_version", &version); err != nil {
		return 0, nil, fmt.Errorf("unable to decode script_version: %v", err)
	}

	var pkscript []byte
	var err error

	// Versions other than 0 aren't standardized yet, so account
	// addresseses using that should be decoded using that version or with
	// an 0x prefix need are raw pk scripts.
	if version != 0 || strings.HasPrefix(account.Address, "0x") {
		pkscript, err = rawAccountAddrToPkScript(version, account.Address)
	} else {
		var addr dcrutil.Address
		addr, err = dcrutil.DecodeAddress(account.Address, chainParams)
		if err == nil {
			pkscript, err = txscript.PayToAddrScript(addr)
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
