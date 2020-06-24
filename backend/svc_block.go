package backend

import (
	"context"
	"fmt"

	rserver "github.com/coinbase/rosetta-sdk-go/server"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrros/types"
	"golang.org/x/sync/errgroup"
)

var _ rserver.BlockAPIServicer = (*Server)(nil)

func (s *Server) inputsFetcher(ctx context.Context, inputList ...*wire.OutPoint) (map[wire.OutPoint]*types.PrevInput, error) {
	// First, dedupe the needed txs.
	txs := make(map[chainhash.Hash]*wire.MsgTx, len(inputList))
	for _, in := range inputList {
		txs[in.Hash] = nil
	}

	// Now, request the txs concurrently from dcrd (assumes txindex is on).
	g, gctx := errgroup.WithContext(ctx)
	for txh := range txs {
		txh := txh
		g.Go(func() error {
			tx, err := s.c.GetRawTransaction(gctx, &txh)
			if err != nil {
				return err
			}
			txs[txh] = tx.MsgTx()
			return nil
		})
	}

	err := g.Wait()
	if err != nil {
		return nil, err
	}

	// Now build the resulting map.
	res := make(map[wire.OutPoint]*types.PrevInput, len(inputList))
	for _, in := range inputList {
		tx := txs[in.Hash]
		if len(tx.TxOut) <= int(in.Index) {
			return nil, fmt.Errorf("non-existant output index %s", in.String())
		}
		out := tx.TxOut[in.Index]
		res[*in] = &types.PrevInput{
			PkScript: out.PkScript,
			Version:  out.Version,
			Amount:   dcrutil.Amount(out.Value),
		}
	}

	return res, nil
}

func (s *Server) makeInputsFetcher(ctx context.Context) types.PrevInputsFetcher {
	return func(inputList ...*wire.OutPoint) (map[wire.OutPoint]*types.PrevInput, error) {
		return s.inputsFetcher(ctx, inputList...)
	}
}

func (s *Server) Block(ctx context.Context, req *rtypes.BlockRequest) (*rtypes.BlockResponse, *rtypes.Error) {
	var bh *chainhash.Hash
	var err error

	bli := req.BlockIdentifier
	switch {
	case bli == nil || (bli.Hash == nil && bli.Index == nil):
		// Neither hash nor index were specified, so fetch current
		// block.
		if bh, err = s.c.GetBestBlockHash(ctx); err != nil {
			return nil, types.DcrdError(err)
		}

	case bli.Hash != nil:
		bh = new(chainhash.Hash)
		if err := chainhash.Decode(bh, *bli.Hash); err != nil {
			return nil, types.ErrInvalidChainHash.RError()
		}

	case bli.Index != nil:
		if bh, err = s.c.GetBlockHash(ctx, *bli.Index); err != nil {
			return nil, types.DcrdError(err, types.MapRpcErrCode(-5, types.ErrBlockNotFound))
		}

	default:
		// This should never happen unless the spec changed to allow
		// some other form of block querying.
		return nil, types.ErrInvalidArgument.RError()
	}

	b, err := s.c.GetBlock(ctx, bh)
	if err != nil {
		return nil, types.DcrdError(err, types.MapRpcErrCode(-5, types.ErrBlockNotFound))
	}
	var prev *wire.MsgBlock

	// Fetch the previous block when the current block disapproves of its
	// parent, since we'll need to reverse the transactions in the parent.
	// We include a special check for the genesis block because it has
	// VoteBits == 0.
	approvesParent := b.Header.VoteBits&0x01 == 0x01
	if !approvesParent && b.Header.Height > 0 {
		prev, err = s.c.GetBlock(ctx, &b.Header.PrevBlock)
		if err != nil {
			return nil, types.DcrdError(err, types.MapRpcErrCode(-5, types.ErrBlockNotFound))
		}
	}

	fetchInputs := s.makeInputsFetcher(ctx)
	rblock, err := types.WireBlockToRosetta(b, prev, fetchInputs, s.chainParams)
	if err != nil {
		return nil, types.DcrdError(err)
	}
	return &rtypes.BlockResponse{
		Block: rblock,
	}, nil
}

func (s *Server) BlockTransaction(context.Context, *rtypes.BlockTransactionRequest,
) (*rtypes.BlockTransactionResponse, *rtypes.Error) {

	return nil, nil
}
