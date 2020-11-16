// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package types

import (
	"bytes"
	"encoding/hex"
	"testing"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
)

// mustHex decodes the given string as a byte slice. It must only be used with
// hardcoded values.
func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// mustHash decodes the given string as a chainhash.Hash value or panics.
func mustHash(s string) chainhash.Hash {
	var h chainhash.Hash
	if err := chainhash.Decode(&h, s); err != nil {
		panic(err)
	}
	return h
}

// TestDcrPkScriptToRosetta tests that converting pkscripts to Rosetta
// addresses work as expected.
func TestDcrPkScriptToRosetta(t *testing.T) {
	testnet := chaincfg.TestNet3Params()
	mainnet := chaincfg.MainNetParams()

	tests := []struct {
		name     string
		version  uint16
		pks      []byte
		wantAddr string
		net      *chaincfg.Params
	}{{
		name:     "Testnet P2PKH",
		version:  0,
		pks:      mustHex("76a914a5a7f924934685fbca3008c9524dae1cea9f9d3488ac"),
		wantAddr: "Tsg83CCHqrDjocSUScqJbkezdt531FY36Sn",
		net:      testnet,
	}, {
		name:     "Testnet P2PKH v1",
		version:  1,
		pks:      mustHex("76a914a5a7f924934685fbca3008c9524dae1cea9f9d3488ac"),
		wantAddr: "0x000176a914a5a7f924934685fbca3008c9524dae1cea9f9d3488ac",
		net:      testnet,
	}, {
		name:     "Testnet P2SH",
		version:  0,
		pks:      mustHex("a914d585cd7426d25b4ea5faf1e6987aacfeda3db94287"),
		wantAddr: "TcrypGAcGCRVXrES7hWqVZb5oLJKCZEtoL1",
		net:      testnet,
	}, {
		name:     "Testnet P2SH v1",
		version:  1,
		pks:      mustHex("a914d585cd7426d25b4ea5faf1e6987aacfeda3db94287"),
		wantAddr: "0x0001a914d585cd7426d25b4ea5faf1e6987aacfeda3db94287",
		net:      testnet,
	}, {
		name:     "Mainnet P2PKH",
		version:  0,
		pks:      mustHex("76a91463ae8c6af3c51d3d6e2bb5c5f4be0d623395c5c088ac"),
		wantAddr: "Dsa3yVGJK9XFx6L5cC8YzcW3M5Q85wdEXcz",
		net:      mainnet,
	}}

	for _, tc := range tests {
		tc := tc
		ok := t.Run(tc.name, func(t *testing.T) {
			got, err := dcrPkScriptToAccountAddr(tc.version, tc.pks, tc.net)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tc.wantAddr {
				t.Fatalf("unexpected address. want=%s got=%s",
					tc.wantAddr, got)
			}
		})

		if !ok {
			break
		}
	}

}

// TestRosettaAccountToPkScript tests that converting Rosetta accounts to a
// pkscript work as expected.
func TestRosettaAccountToPkScript(t *testing.T) {
	testnet := chaincfg.TestNet3Params()
	mainnet := chaincfg.MainNetParams()

	tests := []struct {
		name    string
		version uint16
		addr    string
		valid   bool
		wantPks []byte
		net     *chaincfg.Params
	}{{
		name:    "Testnet P2PKH",
		version: 0,
		addr:    "Tsg83CCHqrDjocSUScqJbkezdt531FY36Sn",
		valid:   true,
		wantPks: mustHex("76a914a5a7f924934685fbca3008c9524dae1cea9f9d3488ac"),
		net:     testnet,
	}, {
		name:    "Testnet P2PKH v1",
		version: 1,
		addr:    "0x000176a914a5a7f924934685fbca3008c9524dae1cea9f9d3488ac",
		valid:   true,
		wantPks: mustHex("76a914a5a7f924934685fbca3008c9524dae1cea9f9d3488ac"),
		net:     testnet,
	}, {
		name:    "Testnet P2PKH wrong version",
		version: 999,
		addr:    "0x000176a914a5a7f924934685fbca3008c9524dae1cea9f9d3488ac",
		valid:   false,
		net:     testnet,
	}, {
		name:    "Testnet P2SH",
		version: 0,
		addr:    "TcrypGAcGCRVXrES7hWqVZb5oLJKCZEtoL1",
		valid:   true,
		wantPks: mustHex("a914d585cd7426d25b4ea5faf1e6987aacfeda3db94287"),
		net:     testnet,
	}, {
		name:    "Testnet P2PSH v1",
		version: 1,
		addr:    "0x0001a914d585cd7426d25b4ea5faf1e6987aacfeda3db94287",
		valid:   true,
		wantPks: mustHex("a914d585cd7426d25b4ea5faf1e6987aacfeda3db94287"),
		net:     testnet,
	}, {
		name:    "Testnet P2SH wrong version",
		version: 999,
		addr:    "0x0001a914d585cd7426d25b4ea5faf1e6987aacfeda3db94287",
		valid:   false,
		net:     testnet,
	}, {
		name:    "Testnet P2SH too short",
		version: 1,
		addr:    "0x000",
		valid:   false,
		net:     testnet,
	}, {
		name:    "Testnet P2SH wrong prefix",
		version: 1,
		addr:    "0,0001a914d585cd7426d25b4ea5faf1e6987aacfeda3db94287",
		valid:   false,
		net:     testnet,
	}, {
		name:    "Testnet P2PKH decoded as mainnet",
		version: 0,
		addr:    "Tsg83CCHqrDjocSUScqJbkezdt531FY36Sn",
		valid:   false,
		net:     mainnet,
	}, {
		name:    "Mainnet P2PKH",
		version: 0,
		addr:    "Dsa3yVGJK9XFx6L5cC8YzcW3M5Q85wdEXcz",
		valid:   true,
		wantPks: mustHex("76a91463ae8c6af3c51d3d6e2bb5c5f4be0d623395c5c088ac"),
		net:     mainnet,
	}, {
		name:    "Mainnet P2PKH decoded as testnet",
		version: 0,
		addr:    "Dsa3yVGJK9XFx6L5cC8YzcW3M5Q85wdEXcz",
		valid:   false,
		net:     testnet,
	}}

	for _, tc := range tests {
		tc := tc
		ok := t.Run(tc.name, func(t *testing.T) {
			acct := &rtypes.AccountIdentifier{
				Address: tc.addr,
				Metadata: map[string]interface{}{
					"script_version": tc.version,
				},
			}
			gotVersion, got, err := rosettaAccountToPkScript(acct, tc.net)
			gotValid := err == nil
			if tc.valid != gotValid {
				t.Fatalf("unexpected validity: want=%v got=%v",
					tc.valid, err)
			}

			if gotVersion != tc.version {
				t.Fatalf("unexpected version. want=%d got=%d",
					tc.version, gotVersion)
			}

			if !bytes.Equal(tc.wantPks, got) {
				t.Fatalf("unexpected pkscript. want=%x got=%x",
					tc.wantPks, got)
			}
		})

		if !ok {
			break
		}
	}
}

// TestCheckRosettaAccount tests that verifying rosetta accounts works as
// expected.
func TestCheckRosettaAccount(t *testing.T) {
	testnet := chaincfg.TestNet3Params()
	mainnet := chaincfg.MainNetParams()

	tests := []struct {
		name    string
		version uint16
		addr    string
		net     *chaincfg.Params
		wantErr bool
	}{{
		name:    "Testnet P2PKH",
		version: 0,
		addr:    "Tsg83CCHqrDjocSUScqJbkezdt531FY36Sn",
		net:     testnet,
		wantErr: false,
	}, {
		name:    "Testnet P2PKH v1",
		version: 1,
		addr:    "0x000176a914a5a7f924934685fbca3008c9524dae1cea9f9d3488ac",
		net:     testnet,
		wantErr: false,
	}, {
		name:    "Testnet P2PKH wrong version",
		version: 999,
		addr:    "0x000176a914a5a7f924934685fbca3008c9524dae1cea9f9d3488ac",
		net:     testnet,
		wantErr: true,
	}, {
		name:    "Testnet P2SH",
		version: 0,
		addr:    "TcrypGAcGCRVXrES7hWqVZb5oLJKCZEtoL1",
		net:     testnet,
		wantErr: false,
	}, {
		name:    "Testnet P2PSH v1",
		version: 1,
		addr:    "0x0001a914d585cd7426d25b4ea5faf1e6987aacfeda3db94287",
		net:     testnet,
		wantErr: false,
	}, {
		name:    "Testnet P2SH wrong version",
		version: 999,
		addr:    "0x0001a914d585cd7426d25b4ea5faf1e6987aacfeda3db94287",
		net:     testnet,
		wantErr: true,
	}, {
		name:    "Testnet P2PKH decoded as mainnet",
		version: 0,
		addr:    "Tsg83CCHqrDjocSUScqJbkezdt531FY36Sn",
		net:     mainnet,
		wantErr: true,
	}, {
		name:    "Mainnet P2PKH",
		version: 0,
		addr:    "Dsa3yVGJK9XFx6L5cC8YzcW3M5Q85wdEXcz",
		net:     mainnet,
		wantErr: false,
	}, {
		name:    "Mainnet P2PKH decoded as testnet",
		version: 0,
		addr:    "Dsa3yVGJK9XFx6L5cC8YzcW3M5Q85wdEXcz",
		net:     testnet,
		wantErr: true,
	}}

	for _, tc := range tests {
		tc := tc
		ok := t.Run(tc.name, func(t *testing.T) {
			acct := &rtypes.AccountIdentifier{
				Address: tc.addr,
				Metadata: map[string]interface{}{
					"script_version": tc.version,
				},
			}
			err := CheckRosettaAccount(acct, tc.net)
			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Fatalf("unexpected error. want=%v got=%v",
					tc.wantErr, gotErr)
			}
		})

		if !ok {
			break
		}
	}
}
