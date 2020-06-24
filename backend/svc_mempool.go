package backend

import (
	"context"

	rserver "github.com/coinbase/rosetta-sdk-go/server"
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrros/types"
)

var _ rserver.MempoolAPIServicer = (*Server)(nil)

func (s *Server) Mempool(ctx context.Context, req *rtypes.MempoolRequest) (*rtypes.MempoolResponse, *rtypes.Error) {
	mempool, err := s.c.GetRawMempool(ctx, chainjson.GRMAll)
	if err != nil {
		return nil, types.DcrdError(err)
	}
	txs := make([]*rtypes.TransactionIdentifier, len(mempool))
	for i, txh := range mempool {
		txs[i] = &rtypes.TransactionIdentifier{
			Hash: txh.String(),
		}
	}
	return &rtypes.MempoolResponse{
		TransactionIdentifiers: txs,
	}, nil
}

func (s *Server) MempoolTransaction(ctx context.Context, req *rtypes.MempoolTransactionRequest) (*rtypes.MempoolTransactionResponse, *rtypes.Error) {

	var txh chainhash.Hash
	err := chainhash.Decode(&txh, req.TransactionIdentifier.Hash)
	if err != nil {
		return nil, types.ErrInvalidChainHash.RError()
	}

	tx, err := s.c.GetRawTransaction(ctx, &txh)
	if err != nil {
		return nil, types.DcrdError(err)
	}

	// TODO: What if the returned tx has already been mined?

	rtx, err := types.MempoolTxToRosetta(tx.MsgTx(), s.makeInputsFetcher(ctx), s.chainParams)
	if err != nil {
		return nil, types.RError(err)
	}
	return &rtypes.MempoolTransactionResponse{
		Transaction: rtx,
	}, nil
}
