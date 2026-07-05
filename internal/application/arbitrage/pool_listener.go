package arbitrageapp

import (
	"context"

	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// V4PoolListener adapts arbitrage services to the V4 sync ChangedPoolsListener interface.
type V4PoolListener struct {
	Services *Services
}

func (l V4PoolListener) OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []marketuniv4.PoolID) error {
	if l.Services == nil {
		return nil
	}
	return l.Services.OnV4PoolsChanged(ctx, blockNumber, pools)
}

// PancakePoolListener adapts arbitrage services to the Pancake V3 sync ChangedPoolsListener interface.
type PancakePoolListener struct {
	Services *Services
}

func (l PancakePoolListener) OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error {
	if l.Services == nil {
		return nil
	}
	return l.Services.OnPancakePoolsChanged(ctx, blockNumber, pools)
}

func (s *Services) notifyPoolsChanged(
	ctx context.Context,
	blockNumber uint64,
	univ3Pools []common.Address,
	pancakePools []common.Address,
	univ4Pools []marketuniv4.PoolID,
) error {
	routes := s.Scan.FindAffected(univ3Pools, pancakePools, univ4Pools)
	opportunities, err := s.Opportunities.Generate(ctx, GenerateRequest{
		BlockNumber: blockNumber,
		Routes:      routes,
	})
	if err != nil {
		return err
	}
	return s.Publish.Publish(ctx, opportunities)
}

// OnPoolsChanged implements the Uniswap V3 sync ChangedPoolsListener interface.
func (s *Services) OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error {
	return s.notifyPoolsChanged(ctx, blockNumber, pools, nil, nil)
}

// OnPancakePoolsChanged handles PancakeSwap V3 pool updates after a block is applied.
func (s *Services) OnPancakePoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error {
	return s.notifyPoolsChanged(ctx, blockNumber, nil, pools, nil)
}

// OnV4PoolsChanged handles V4 pool updates after a block is applied.
func (s *Services) OnV4PoolsChanged(ctx context.Context, blockNumber uint64, pools []marketuniv4.PoolID) error {
	return s.notifyPoolsChanged(ctx, blockNumber, nil, nil, pools)
}
