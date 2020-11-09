package backend

import (
	"context"
	"encoding/hex"
	"io/ioutil"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"decred.org/dcrros/backend/backenddb"
	"decred.org/dcrros/backend/internal/badgerdb"
	"decred.org/dcrros/backend/internal/memdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrjson/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/wire"
	"github.com/stretchr/testify/require"
)

// defaultTimeout is the default timeout for test contexts.
const defaultTimeout = time.Second * 30

// testCtx returns a context that gets canceled after defaultTimeout or after
// the test ends.
func testCtx(t *testing.T) context.Context {
	ctxt, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	t.Cleanup(cancel)
	return ctxt
}

// testDbInstances executes the specified test on all available DB
// implementations.
func testDbInstances(t *testing.T, parallel bool, test func(t *testing.T, db backenddb.DB)) {
	testCases := []struct {
		name  string
		newDb func(t *testing.T) backenddb.DB
	}{{
		name: "memdb",
		newDb: func(t *testing.T) backenddb.DB {
			db, err := memdb.NewMemDB()
			require.NoError(t, err)
			return db
		},
	}, {
		name: "badger-mem",
		newDb: func(t *testing.T) backenddb.DB {
			db, err := badgerdb.NewBadgerDB("")
			require.NoError(t, err)
			return db
		},
	}, {
		name: "badger",
		newDb: func(t *testing.T) backenddb.DB {
			tmpDir, err := ioutil.TempDir("", "dcrros_badger")
			require.NoError(t, err)
			t.Cleanup(func() { os.RemoveAll(tmpDir) })
			db, err := badgerdb.NewBadgerDB(tmpDir)
			require.NoError(t, err)
			return db
		},
	}}

	for _, tc := range testCases {
		ok := t.Run(tc.name, func(t *testing.T) {
			if parallel {
				t.Parallel()
			}
			db := tc.newDb(t)
			t.Cleanup(func() {
				err := db.Close()
				if err != nil {
					t.Logf("Error closing db: %v", err)
				}
			})
			test(t, db)
		})
		if !ok {
			break
		}
	}
}

// mustHex decodes the given string as a byte slice. It must only be used with
// hardcoded values.
func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// mustHash decodes the given string as chainhash. It must only be used with
// hardcoded values.
func mustHash(s string) chainhash.Hash {
	var h chainhash.Hash
	if err := chainhash.Decode(&h, s); err != nil {
		panic(err)
	}
	return h
}

// mockChain is a mock struct used for tests, which fulfills the chain
// interface.
//
// Note that this chain does _not_ follow all consensus rules since it doesn't
// have an underlying blockchain instance guaranteeing its correct behavior.
// Tests are responsible for creating an appropriate chain, according to the
// specific assertions they will perform.
//
// The exported functions are performed under the mtx lock. Unexported
// functions are not and callers need to lock the chain if performing
// concurrent modifications.
type mockChain struct {
	t      *testing.T
	noncer *rand.Rand
	params *chaincfg.Params

	mtx            sync.Mutex
	blocks         map[chainhash.Hash]*wire.MsgBlock
	blocksByHeight map[int64]*chainhash.Hash
	tipHash        chainhash.Hash
	tipHeight      int64
	txByHash       map[chainhash.Hash]*wire.MsgTx

	// The following *hook functions are called by the corresponding
	// exported functions if they are defined.

	getBlockHook           func(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error)
	getBlockHashHook       func(ctx context.Context, blockHeight int64) (*chainhash.Hash, error)
	getBlockChainInfoHook  func(ctx context.Context) (*chainjson.GetBlockChainInfoResult, error)
	getInfoHook            func(ctx context.Context) (*chainjson.InfoChainResult, error)
	versionHook            func(ctx context.Context) (map[string]chainjson.VersionResult, error)
	getRawMempoolHook      func(ctx context.Context, txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error)
	sendRawTransactionHook func(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error)
}

type blockMangler func(b *wire.MsgBlock)

func newMockChain(t *testing.T, params *chaincfg.Params) *mockChain {
	m := &mockChain{
		t:              t,
		noncer:         rand.New(rand.NewSource(0xfafef1f0f00000)),
		params:         params,
		blocks:         make(map[chainhash.Hash]*wire.MsgBlock),
		blocksByHeight: make(map[int64]*chainhash.Hash),
		txByHash:       make(map[chainhash.Hash]*wire.MsgTx),
		tipHash:        params.GenesisHash,
	}

	// Add genesis.
	m.blocks[params.GenesisHash] = params.GenesisBlock
	m.blocksByHeight[0] = &params.GenesisHash

	return m
}

func (mc *mockChain) addCoinbase(b *wire.MsgBlock) {
	pksCb := mustHex("76a914ca2adec002d8fd0992b169ae7a4779076e150c2d88ac")

	// Add a random nonce to ensure each coinbase is unique.
	var opRet [8]byte
	mc.noncer.Read(opRet[:])
	tx := &wire.MsgTx{
		TxIn: []*wire.TxIn{
			{}, // Coinbase
		},
		TxOut: []*wire.TxOut{
			{PkScript: pksCb},    // Treasury
			{PkScript: opRet[:]}, // OP_RETURN
			{PkScript: pksCb},    // PoW reward
		},
	}
	b.Transactions = append(b.Transactions, tx)
}

// addSudoTx adds a pseudo-tx directly to the tx index, allowing this tx to be
// used as source for another tx. The given value and pks are used in the
// single tx out.
func (mc *mockChain) addSudoTx(value int64, pkScript []byte) chainhash.Hash {
	// Add a random nonce to ensure each tx is unique.
	var prevHash [32]byte
	mc.noncer.Read(prevHash[:])

	tx := wire.NewMsgTx()
	tx.AddTxIn(wire.NewTxIn(&wire.OutPoint{Hash: prevHash}, 0, nil))
	tx.AddTxOut(wire.NewTxOut(value, pkScript))
	txh := tx.TxHash()
	mc.txByHash[txh] = tx
	return txh
}

// txToAddr returns a block mangler that adds a transaction that creates
// outputs paying to a specific pkScript.
//
// `nb` outputs are created in the tx, each paying `value` amount.
//
// Note the source tx for the funds is only added to tx index for input
// fetching and _not_ to the block itself.
func (mc *mockChain) txToAddr(pkScript []byte, value, nb int64) blockMangler {
	return func(b *wire.MsgBlock) {
		prevHash := mc.addSudoTx(0, []byte{})
		tx := wire.NewMsgTx()
		prevOut := &wire.OutPoint{Hash: prevHash}
		tx.AddTxIn(wire.NewTxIn(prevOut, value*nb, nil))
		for i := int64(0); i < nb; i++ {
			tx.AddTxOut(wire.NewTxOut(value, pkScript))
		}
		b.Transactions = append(b.Transactions, tx)
	}
}

// fromAddr returns a block mangler that creates a tx with `nb` inputs spending
// `value` coins from pseudo-tx outputs that use the given `pkScript`.
//
// Note the source outputs spent are _not_ added to the block, only to the tx
// index to allow input fetching.
func (mc *mockChain) txFromAddr(pkScript []byte, value, nb int64) blockMangler {
	return func(b *wire.MsgBlock) {
		tx := wire.NewMsgTx()
		for i := int64(0); i < nb; i++ {
			prevHash := mc.addSudoTx(value, pkScript)
			prevOut := &wire.OutPoint{Hash: prevHash}
			tx.AddTxIn(wire.NewTxIn(prevOut, value, nil))
		}
		tx.AddTxOut(wire.NewTxOut(0, []byte{}))
		b.Transactions = append(b.Transactions, tx)
	}
}

// extendTip generates a new block with a coinbase extending the current tip,
// calls every passed mangler and finally adds the block to the blockchain,
// replacing the current tip with it.
func (mc *mockChain) extendTip(manglers ...blockMangler) {
	// Basic block extending tip.
	height := mc.tipHeight + 1
	hdr := wire.BlockHeader{
		Height:    uint32(height),
		PrevBlock: mc.tipHash,
		Nonce:     uint32(mc.noncer.Int31()),
		VoteBits:  1, // Approves parent.
	}
	b := wire.NewMsgBlock(&hdr)
	mc.addCoinbase(b)

	// Call manglers.
	for _, f := range manglers {
		f(b)
	}

	// Update tx index.
	for _, tx := range b.Transactions {
		txh := tx.TxHash()
		mc.txByHash[txh] = tx
	}
	for _, tx := range b.STransactions {
		txh := tx.TxHash()
		mc.txByHash[txh] = tx
	}

	// Update tip.
	bh := b.BlockHash()
	mc.blocks[bh] = b
	mc.blocksByHeight[height] = &bh
	mc.tipHeight = height
	mc.tipHash = bh
}

// rewindChain rewinds the chain tip the given number of blocks.
//
// Note this doesn't currently clean up blocks that have been rewinded-out.
func (mc *mockChain) rewindChain(nbBlocks int) {
	mc.t.Helper()

	height := mc.tipHeight - int64(nbBlocks)
	bh := mc.blocksByHeight[height]
	if bh == nil {
		mc.t.Fatalf("unable to rewind chaing %d blocks from tip %d",
			nbBlocks, mc.tipHeight)
	}

	mc.tipHeight = height
	mc.tipHash = *bh
}

func (mc *mockChain) GetBlockChainInfo(ctx context.Context) (*chainjson.GetBlockChainInfoResult, error) {
	mc.mtx.Lock()
	defer mc.mtx.Unlock()

	if mc.getBlockChainInfoHook != nil {
		return mc.getBlockChainInfoHook(ctx)
	}
	return &chainjson.GetBlockChainInfoResult{
		Chain:                mc.params.Name,
		SyncHeight:           mc.tipHeight,
		InitialBlockDownload: false,
		Blocks:               mc.tipHeight,
	}, nil
}

func (mc *mockChain) getBlockHash(ctx context.Context, blockHeight int64) (*chainhash.Hash, error) {
	if blockHeight > mc.tipHeight {
		return nil, &dcrjson.RPCError{Code: dcrjson.ErrRPCOutOfRange}
	}
	return mc.blocksByHeight[blockHeight], nil
}

func (mc *mockChain) GetBlockHash(ctx context.Context, blockHeight int64) (*chainhash.Hash, error) {
	mc.mtx.Lock()
	defer mc.mtx.Unlock()

	if mc.getBlockHashHook != nil {
		return mc.getBlockHashHook(ctx, blockHeight)
	}
	return mc.getBlockHash(ctx, blockHeight)
}

func (mc *mockChain) getBlock(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error) {
	bl, ok := mc.blocks[*blockHash]
	if !ok {
		return nil, &dcrjson.RPCError{Code: dcrjson.ErrRPCBlockNotFound}
	}

	return bl, nil
}

func (mc *mockChain) GetBlock(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error) {
	mc.mtx.Lock()
	defer mc.mtx.Unlock()

	if mc.getBlockHook != nil {
		return mc.getBlockHook(ctx, blockHash)
	}

	return mc.getBlock(ctx, blockHash)
}

func (mc *mockChain) GetRawTransaction(ctx context.Context, txHash *chainhash.Hash) (*dcrutil.Tx, error) {
	mc.mtx.Lock()
	defer mc.mtx.Unlock()

	tx, ok := mc.txByHash[*txHash]
	if !ok {
		return nil, &dcrjson.RPCError{
			Code:    dcrjson.ErrRPCNoTxInfo,
			Message: "tx does not exist",
		}
	}
	return dcrutil.NewTx(tx), nil
}

func (mc *mockChain) GetBestBlockHash(ctx context.Context) (*chainhash.Hash, error) {
	return nil, nil
}

func (mc *mockChain) GetRawMempool(ctx context.Context, txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error) {
	if mc.getRawMempoolHook != nil {
		return mc.getRawMempoolHook(ctx, txType)
	}
	return nil, nil
}

func (mc *mockChain) SendRawTransaction(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error) {
	if mc.sendRawTransactionHook != nil {
		return mc.sendRawTransactionHook(ctx, tx, allowHighFees)
	}
	return nil, nil
}

func (mc *mockChain) NotifyBlocks(ctx context.Context) error {
	return nil
}

func (mc *mockChain) GetInfo(ctx context.Context) (*chainjson.InfoChainResult, error) {
	mc.mtx.Lock()
	defer mc.mtx.Unlock()

	if mc.getInfoHook != nil {
		return mc.getInfoHook(ctx)
	}
	return &chainjson.InfoChainResult{
		TxIndex: true,
	}, nil
}

func (mc *mockChain) Version(ctx context.Context) (map[string]chainjson.VersionResult, error) {
	mc.mtx.Lock()
	defer mc.mtx.Unlock()

	if mc.versionHook != nil {
		return mc.versionHook(ctx)
	}
	return map[string]chainjson.VersionResult{
		"dcrdjsonrpcapi": {Major: wantJsonRpcMajor, Minor: wantJsonRpcMinor},
		"dcrd":           {},
	}, nil
}

func (mc *mockChain) Connect(ctx context.Context, retry bool) error {
	return nil
}

// newTestServer returns a Server instance that is forced into the connected
// state, so that functions that depend on having a connected chain work.
//
// The internal context used for notifications is set to a context which is
// canceled during the test cleanup phase.
func newTestServer(t *testing.T, cfg *ServerConfig) *Server {
	t.Helper()

	ctxt, cancel := context.WithCancel(context.Background())
	svr, err := NewServer(ctxt, cfg)
	require.NoError(t, err)
	t.Cleanup(cancel)

	// Force the server to be connected.
	svr.mtx.Lock()
	svr.dcrdActiveErr = nil
	svr.ctx = ctxt
	svr.mtx.Unlock()

	return svr
}
