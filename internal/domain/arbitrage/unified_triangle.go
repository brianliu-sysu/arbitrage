package arbitrage

import (
	"fmt"
	"sort"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

type unifiedTriangleEdge struct {
	poolRef PoolRef
	token   common.Address
	peer    common.Address
}

func buildUnifiedTriangleAdjacency(edges []quoteunified.PoolEdge) map[common.Address][]unifiedTriangleEdge {
	adjacency := make(map[common.Address][]unifiedTriangleEdge)
	for _, edge := range edges {
		refV0 := poolRefFromUnifiedEdge(edge)
		adjacency[edge.Token0] = append(adjacency[edge.Token0], unifiedTriangleEdge{
			poolRef: refV0,
			token:   edge.Token0,
			peer:    edge.Token1,
		})
		adjacency[edge.Token1] = append(adjacency[edge.Token1], unifiedTriangleEdge{
			poolRef: refV0,
			token:   edge.Token1,
			peer:    edge.Token0,
		})
	}
	appendDirectedWETHBridgeTriangleAdjacency(adjacency)
	return adjacency
}

func appendDirectedWETHBridgeTriangleAdjacency(adjacency map[common.Address][]unifiedTriangleEdge) {
	weth := asset.MainnetWETH
	native := common.Address{}
	adjacency[weth] = append(adjacency[weth], unifiedTriangleEdge{
		poolRef: PoolRef{Version: quoteunified.PoolVersionUnwrapWETH},
		token:   weth,
		peer:    native,
	})
	adjacency[native] = append(adjacency[native], unifiedTriangleEdge{
		poolRef: PoolRef{Version: quoteunified.PoolVersionWrapWETH},
		token:   native,
		peer:    weth,
	})
}

func poolRefFromUnifiedEdge(edge quoteunified.PoolEdge) PoolRef {
	switch edge.Version {
	case quoteunified.PoolVersionV3:
		return PoolRefFromV3(edge.PoolV3)
	case quoteunified.PoolVersionPancakeV3:
		return PoolRefFromPancakeV3(edge.PoolPancakeV3)
	case quoteunified.PoolVersionV4:
		return PoolRefFromV4(edge.PoolV4)
	case quoteunified.PoolVersionUnwrapWETH:
		return PoolRef{Version: quoteunified.PoolVersionUnwrapWETH}
	case quoteunified.PoolVersionWrapWETH:
		return PoolRef{Version: quoteunified.PoolVersionWrapWETH}
	default:
		return PoolRef{}
	}
}

// FindUnifiedTriangleRoutes discovers A->B->C->A routes with exactly three hops.
func FindUnifiedTriangleRoutes(graph quoteunified.PoolGraph, startToken common.Address) []quoteunified.Route {
	if graph == nil || startToken == (common.Address{}) {
		return nil
	}

	adjacency := buildUnifiedTriangleAdjacency(graph.Edges())
	firstHopEdges := adjacency[startToken]
	if len(firstHopEdges) == 0 {
		return nil
	}

	routes := make([]quoteunified.Route, 0)
	seen := make(map[string]struct{})

	for _, hopAB := range firstHopEdges {
		tokenB := hopAB.peer
		if tokenB == startToken {
			continue
		}

		for _, hopBC := range adjacency[tokenB] {
			tokenC := hopBC.peer
			if tokenC == startToken || tokenC == tokenB {
				continue
			}

			for _, hopCA := range adjacency[tokenC] {
				if hopCA.peer != startToken {
					continue
				}

				route := quoteunified.Route{
					TokenIn:  startToken,
					TokenOut: startToken,
					Hops: []quoteunified.RouteHop{
						unifiedHop(hopAB),
						unifiedHop(hopBC),
						unifiedHop(hopCA),
					},
				}
				if !IsUnifiedTriangleRoute(route) {
					continue
				}

				id := UnifiedTriangleRouteID(route)
				if _, ok := seen[id]; ok {
					continue
				}
				seen[id] = struct{}{}
				routes = append(routes, route)
			}
		}
	}

	sort.Slice(routes, func(i, j int) bool {
		return UnifiedTriangleRouteID(routes[i]) < UnifiedTriangleRouteID(routes[j])
	})
	return routes
}

func unifiedHop(edge unifiedTriangleEdge) quoteunified.RouteHop {
	hop := quoteunified.RouteHop{
		TokenIn:  edge.token,
		TokenOut: edge.peer,
	}
	switch edge.poolRef.Version {
	case quoteunified.PoolVersionV3:
		hop.Version = quoteunified.PoolVersionV3
		hop.PoolV3 = edge.poolRef.V3
	case quoteunified.PoolVersionPancakeV3:
		hop.Version = quoteunified.PoolVersionPancakeV3
		hop.PoolPancakeV3 = edge.poolRef.PancakeV3
	case quoteunified.PoolVersionV4:
		hop.Version = quoteunified.PoolVersionV4
		hop.PoolV4 = edge.poolRef.V4
	case quoteunified.PoolVersionUnwrapWETH, quoteunified.PoolVersionWrapWETH:
		hop.Version = edge.poolRef.Version
	}
	return hop
}

// IsUnifiedTriangleRoute reports whether route is a three-hop cycle through three distinct tokens.
func IsUnifiedTriangleRoute(route quoteunified.Route) bool {
	if route.Len() != 3 {
		return false
	}
	if route.TokenIn != route.TokenOut {
		return false
	}

	tokens := []common.Address{route.TokenIn}
	for _, hop := range route.Hops {
		if hop.TokenIn != tokens[len(tokens)-1] {
			return false
		}
		tokens = append(tokens, hop.TokenOut)
	}
	if len(tokens) != 4 || tokens[0] != tokens[3] {
		return false
	}

	start, tokenB, tokenC := tokens[0], tokens[1], tokens[2]
	return start != tokenB && start != tokenC && tokenB != tokenC
}

// UnifiedTriangleRouteID returns a stable identifier for a unified triangle route.
func UnifiedTriangleRouteID(route quoteunified.Route) string {
	tokens := unifiedTriangleTokens(route)
	if len(tokens) != 3 {
		return fmt.Sprintf("triangle-%s", route.TokenIn.Hex())
	}
	return fmt.Sprintf("triangle-%s-%s-%s", tokens[0].Hex(), tokens[1].Hex(), tokens[2].Hex())
}

func unifiedTriangleTokens(route quoteunified.Route) []common.Address {
	if !IsUnifiedTriangleRoute(route) {
		return nil
	}
	return []common.Address{route.TokenIn, route.Hops[0].TokenOut, route.Hops[1].TokenOut}
}

// UnifiedTriangleRouteIDWithPools extends the token-only id with pool refs for uniqueness.
func UnifiedTriangleRouteIDWithPools(route quoteunified.Route) string {
	if !IsUnifiedTriangleRoute(route) {
		return UnifiedTriangleRouteID(route)
	}
	parts := make([]string, 0, len(route.Hops))
	for _, hop := range route.Hops {
		parts = append(parts, PoolRefFromHop(hop).Key())
	}
	return fmt.Sprintf("%s|%s", UnifiedTriangleRouteID(route), joinKeys(parts))
}

func joinKeys(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += ">" + parts[i]
	}
	return result
}

// MatchesUnifiedStrategy reports whether a unified route satisfies the strategy constraints.
func MatchesUnifiedStrategy(strategy Strategy, route quoteunified.Route) bool {
	if route.TokenIn != strategy.StartToken || route.TokenOut != strategy.StartToken {
		return false
	}

	switch strategy.Kind {
	case StrategyKindCycle:
		return route.Len() > 0 && route.Len() <= strategy.MaxHops
	case StrategyKindTriangle:
		return IsUnifiedTriangleRoute(route)
	default:
		return false
	}
}
