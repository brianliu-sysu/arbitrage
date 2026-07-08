package arbitrage

import (
	"fmt"
	"sort"

	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

const spreadHopCount = 2

type tokenPairKey struct {
	low  common.Address
	high common.Address
}

func canonicalTokenPairKey(tokenA, tokenB common.Address) tokenPairKey {
	if tokenA.Hex() < tokenB.Hex() {
		return tokenPairKey{low: tokenA, high: tokenB}
	}
	return tokenPairKey{low: tokenB, high: tokenA}
}

type spreadPoolRef struct {
	ref    PoolRef
	token0 common.Address
	token1 common.Address
}

func buildSpreadPoolsByPair(edges []quoteunified.PoolEdge) map[tokenPairKey][]spreadPoolRef {
	poolsByPair := make(map[tokenPairKey][]spreadPoolRef)
	seen := make(map[tokenPairKey]map[string]struct{})

	for _, edge := range edges {
		if edge.Token0 == (common.Address{}) || edge.Token1 == (common.Address{}) {
			continue
		}
		if quoteunified.IsWETHBridgeVersion(edge.Version) {
			continue
		}

		key := canonicalTokenPairKey(edge.Token0, edge.Token1)
		ref := poolRefFromUnifiedEdge(edge)
		if ref.Key() == "" {
			continue
		}
		if _, ok := seen[key]; !ok {
			seen[key] = make(map[string]struct{})
		}
		if _, ok := seen[key][ref.Key()]; ok {
			continue
		}
		seen[key][ref.Key()] = struct{}{}
		poolsByPair[key] = append(poolsByPair[key], spreadPoolRef{
			ref:    ref,
			token0: edge.Token0,
			token1: edge.Token1,
		})
	}

	for key, pools := range poolsByPair {
		sort.Slice(pools, func(i, j int) bool {
			return pools[i].ref.Key() < pools[j].ref.Key()
		})
		poolsByPair[key] = pools
	}
	return poolsByPair
}

// FindUnifiedSpreadRoutes discovers A->B->A routes across two distinct pools for the same token pair.
func FindUnifiedSpreadRoutes(graph quoteunified.PoolGraph, startToken common.Address) []quoteunified.Route {
	if graph == nil || startToken == (common.Address{}) {
		return nil
	}

	poolsByPair := buildSpreadPoolsByPair(graph.Edges())
	routes := make([]quoteunified.Route, 0)
	seen := make(map[string]struct{})

	for _, pools := range poolsByPair {
		if len(pools) < 2 {
			continue
		}

		otherToken, ok := spreadOtherToken(pools[0], startToken)
		if !ok {
			continue
		}

		for i := range pools {
			for j := range pools {
				if i == j {
					continue
				}
				route := quoteunified.Route{
					TokenIn:  startToken,
					TokenOut: startToken,
					Hops: []quoteunified.RouteHop{
						spreadHop(pools[i], startToken, otherToken),
						spreadHop(pools[j], otherToken, startToken),
					},
				}
				if !IsUnifiedSpreadRoute(route) {
					continue
				}

				id := UnifiedSpreadRouteIDWithPools(route)
				if _, exists := seen[id]; exists {
					continue
				}
				seen[id] = struct{}{}
				routes = append(routes, route)
			}
		}
	}

	sort.Slice(routes, func(i, j int) bool {
		return UnifiedSpreadRouteIDWithPools(routes[i]) < UnifiedSpreadRouteIDWithPools(routes[j])
	})
	return routes
}

func spreadOtherToken(pool spreadPoolRef, startToken common.Address) (common.Address, bool) {
	if pool.token0 == startToken {
		return pool.token1, true
	}
	if pool.token1 == startToken {
		return pool.token0, true
	}
	return common.Address{}, false
}

func spreadHop(pool spreadPoolRef, tokenIn, tokenOut common.Address) quoteunified.RouteHop {
	hop := quoteunified.RouteHop{
		TokenIn:  tokenIn,
		TokenOut: tokenOut,
	}
	switch pool.ref.Version {
	case quoteunified.PoolVersionV3:
		hop.Version = quoteunified.PoolVersionV3
		hop.PoolV3 = pool.ref.V3
	case quoteunified.PoolVersionPancakeV3:
		hop.Version = quoteunified.PoolVersionPancakeV3
		hop.PoolPancakeV3 = pool.ref.PancakeV3
	case quoteunified.PoolVersionQuickSwapV3:
		hop.Version = quoteunified.PoolVersionQuickSwapV3
		hop.PoolQuickSwapV3 = pool.ref.QuickSwapV3
	case quoteunified.PoolVersionV4:
		hop.Version = quoteunified.PoolVersionV4
		hop.PoolV4 = pool.ref.V4
	case quoteunified.PoolVersionBalancer:
		hop.Version = quoteunified.PoolVersionBalancer
		hop.PoolBalancer = pool.ref.Balancer
	}
	return hop
}

// IsUnifiedSpreadRoute reports whether route is a two-hop cycle through the same token pair on different pools.
func IsUnifiedSpreadRoute(route quoteunified.Route) bool {
	if route.Len() != spreadHopCount {
		return false
	}
	if route.TokenIn != route.TokenOut || route.TokenIn == (common.Address{}) {
		return false
	}

	hop0 := route.Hops[0]
	hop1 := route.Hops[1]
	if hop0.TokenIn != route.TokenIn || hop0.TokenOut != hop1.TokenIn || hop1.TokenOut != route.TokenIn {
		return false
	}
	if hop0.TokenOut == route.TokenIn {
		return false
	}

	pair0 := canonicalTokenPairKey(hop0.TokenIn, hop0.TokenOut)
	pair1 := canonicalTokenPairKey(hop1.TokenIn, hop1.TokenOut)
	if pair0 != pair1 {
		return false
	}

	return PoolRefFromHop(hop0).Key() != PoolRefFromHop(hop1).Key()
}

// UnifiedSpreadRouteID returns a stable token-only identifier for a spread route.
func UnifiedSpreadRouteID(route quoteunified.Route) string {
	if !IsUnifiedSpreadRoute(route) {
		return fmt.Sprintf("spread-%s", route.TokenIn.Hex())
	}
	other := route.Hops[0].TokenOut
	if route.Hops[0].TokenIn == route.TokenIn {
		return fmt.Sprintf("spread-%s-%s", route.TokenIn.Hex(), other.Hex())
	}
	return fmt.Sprintf("spread-%s-%s", other.Hex(), route.TokenIn.Hex())
}

// UnifiedSpreadRouteIDWithPools extends the token-only id with pool refs for uniqueness.
func UnifiedSpreadRouteIDWithPools(route quoteunified.Route) string {
	if !IsUnifiedSpreadRoute(route) {
		return UnifiedSpreadRouteID(route)
	}
	parts := make([]string, 0, len(route.Hops))
	for _, hop := range route.Hops {
		parts = append(parts, PoolRefFromHop(hop).Key())
	}
	return fmt.Sprintf("%s|%s", UnifiedSpreadRouteID(route), joinKeys(parts))
}
