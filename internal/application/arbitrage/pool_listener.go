package arbitrageapp

import (
	"context"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
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

func (s *Services) reportApplied(ctx context.Context, report ProtocolBlockReport) error {
	if s == nil || s.Coordinator == nil {
		return nil
	}
	return s.Coordinator.ReportApplied(ctx, report)
}

// OnPoolsChanged implements the Uniswap V3 sync ChangedPoolsListener interface.
func (s *Services) OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error {
	return s.reportApplied(ctx, ProtocolBlockReport{
		Protocol:    SyncProtocolUniv3,
		BlockNumber: blockNumber,
		Univ3Pools:  pools,
	})
}

// OnPancakePoolsChanged handles PancakeSwap V3 pool updates after a block is applied.
func (s *Services) OnPancakePoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error {
	return s.reportApplied(ctx, ProtocolBlockReport{
		Protocol:     SyncProtocolPancakeV3,
		BlockNumber:  blockNumber,
		PancakePools: pools,
	})
}

// OnQuickSwapPoolsChanged handles QuickSwap V3 pool updates after a block is applied.
func (s *Services) OnQuickSwapPoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error {
	return s.reportApplied(ctx, ProtocolBlockReport{
		Protocol:       SyncProtocolQuickSwapV3,
		BlockNumber:    blockNumber,
		QuickSwapPools: pools,
	})
}

// OnV4PoolsChanged handles V4 pool updates after a block is applied.
func (s *Services) OnV4PoolsChanged(ctx context.Context, blockNumber uint64, pools []marketuniv4.PoolID) error {
	return s.reportApplied(ctx, ProtocolBlockReport{
		Protocol:    SyncProtocolUniv4,
		BlockNumber: blockNumber,
		Univ4Pools:  pools,
	})
}

// OnBalancerPoolsChanged handles Balancer pool updates after a block is applied.
func (s *Services) OnBalancerPoolsChanged(ctx context.Context, blockNumber uint64, pools []marketbalancer.PoolID) error {
	return s.reportApplied(ctx, ProtocolBlockReport{
		Protocol:      SyncProtocolBalancer,
		BlockNumber:   blockNumber,
		BalancerPools: pools,
	})
}
