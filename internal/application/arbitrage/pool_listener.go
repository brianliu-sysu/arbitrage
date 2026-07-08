package arbitrageapp

import (
	"context"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
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

// QuickSwapPoolListener adapts arbitrage services to the QuickSwap V3 sync ChangedPoolsListener interface.
type QuickSwapPoolListener struct {
	Services *Services
}

func (l QuickSwapPoolListener) OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error {
	if l.Services == nil {
		return nil
	}
	return l.Services.OnQuickSwapPoolsChanged(ctx, blockNumber, pools)
}

// BalancerPoolListener adapts arbitrage services to the Balancer sync ChangedPoolsListener interface.
type BalancerPoolListener struct {
	Services *Services
}

func (l BalancerPoolListener) OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []marketbalancer.PoolID) error {
	if l.Services == nil {
		return nil
	}
	return l.Services.OnBalancerPoolsChanged(ctx, blockNumber, pools)
}

func (s *Services) notifyPoolsChanged(
	ctx context.Context,
	blockNumber uint64,
	univ3Pools []common.Address,
	pancakePools []common.Address,
	quickSwapPools []common.Address,
	univ4Pools []marketuniv4.PoolID,
	balancerPools []marketbalancer.PoolID,
) error {
	routes := s.Scan.FindAffected(univ3Pools, pancakePools, quickSwapPools, univ4Pools, balancerPools)
	logger := s.logger
	if logger == nil {
		logger = zap.NewNop()
	}
	logger.Debug("arbitrage pools changed",
		zap.Uint64("block", blockNumber),
		zap.Int("univ3_pools", len(univ3Pools)),
		zap.Int("pancakev3_pools", len(pancakePools)),
		zap.Int("quickswapv3_pools", len(quickSwapPools)),
		zap.Int("univ4_pools", len(univ4Pools)),
		zap.Int("balancer_pools", len(balancerPools)),
		zap.Int("affected_routes", len(routes)),
	)
	opportunities, err := s.Opportunities.Generate(ctx, GenerateRequest{
		BlockNumber: blockNumber,
		Routes:      routes,
	})
	if err != nil {
		return err
	}
	logger.Debug("arbitrage opportunities generated",
		zap.Uint64("block", blockNumber),
		zap.Int("affected_routes", len(routes)),
		zap.Int("opportunities", len(opportunities)),
	)
	return s.Publish.Publish(ctx, opportunities)
}

// OnPoolsChanged implements the Uniswap V3 sync ChangedPoolsListener interface.
func (s *Services) OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error {
	return s.notifyPoolsChanged(ctx, blockNumber, pools, nil, nil, nil, nil)
}

// OnPancakePoolsChanged handles PancakeSwap V3 pool updates after a block is applied.
func (s *Services) OnPancakePoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error {
	return s.notifyPoolsChanged(ctx, blockNumber, nil, pools, nil, nil, nil)
}

// OnQuickSwapPoolsChanged handles QuickSwap V3 pool updates after a block is applied.
func (s *Services) OnQuickSwapPoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error {
	return s.notifyPoolsChanged(ctx, blockNumber, nil, nil, pools, nil, nil)
}

// OnV4PoolsChanged handles V4 pool updates after a block is applied.
func (s *Services) OnV4PoolsChanged(ctx context.Context, blockNumber uint64, pools []marketuniv4.PoolID) error {
	return s.notifyPoolsChanged(ctx, blockNumber, nil, nil, nil, pools, nil)
}

// OnBalancerPoolsChanged handles Balancer pool updates after a block is applied.
func (s *Services) OnBalancerPoolsChanged(ctx context.Context, blockNumber uint64, pools []marketbalancer.PoolID) error {
	return s.notifyPoolsChanged(ctx, blockNumber, nil, nil, nil, nil, pools)
}
