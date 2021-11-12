// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"
	"errors"
	"testing"

	"decred.org/dcrros/backend/backenddb"
	"decred.org/dcrros/backend/internal/memdb"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/stretchr/testify/require"
)

// TestNetworkListEndpoint tests that the NetworkList() call works as expected.
func TestNetworkListEndpoint(t *testing.T) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	// Initialize the server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}
	svr := newTestServer(t, cfg)

	// Execute the NetworkList call.
	res, rerr := svr.NetworkList(context.Background(), nil)
	require.Nil(t, rerr)

	if len(res.NetworkIdentifiers) != 1 {
		t.Fatalf("unexpected nb of network identifiers. want=1 got=%d",
			len(res.NetworkIdentifiers))
	}

	gotId := res.NetworkIdentifiers[0]
	if gotId.Blockchain != "decred" {
		t.Fatalf("unexpected network blockchain. want=decred got=%s",
			gotId.Blockchain)
	}

	if gotId.Network != params.Name {
		t.Fatalf("unexpected network name. want=%s got=%s", params.Name,
			gotId.Network)
	}
}

// TestNetworkOptionsEndpoints verifies the NetworkOptions() call works as
// expected.
func TestNetworkOptionsEndpoint(t *testing.T) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	// Initialize the server.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
	}
	svr := newTestServer(t, cfg)

	// Execute the NetworkOptions call.
	res, rerr := svr.NetworkOptions(context.Background(), nil)
	require.Nil(t, rerr)

	if res.Version.RosettaVersion != rosettaVersion {
		t.Fatalf("unexpected rosetta version. want=%s got=%s",
			rosettaVersion, res.Version.RosettaVersion)
	}
}

func testNetworkStatusEndpoint(t *testing.T, db backenddb.DB) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	// Generate a chain.
	c.extendTip()
	c.extendTip()
	c.extendTip()

	// Initialize the server and process the blockchain.
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
		db:          db,
	}
	svr := newTestServer(t, cfg)

	// Shorter function names to improve readability.
	assertTip := func(hash *chainhash.Hash, index int64) {
		t.Helper()

		res, rerr := svr.NetworkStatus(context.Background(), nil)
		require.Nil(t, rerr)

		cbi := res.CurrentBlockIdentifier
		if cbi.Hash != hash.String() {
			t.Fatalf("unexpected tip hash. want=%s got=%s",
				hash, cbi.Hash)
		}
		if cbi.Index != index {
			t.Fatalf("unexpected tip height. want=%d got=%d",
				index, cbi.Index)
		}

		genesis := res.GenesisBlockIdentifier
		if genesis.Hash != params.GenesisHash.String() {
			t.Fatalf("unexpected genesis hash. want=%s got=%s",
				params.GenesisHash, genesis.Hash)
		}
		if genesis.Index != 0 {
			t.Fatalf("unexpected genesis index. want=%d got=%d",
				0, genesis.Index)
		}
	}

	// An empty server should return genesis as the current tip.
	assertTip(&params.GenesisHash, 0)

	// Preprocess the mock chain and generate an unprocessed block.
	err := svr.preProcessAccounts(testCtx(t))
	require.NoError(t, err)
	c.mtx.Lock()
	lastHash := c.tipHash
	lastHeight := c.tipHeight
	c.extendTip()
	tipHeader := c.blocks[c.tipHash].Header
	c.mtx.Unlock()

	// The server tip should match the last processed block.
	assertTip(&lastHash, lastHeight)

	// Connect the unprocessed block.
	err = svr.handleBlockConnected(testCtx(t), &tipHeader)
	require.NoError(t, err)

	// The server tip should match the new block.
	assertTip(&c.tipHash, c.tipHeight)

	// Disconnect the tip block.
	err = svr.handleBlockDisconnected(testCtx(t), &tipHeader)
	require.NoError(t, err)

	// The server tip should have reverted to the previous one.
	assertTip(&lastHash, lastHeight)
}

func TestNetworkStatusEndpoint(t *testing.T) {
	testDbInstances(t, true, testNetworkStatusEndpoint)
}

// TestNetworkStatusPeers verifies peers are returned correctly by the
// /network/status endpoint.
func TestNetworkStatusPeers(t *testing.T) {
	params := chaincfg.RegNetParams()
	c := newMockChain(t, params)

	// Initialize the server and process the blockchain.
	db, err := memdb.NewMemDB()
	require.NoError(t, err)
	cfg := &ServerConfig{
		ChainParams: params,
		DBType:      dbTypePreconfigured,
		c:           c,
		db:          db,
	}
	svr := newTestServer(t, cfg)

	type testCase struct {
		name      string
		peers     []chainjson.GetPeerInfoResult
		err       error
		wantPeers []*rtypes.Peer
	}

	testCases := []testCase{{
		name:      "no peers",
		peers:     []chainjson.GetPeerInfoResult{},
		wantPeers: []*rtypes.Peer{},
	}, {
		name: "one peer",
		peers: []chainjson.GetPeerInfoResult{{
			ID:       1,
			Addr:     "addr1",
			ConnTime: 20,
		}},
		wantPeers: []*rtypes.Peer{{
			PeerID: "1",
			Metadata: map[string]interface{}{
				"addr":      "addr1",
				"inbound":   false,
				"conn_time": int64(20),
			},
		}},
	}, {
		name: "multiple peers",
		peers: []chainjson.GetPeerInfoResult{{
			ID:       1,
			Addr:     "addr1",
			ConnTime: 20,
		}, {
			ID:       10,
			Addr:     "addr10",
			ConnTime: 200,
			Inbound:  true,
		}},
		wantPeers: []*rtypes.Peer{{
			PeerID: "1",
			Metadata: map[string]interface{}{
				"addr":      "addr1",
				"inbound":   false,
				"conn_time": int64(20),
			},
		}, {
			PeerID: "10",
			Metadata: map[string]interface{}{
				"addr":      "addr10",
				"inbound":   true,
				"conn_time": int64(200),
			},
		}},
	}, {
		name: "errored",
		err:  errors.New("foo"),
	}}

	test := func(t *testing.T, tc *testCase) {
		c.getPeerInfoHook = func(ctx context.Context) ([]chainjson.GetPeerInfoResult, error) {
			return tc.peers, tc.err
		}

		res, rerr := svr.NetworkStatus(testCtx(t), &rtypes.NetworkRequest{})
		if tc.err != nil {
			if rerr == nil {
				t.Fatalf("unexpected error. want=%v, got=%v",
					tc.err, rerr)
			}

			return
		}
		require.Nil(t, rerr)
		require.Len(t, res.Peers, len(tc.wantPeers))

		for i, want := range tc.wantPeers {
			got := res.Peers[i]
			require.Equal(t, want.PeerID, got.PeerID)
			require.Equal(t, want.Metadata["addr"], got.Metadata["addr"])
			require.Equal(t, want.Metadata["inbound"], got.Metadata["inbound"])
			require.Equal(t, want.Metadata["conn_time"], got.Metadata["conn_time"])
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { test(t, &tc) })
	}
}
