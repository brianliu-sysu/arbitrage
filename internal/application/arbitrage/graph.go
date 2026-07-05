package arbitrageapp

import (
	"context"
	"fmt"

	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

// BuildUnifiedPoolGraph builds a routing graph from tracked Uniswap V3, Pancake V3, and V4 pools.
func BuildUnifiedPoolGraph(
	ctx context.Context,
	v3Registry marketuniv3.PoolRegistry,
	univ3Pools marketuniv3.PoolRepository,
	pancakeRegistry marketpancake.PoolRegistry,
	pancakePools marketpancake.PoolRepository,
	v4Registry marketuniv4.PoolRegistry,
	univ4Pools marketuniv4.PoolRepository,
) (quoteunified.PoolGraph, error) {
	edges := make([]quoteunified.PoolEdge, 0)

	if v3Registry != nil && univ3Pools != nil {
		addresses, err := v3Registry.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list univ3 pools: %w", err)
		}
		for _, address := range addresses {
			pool, err := univ3Pools.Get(ctx, address)
			if err != nil {
				return nil, fmt.Errorf("load univ3 pool %s: %w", address.Hex(), err)
			}
			if pool == nil {
				continue
			}
			edges = append(edges, quoteunified.PoolEdge{
				Version: quoteunified.PoolVersionV3,
				PoolV3:  pool.Address,
				Token0:  pool.Token0,
				Token1:  pool.Token1,
			})
		}
	}

	if pancakeRegistry != nil && pancakePools != nil {
		addresses, err := pancakeRegistry.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list pancakev3 pools: %w", err)
		}
		for _, address := range addresses {
			pool, err := pancakePools.Get(ctx, address)
			if err != nil {
				return nil, fmt.Errorf("load pancakev3 pool %s: %w", address.Hex(), err)
			}
			if pool == nil {
				continue
			}
			edges = append(edges, quoteunified.PoolEdge{
				Version:       quoteunified.PoolVersionPancakeV3,
				PoolPancakeV3: pool.Address,
				Token0:        pool.Token0,
				Token1:        pool.Token1,
			})
		}
	}

	if v4Registry != nil && univ4Pools != nil {
		poolIDs, err := v4Registry.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list univ4 pools: %w", err)
		}
		for _, poolID := range poolIDs {
			pool, err := univ4Pools.Get(ctx, poolID)
			if err != nil {
				return nil, fmt.Errorf("load univ4 pool %s: %w", poolID.String(), err)
			}
			if pool == nil {
				continue
			}
			edges = append(edges, quoteunified.PoolEdge{
				Version: quoteunified.PoolVersionV4,
				PoolV4:  pool.ID,
				Token0:  pool.Key.Currency0,
				Token1:  pool.Key.Currency1,
			})
		}
	}

	if len(edges) == 0 {
		return nil, fmt.Errorf("no pools available for routing")
	}

	return quoteunified.NewStaticPoolGraph(edges), nil
}

// BuildPoolGraph builds a Uniswap V3-only routing graph from tracked pools.
func BuildPoolGraph(ctx context.Context, registry marketuniv3.PoolRegistry, pools marketuniv3.PoolRepository) (quoteunified.PoolGraph, error) {
	return BuildUnifiedPoolGraph(ctx, registry, pools, nil, nil, nil, nil)
}
