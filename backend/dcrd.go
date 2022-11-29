// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backend

import (
	"context"
	"errors"
	"fmt"
	"time"

	"decred.org/dcrros/backend/backenddb"
	"decred.org/dcrros/types"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrjson/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/rpcclient/v8"
	"github.com/decred/dcrd/wire"
)

const (
	// The following define the minium json rpc server the underlying dcrd
	// instance should be running on. These are interpreted according to
	// semver, so any difference in major versions causes an error while we
	// accept any minor version greater than or equal to the minimum.
	wantJsonRpcMajor uint32 = 8
	wantJsonRpcMinor uint32 = 0
)

var (
	errDcrdUnconnected = errors.New("dcrd instance not connected")
	errDcrdUnsuitable  = errors.New("dcrd instance is unsuitable for dcrros operation")

	syncStatusStageBlockchainSync     = "blockchain_sync"
	syncStatusStageProcessingAccounts = "processing_accounts"
)

// chain specifies the functions needed by a backing chain implementation (an
// rpcclient.Client instance, a mock chain used for tests, etc).
type chain interface {
	GetBlockChainInfo(ctx context.Context) (*chainjson.GetBlockChainInfoResult, error)
	GetBlockHash(ctx context.Context, blockHeight int64) (*chainhash.Hash, error)
	GetBlock(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error)
	GetRawTransaction(ctx context.Context, txHash *chainhash.Hash) (*dcrutil.Tx, error)
	GetBestBlockHash(ctx context.Context) (*chainhash.Hash, error)
	GetRawMempool(ctx context.Context, txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error)
	SendRawTransaction(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error)
	NotifyBlocks(ctx context.Context) error
	GetInfo(ctx context.Context) (*chainjson.InfoChainResult, error)
	Version(ctx context.Context) (map[string]chainjson.VersionResult, error)
	Connect(ctx context.Context, retry bool) error
	GetPeerInfo(ctx context.Context) ([]chainjson.GetPeerInfoResult, error)
}

// checkDcrd verifies whether the specified dcrd instance fulfills the required
// elements for running the dcrros server.
//
// This method returns the version string reported by the dcrd instance.
func checkDcrd(ctx context.Context, c chain, chain *chaincfg.Params) (string, error) {
	chainInfo, err := c.GetBlockChainInfo(ctx)
	if err != nil {
		return "", fmt.Errorf("unable to get blockchain info from dcrd: %v", err)
	}

	if chainInfo.Chain != chain.Name {
		return "", fmt.Errorf("dcrros and dcrd network mismatch (want %s, "+
			"got %s)", chain.Name, chainInfo.Chain)
	}

	info, err := c.GetInfo(ctx)
	if err != nil {
		return "", fmt.Errorf("unable to get info from dcrd: %v", err)
	}

	if !info.TxIndex {
		return "", fmt.Errorf("dcrd running without --txindex")
	}

	version, err := c.Version(ctx)
	if err != nil {
		return "", fmt.Errorf("unable to query dcrd vesion: %v", err)
	}
	rpcVersion, ok := version["dcrdjsonrpcapi"]
	if !ok {
		return "", fmt.Errorf("dcrd did not provide the 'dcrdjsonrpcapi' version")
	}
	if rpcVersion.Major != wantJsonRpcMajor || rpcVersion.Minor < wantJsonRpcMinor {
		return "", fmt.Errorf("dcrd running on unsupported rpcjson version "+
			"(want %d.%d got %s)", wantJsonRpcMajor,
			wantJsonRpcMinor, rpcVersion.VersionString)
	}

	dcrdVersion, ok := version["dcrd"]
	if !ok {
		return "", fmt.Errorf("dcrd did not provide the 'dcrd' version")
	}
	return dcrdVersion.VersionString, nil
}

// CheckDcrd verifies whether the dcrd in the given address is reachable and
// usable by a Server instance.
//
// Note that while we do some perfunctory tests on the specified dcrd instance,
// there's no guarantee the underlying server won't change (e.g. changing the
// chain after a restart) so this is only offered as a helper for early testing
// during process startup for easier error reporting.
func CheckDcrd(ctx context.Context, cfg *ServerConfig) error {
	// We make a copy of the passed config because we change some of the
	// parameters locally to ensure they are configured as needed by the
	// Server struct.
	connCfg := *cfg.DcrdCfg
	connCfg.DisableConnectOnNew = true
	connCfg.DisableAutoReconnect = true
	connCfg.HTTPPostMode = false
	c, err := rpcclient.New(&connCfg, nil)
	if err != nil {
		return err
	}

	err = c.Connect(ctx, false)
	if err != nil {
		return err
	}

	_, err = checkDcrd(ctx, c, cfg.ChainParams)
	c.Disconnect()
	return err
}

// isErrRPCOutOFRange returns true if the given error is an RPCError with a
// ErrRPCOutOfRange code.
func isErrRPCOutOfRange(err error) bool {
	if rpcerr, ok := err.(*dcrjson.RPCError); ok && rpcerr.Code == dcrjson.ErrRPCOutOfRange {
		return true
	}
	return false
}

// isErrNoTxInfo returns true if the given error is an RPCError with a
// ErrRPCNoTxInfo code.
func isErrNoTxInfo(err error) bool {
	if rpcerr, ok := err.(*dcrjson.RPCError); ok && rpcerr.Code == dcrjson.ErrRPCNoTxInfo {
		return true
	}
	return false
}

// waitForDcrdConnection waits until the server is connected to an underlying
// dcrd node.
func (s *Server) waitForDcrdConnection(ctx context.Context) error {
	svrLog.Debugf("Waiting for an active dcrd connection")
	for {
		if err := s.isDcrdActive(); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

var errNoPeersAfterTimeout = errors.New("no peers after timeout")

// waitForPeers waits until the underlying blockchain has peers to fetch blocks
// from or the given timeout expires.
//
// Note that this returns errNoPeersAfterTimeout if the timeout elapses without
// any peers being found.
func (s *Server) waitForPeers(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	svrLog.Infof("Waiting for dcrd to have peers until %s",
		deadline.Format("2006-01-02 15:04:05"))
	for {
		if time.Now().After(deadline) {
			return errNoPeersAfterTimeout
		}

		peers, err := s.c.GetPeerInfo(ctx)
		if err != nil {
			return err
		}

		if len(peers) > 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// waitForBlockchainSync blocks until the underlying dcrd node is synced to the
// best known chain.
func (s *Server) waitForBlockchainSync(ctx context.Context) error {
	var lastLogTime time.Time

	_, bestHeight, err := s.lastProcessedBlock(ctx)
	if err != nil {
		return err
	}
	svrLog.Infof("Waiting for blockchain sync to at least height %d", bestHeight)

	for {
		info, err := s.c.GetBlockChainInfo(ctx)
		if err != nil {
			return fmt.Errorf("unable to get blockchain info from dcrd: %v", err)
		}

		// Verify we're connected to a suitable dcrd node. We do this
		// after calling GetBlockChainInfo() because that call might
		// have triggered a reconnect.
		if err := s.isDcrdActive(); err != nil {
			if time.Since(lastLogTime) > time.Minute {
				svrLog.Infof("Not connected to a suitable dcrd "+
					"instance: %v", err)
				lastLogTime = time.Now()
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}

			continue
		}

		syncComplete := info.SyncHeight > 0 &&
			!info.InitialBlockDownload &&
			info.SyncHeight <= info.Blocks &&
			info.Blocks >= bestHeight

		// Update the current sync status.
		s.mtx.Lock()
		s.syncStatus.Stage = &syncStatusStageBlockchainSync
		s.syncStatus.CurrentIndex = &info.Blocks
		s.syncStatus.TargetIndex = &info.SyncHeight
		s.syncStatus.Synced = &syncComplete
		s.mtx.Unlock()

		if syncComplete {
			svrLog.Infof("Blockchain sync complete at height %d",
				info.Blocks)
			return nil
		}

		if time.Since(lastLogTime) > time.Minute {
			svrLog.Infof("Waiting blockchain sync (IBD=%v Headers=%d "+
				"Blocks=%d Target=%d Progress=%.2f%%)",
				info.InitialBlockDownload, info.Headers,
				info.Blocks, info.SyncHeight,
				info.VerificationProgress*100)
			lastLogTime = time.Now()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}

		// If syncHeight is still zero after the first wait, we'll
		// allow the server to start anyway. This allows using the
		// server even when the blockchain is empty and there are no
		// peers to sync to, which is the case on a newly created,
		// isolated (i.e. single peer) simnet or on offline nodes.
		if info.SyncHeight == 0 {
			svrLog.Infof("Terminating chain sync wait early due to SyncHeight == 0")
			return nil
		}
	}
}

// bestBlock returns the current best block hash, height and decoded block.
func (s *Server) bestBlock(ctx context.Context) (*chainhash.Hash, int64, *wire.MsgBlock, error) {
	hash, _, err := s.lastProcessedBlock(ctx)
	if err != nil {
		return nil, 0, nil, err
	}

	// Special case an unitialized database to return the genesis block.
	var emptyHash chainhash.Hash
	if *hash == emptyHash {
		return &s.chainParams.GenesisHash, 0, s.chainParams.GenesisBlock, nil
	}

	block, err := s.getBlock(ctx, hash)
	if err != nil {
		return nil, 0, nil, err
	}

	return hash, int64(block.Header.Height), block, nil
}

// getBlock returns the given block identified by its hash.
//
// It returns a types.ErrBlockNotFound if the given block is not found.
//
// Note that this can return blocks that have not yet been processed.
func (s *Server) getBlock(ctx context.Context, bh *chainhash.Hash) (*wire.MsgBlock, error) {
	bl, ok := s.cacheBlocks.Lookup(*bh)
	if ok {
		return bl.(*wire.MsgBlock), nil
	}

	if err := s.isDcrdActive(); err != nil {
		return nil, err
	}

	b, err := s.c.GetBlock(ctx, bh)
	if err == nil {
		s.cacheBlocks.Add(*bh, b)
		return b, err
	}

	if rpcerr, ok := err.(*dcrjson.RPCError); ok && rpcerr.Code == dcrjson.ErrRPCBlockNotFound {
		return nil, types.ErrBlockNotFound
	}

	return nil, err
}

// getBlockHash returns the block hash of the main chain block at the provided
// height.
//
// It returns a types.ErrBlockIndexPastTip if the given block height doesn't
// exist in the blockchain.
//
// Note this only returns block hashes for previously processed blocks.
func (s *Server) getBlockHash(ctx context.Context, height int64) (*chainhash.Hash, error) {
	// Special case genesis.
	if height == 0 {
		return &s.chainParams.GenesisHash, nil
	}

	var bh chainhash.Hash
	err := s.db.View(ctx, func(dbtx backenddb.ReadTx) error {
		var err error
		bh, err = s.db.ProcessedBlockHash(dbtx, height)
		return err
	})

	switch {
	case errors.Is(err, backenddb.ErrBlockHeightNotFound):
		return nil, types.ErrBlockIndexAfterTip
	case err != nil:
		return nil, err
	default:
		return &bh, nil
	}
}

// getBlockByPartialId returns the block identified by the provided Rosetta
// partial block identifier.
//
// Note that when fetching by block hash, an unprocessed block may be returned.
// When fetching the best block or by height, only processed blocks are
// returned.
func (s *Server) getBlockByPartialId(ctx context.Context, bli *rtypes.PartialBlockIdentifier) (*chainhash.Hash, int64, *wire.MsgBlock, error) {
	var bh *chainhash.Hash
	var err error

	switch {
	case bli == nil || (bli.Hash == nil && bli.Index == nil):
		// Neither hash nor index were specified, so fetch current
		// block.
		err := s.db.View(ctx, func(dbtx backenddb.ReadTx) error {
			bhh, _, err := s.db.LastProcessedBlock(dbtx)
			if err == nil {
				bh = &bhh
			}
			return err
		})
		if err != nil {
			return nil, 0, nil, err
		}

	case bli.Hash != nil:
		bh = new(chainhash.Hash)
		if err := chainhash.Decode(bh, *bli.Hash); err != nil {
			return nil, 0, nil, types.ErrInvalidChainHash
		}
	case bli.Index != nil:
		if bh, err = s.getBlockHash(ctx, *bli.Index); err != nil {
			return nil, 0, nil, err
		}
	default:
		// This should never happen unless the spec changed to allow
		// some other form of block querying.
		return nil, 0, nil, types.ErrInvalidArgument
	}

	b, err := s.getBlock(ctx, bh)
	if err != nil {
		return nil, 0, nil, err
	}

	return bh, int64(b.Header.Height), b, nil
}

func (s *Server) getRawTx(ctx context.Context, txh *chainhash.Hash) (*wire.MsgTx, error) {
	if tx, ok := s.cacheRawTxs.Lookup(*txh); ok {
		return tx.(*wire.MsgTx), nil
	}

	if err := s.isDcrdActive(); err != nil {
		return nil, err
	}

	tx, err := s.c.GetRawTransaction(ctx, txh)
	if err == nil {
		s.cacheRawTxs.Add(*txh, tx.MsgTx())
		return tx.MsgTx(), nil
	}

	return nil, err
}
