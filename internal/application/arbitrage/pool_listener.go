package arbitrageapp

import (
	"context"

	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
	"github.com/ethereum/go-ethereum/common"
)

// V4PoolListener adapts arbitrage services to the V4 sync ChangedPoolsListener interface.
type V4PoolListener struct {
	Services *Services
}

func (l V4PoolListener) OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []marketv4.PoolID) error {
	if l.Services == nil {
		return nil
	}
	return l.Services.OnV4PoolsChanged(ctx, blockNumber, pools)
}

func (s *Services) notifyPoolsChanged(
	ctx context.Context,
	blockNumber uint64,
	v3Pools []common.Address,
	v4Pools []marketv4.PoolID,
) error {
	routes := s.Scan.FindAffected(v3Pools, v4Pools)
	opportunities, err := s.Opportunities.Generate(ctx, GenerateRequest{
		BlockNumber: blockNumber,
		Routes:      routes,
	})
	if err != nil {
		return err
	}
	return s.Publish.Publish(ctx, opportunities)
}

// OnPoolsChanged implements the V3 sync ChangedPoolsListener interface.
func (s *Services) OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error {
	return s.notifyPoolsChanged(ctx, blockNumber, pools, nil)
}

// OnV4PoolsChanged handles V4 pool updates after a block is applied.
func (s *Services) OnV4PoolsChanged(ctx context.Context, blockNumber uint64, pools []marketv4.PoolID) error {
	return s.notifyPoolsChanged(ctx, blockNumber, nil, pools)
}
