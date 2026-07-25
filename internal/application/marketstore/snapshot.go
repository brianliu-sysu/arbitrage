package marketstore

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/application/committedmarket"
	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

type committedSnapshot struct {
	state snapshot
}

// Snapshot captures the currently published immutable market version.
func (v *View) Snapshot() committedmarket.Snapshot {
	if v == nil {
		return &committedSnapshot{state: emptySnapshot()}
	}
	v.mu.RLock()
	active := v.active
	v.mu.RUnlock()
	return &committedSnapshot{state: active}
}

func (s *committedSnapshot) Version() domainchain.MarketVersion {
	if s == nil {
		return domainchain.MarketVersion{}
	}
	return s.state.version
}

func (s *committedSnapshot) LoadRoutePools(
	ctx context.Context,
	route quoteunified.Route,
) (quoteunified.RoutePools, error) {
	pools := quoteunified.RoutePools{}
	if s == nil {
		return pools, fmt.Errorf("committed market snapshot is nil")
	}
	for _, hop := range route.Hops {
		if err := ctx.Err(); err != nil {
			return quoteunified.RoutePools{}, err
		}
		switch hop.Version {
		case quoteunified.PoolVersionV3:
			pool := s.state.univ3[hop.PoolV3]
			if pool == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("univ3 pool %s is not in committed market", hop.PoolV3.Hex())
			}
			if pools.V3 == nil {
				pools.V3 = make(map[common.Address]*marketuniv3.Pool)
			}
			pools.V3[hop.PoolV3] = pool.Clone()
		case quoteunified.PoolVersionPancakeV3:
			pool := s.state.pancake[hop.PoolPancakeV3]
			if pool == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("pancakev3 pool %s is not in committed market", hop.PoolPancakeV3.Hex())
			}
			if pools.PancakeV3 == nil {
				pools.PancakeV3 = make(map[common.Address]*marketpancake.Pool)
			}
			pools.PancakeV3[hop.PoolPancakeV3] = pool.Clone()
		case quoteunified.PoolVersionQuickSwapV3:
			pool := s.state.quickSwap[hop.PoolQuickSwapV3]
			if pool == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("quickswapv3 pool %s is not in committed market", hop.PoolQuickSwapV3.Hex())
			}
			if pools.QuickSwapV3 == nil {
				pools.QuickSwapV3 = make(map[common.Address]*marketquick.Pool)
			}
			pools.QuickSwapV3[hop.PoolQuickSwapV3] = pool.Clone()
		case quoteunified.PoolVersionV4:
			pool := s.state.univ4[hop.PoolV4]
			if pool == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("univ4 pool %s is not in committed market", hop.PoolV4.String())
			}
			if pools.V4 == nil {
				pools.V4 = make(map[marketuniv4.PoolID]*marketuniv4.Pool)
			}
			pools.V4[hop.PoolV4] = pool.Clone()
		case quoteunified.PoolVersionBalancer:
			pool := s.state.balancer[hop.PoolBalancer]
			if pool == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("balancer pool %s is not in committed market", hop.PoolBalancer.String())
			}
			if pools.Balancer == nil {
				pools.Balancer = make(map[marketbalancer.PoolID]*marketbalancer.Pool)
			}
			pools.Balancer[hop.PoolBalancer] = pool.Clone()
		case quoteunified.PoolVersionWrapWETH, quoteunified.PoolVersionUnwrapWETH:
			continue
		default:
			return quoteunified.RoutePools{}, fmt.Errorf("unsupported pool version %d", hop.Version)
		}
	}
	return pools, nil
}
