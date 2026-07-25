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

// PoolEdges returns routing metadata from the same immutable version as pool state.
func (s *committedSnapshot) PoolEdges() []quoteunified.PoolEdge {
	if s == nil {
		return nil
	}
	edges := make([]quoteunified.PoolEdge, 0,
		len(s.state.univ3)+len(s.state.pancake)+len(s.state.quickSwap)+len(s.state.univ4)+len(s.state.balancer))
	for _, pool := range s.state.univ3 {
		if pool != nil {
			edges = append(edges, quoteunified.PoolEdge{Version: quoteunified.PoolVersionV3, PoolV3: pool.Address, Token0: pool.Token0, Token1: pool.Token1})
		}
	}
	for _, pool := range s.state.pancake {
		if pool != nil {
			edges = append(edges, quoteunified.PoolEdge{Version: quoteunified.PoolVersionPancakeV3, PoolPancakeV3: pool.Address, Token0: pool.Token0, Token1: pool.Token1})
		}
	}
	for _, pool := range s.state.quickSwap {
		if pool != nil {
			edges = append(edges, quoteunified.PoolEdge{Version: quoteunified.PoolVersionQuickSwapV3, PoolQuickSwapV3: pool.Address, Token0: pool.Token0, Token1: pool.Token1})
		}
	}
	for _, pool := range s.state.univ4 {
		if pool != nil {
			edges = append(edges, quoteunified.PoolEdge{Version: quoteunified.PoolVersionV4, PoolV4: pool.ID, Token0: pool.Key.Currency0, Token1: pool.Key.Currency1})
		}
	}
	for _, pool := range s.state.balancer {
		if pool != nil {
			for i := 0; i < len(pool.Tokens); i++ {
				for j := i + 1; j < len(pool.Tokens); j++ {
					edges = append(edges, quoteunified.PoolEdge{
						Version:         quoteunified.PoolVersionBalancer,
						PoolBalancer:    pool.ID,
						BalancerAddress: pool.Address,
						Token0:          pool.Tokens[i],
						Token1:          pool.Tokens[j],
					})
				}
			}
		}
	}
	return edges
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
