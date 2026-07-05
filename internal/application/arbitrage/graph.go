package arbitrageapp

import (
	"context"
	"fmt"

	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

// BuildUnifiedPoolGraph builds a routing graph from tracked V3 and V4 pools.
func BuildUnifiedPoolGraph(
	ctx context.Context,
	v3Registry marketv3.PoolRegistry,
	v3Pools marketv3.PoolRepository,
	v4Registry marketv4.PoolRegistry,
	v4Pools marketv4.PoolRepository,
) (quoteunified.PoolGraph, error) {
	edges := make([]quoteunified.PoolEdge, 0)

	if v3Registry != nil && v3Pools != nil {
		addresses, err := v3Registry.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list v3 pools: %w", err)
		}
		for _, address := range addresses {
			pool, err := v3Pools.Get(ctx, address)
			if err != nil {
				return nil, fmt.Errorf("load v3 pool %s: %w", address.Hex(), err)
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

	if v4Registry != nil && v4Pools != nil {
		poolIDs, err := v4Registry.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list v4 pools: %w", err)
		}
		for _, poolID := range poolIDs {
			pool, err := v4Pools.Get(ctx, poolID)
			if err != nil {
				return nil, fmt.Errorf("load v4 pool %s: %w", poolID.String(), err)
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

// BuildPoolGraph builds a V3-only routing graph from tracked pools.
func BuildPoolGraph(ctx context.Context, registry marketv3.PoolRegistry, pools marketv3.PoolRepository) (quoteunified.PoolGraph, error) {
	return BuildUnifiedPoolGraph(ctx, registry, pools, nil, nil)
}
