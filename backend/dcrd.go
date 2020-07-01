package backend

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrjson/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/rpcclient/v6"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrros/types"
)

const (
	// The following define the minium json rpc server the underlying dcrd
	// instance should be running on. These are interpreted according to
	// semver, so any difference in major versions causes an error while we
	// accept any minor version greater than or equal to the minimum.
	wantJsonRpcMajor uint32 = 6
	wantJsonRpcMinor uint32 = 1
)

// checkDcrd verifies whether the specified dcrd instance fulfills the required
// elements for running the dcrros server.
//
// This method returns the version string reported by the dcrd instance.
func checkDcrd(ctx context.Context, c *rpcclient.Client, chain *chaincfg.Params) (string, error) {
	// FIXME: disabled due to dcrd # 2235.
	/*
		info, err := c.GetBlockChainInfo(ctx)
		if err != nil {
			return "", fmt.Errorf("unable to get blockchain info from dcrd: %v", err)
		}

		if info.Chain != chain.Name {
			return "", fmt.Errorf("dcrros and dcrd network mismatch (want %s, "+
				"got %s)", chain.Name, info.Chain)
		}
	*/

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

// waitForBlockchainSync blocks until the underlying dcrd node is synced to the
// best known chain.
func (s *Server) waitForBlockchainSync(ctx context.Context) error {
	var lastLogTime time.Time

	isSimnet := s.chainParams.Name == "simnet"

	// Error msg of decred's issue # 2235.
	bugMsg := "-32603: hash 0000000000000000000000000000000000000000000000000000000000000000 does not exist"
	for {
		info, err := s.c.GetBlockChainInfo(ctx)
		if err != nil {
			// Get around a dcrd getblockchaininfo bug.
			if strings.Contains(err.Error(), bugMsg) {
				time.Sleep(time.Second)
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				continue
			}

			return fmt.Errorf("unable to get blockchain info from dcrd: %v", err)
		}

		if info.SyncHeight > 0 && info.SyncHeight <= info.Blocks {
			svrLog.Infof("Blockchain sync complete at height %d",
				info.Blocks)
			return nil
		}

		if time.Now().Sub(lastLogTime) > time.Minute {
			svrLog.Infof("Waiting blockchain sync (progress %.2f%%)",
				info.VerificationProgress*100)
			lastLogTime = time.Now()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}

		// Special case for simnet: if syncHeight is still zero after
		// the first wait, we'll allow the server to start anyway. This
		// allows using the server even when the blockchain is empty
		// and there are no peers to sync to, which is the case on a
		// newly created, isolated (i.e. single peer) simnet.
		//
		// To test having this dcrros instance perform a sync on a
		// large simnet network and an empty dcrd instance, use
		// --dcrdrun=dcrd --dcrdextraarg="--connect
		// [other-simnet-node]" so that it automatically connnects to
		// the [other-simnet-node], establishing a syncHeight > 0
		// immediately.
		if info.SyncHeight == 0 && isSimnet {
			svrLog.Infof("Ignoring SyncHeight == 0 on simnet")
			return nil
		}
	}
}

// bestBlock returns the current best block hash, height and decoded block.
func (s *Server) bestBlock(ctx context.Context) (*chainhash.Hash, int64, *wire.MsgBlock, error) {
	hash, err := s.c.GetBestBlockHash(ctx)
	if err != nil {
		return nil, 0, nil, err
	}

	block, err := s.c.GetBlock(ctx, hash)
	if err != nil {
		return nil, 0, nil, err
	}

	return hash, int64(block.Header.Height), block, nil
}

// getBlockByHeight returns the given block identified by its hash.
//
// It returns a types.ErrBlockNotFound if the given block is not found.
func (s *Server) getBlock(ctx context.Context, bh *chainhash.Hash) (*wire.MsgBlock, error) {
	bl, ok := s.blocks.Lookup(*bh)
	if ok {
		return bl.(*wire.MsgBlock), nil
	}

	b, err := s.c.GetBlock(ctx, bh)
	if err == nil {
		s.blocks.Add(*bh, b)
		return b, err
	}

	if rpcerr, ok := err.(*dcrjson.RPCError); ok && rpcerr.Code == dcrjson.ErrRPCBlockNotFound {
		return nil, types.ErrBlockNotFound
	}

	// TODO: types.DcrdError()
	return nil, err
}

// getBlockHash returns the block hash of the main chain block at the provided
// height.
//
// It returns a types.ErrBlockIndexPastTip if the given block height doesn't
// exist in the blockchain.
func (s *Server) getBlockHash(ctx context.Context, height int64) (*chainhash.Hash, error) {
	// TODO: not safe when close to tip due to reorgs.
	bhh, ok := s.blockHashes.Lookup(height)
	if ok {
		return bhh.(*chainhash.Hash), nil
	}

	bh, err := s.c.GetBlockHash(ctx, height)
	if err == nil {
		s.blockHashes.Add(height, bh)
		return bh, nil
	}

	if rpcerr, ok := err.(*dcrjson.RPCError); ok && rpcerr.Code == dcrjson.ErrRPCOutOfRange {
		return nil, types.ErrBlockIndexAfterTip
	}

	// TODO: types.DcrdError()
	return nil, err
}

// getBlockByHeight returns the block at the given main chain height. It also
// returns its block hash so callers won't have to recalculate it by calling
// BlockHash().
//
// It returns a types.ErrBlockNotFound if the given block is not found.
func (s *Server) getBlockByHeight(ctx context.Context, height int64) (*chainhash.Hash, *wire.MsgBlock, error) {
	var bh *chainhash.Hash
	var err error
	if bh, err = s.getBlockHash(ctx, height); err != nil {
		return nil, nil, err
	}

	var b *wire.MsgBlock
	if b, err = s.getBlock(ctx, bh); err != nil {
		return nil, nil, err
	}

	return bh, b, nil
}

func (s *Server) getBlockByPartialId(ctx context.Context, bli *rtypes.PartialBlockIdentifier) (*chainhash.Hash, int64, *wire.MsgBlock, error) {
	var bh *chainhash.Hash
	var err error

	switch {
	case bli == nil || (bli.Hash == nil && bli.Index == nil):
		// Neither hash nor index were specified, so fetch current
		// block.
		if bh, err = s.c.GetBestBlockHash(ctx); err != nil {
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

func (s *Server) searchRawTxs(ctx context.Context,
	address dcrutil.Address, skip, count int) ([]*chainjson.SearchRawTransactionsResult, error) {

	// TODO: count should be fixed. This might not be safe due to reorgs.
	k := fmt.Sprintf("%s_%d", address.Address(), skip)
	rc, ok := s.accountTxs.Lookup(k)
	if ok {
		return rc.([]*chainjson.SearchRawTransactionsResult), nil
	}

	res, err := s.c.SearchRawTransactionsVerbose(ctx, address, skip, count, true, false, nil)
	if err != nil && !strings.Contains(err.Error(), "No Txns available") {
		return nil, err
	}
	if res == nil {
		// Ensure no nils on valid responses.
		res = make([]*chainjson.SearchRawTransactionsResult, 0)
	}

	s.accountTxs.Add(k, res)

	return res, nil
}

func (s *Server) getRawTx(ctx context.Context, txh *chainhash.Hash) (*wire.MsgTx, error) {
	if tx, ok := s.rawTxs.Lookup(*txh); ok {
		return tx.(*wire.MsgTx), nil
	}

	tx, err := s.c.GetRawTransaction(ctx, txh)
	if err == nil {
		s.rawTxs.Add(*txh, tx.MsgTx())
		return tx.MsgTx(), nil
	}

	return nil, err
}

func (s *Server) processSequentialBlocks(ctx context.Context, startHeight int64, f func(*chainhash.Hash, *wire.MsgBlock) error) error {
	concurrency := int64(runtime.NumCPU())
	type gbbhReply struct {
		block *wire.MsgBlock
		hash  *chainhash.Hash
		err   error
	}
	chans := make([]chan gbbhReply, concurrency)
	gctx, cancel := context.WithCancel(ctx)
	for i := startHeight; i < startHeight+concurrency; i++ {
		c := make(chan gbbhReply)
		chans[i%concurrency] = c
		start := startHeight + ((i - startHeight) % concurrency)
		go func() {
			i := int64(0)
			for {
				bh, bl, err := s.getBlockByHeight(gctx, start+i)
				select {
				case c <- gbbhReply{block: bl, hash: bh, err: err}:
				case <-gctx.Done():
					return
				}
				i += concurrency
			}
		}()
	}

	var err error
	for i := startHeight; err == nil; i++ {
		var next gbbhReply
		select {
		case next = <-chans[i%concurrency]:
			err = next.err
		case <-gctx.Done():
			err = gctx.Err()
		}
		if err == nil {
			err = f(next.hash, next.block)
		}
	}
	cancel()
	if err == types.ErrBlockIndexAfterTip {
		return nil
	}

	return err
}
