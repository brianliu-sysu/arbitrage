package arbitrage

import (
	"fmt"
	"sort"

	domainquote "github.com/brianliu-sysu/uniswapv3/internal/domain/quote"
	"github.com/ethereum/go-ethereum/common"
)

type triangleEdge struct {
	pool  common.Address
	token common.Address
	peer  common.Address
}

func buildTriangleAdjacency(edges []domainquote.PoolEdge) map[common.Address][]triangleEdge {
	adjacency := make(map[common.Address][]triangleEdge)
	for _, edge := range edges {
		adjacency[edge.Token0] = append(adjacency[edge.Token0], triangleEdge{
			pool:  edge.PoolAddress,
			token: edge.Token0,
			peer:  edge.Token1,
		})
		adjacency[edge.Token1] = append(adjacency[edge.Token1], triangleEdge{
			pool:  edge.PoolAddress,
			token: edge.Token1,
			peer:  edge.Token0,
		})
	}
	return adjacency
}

// FindTriangleRoutes discovers A->B->C->A routes with exactly three hops.
func FindTriangleRoutes(graph domainquote.PoolGraph, startToken common.Address) []domainquote.Route {
	if graph == nil || startToken == (common.Address{}) {
		return nil
	}

	adjacency := buildTriangleAdjacency(graph.Edges())
	firstHopEdges := adjacency[startToken]
	if len(firstHopEdges) == 0 {
		return nil
	}

	routes := make([]domainquote.Route, 0)
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

				route := domainquote.Route{
					TokenIn:  startToken,
					TokenOut: startToken,
					Hops: []domainquote.RouteHop{
						{PoolAddress: hopAB.pool, TokenIn: startToken, TokenOut: tokenB},
						{PoolAddress: hopBC.pool, TokenIn: tokenB, TokenOut: tokenC},
						{PoolAddress: hopCA.pool, TokenIn: tokenC, TokenOut: startToken},
					},
				}
				if !IsTriangleRoute(route) {
					continue
				}

				id := TriangleRouteID(route)
				if _, ok := seen[id]; ok {
					continue
				}
				seen[id] = struct{}{}
				routes = append(routes, route)
			}
		}
	}

	sort.Slice(routes, func(i, j int) bool {
		return TriangleRouteID(routes[i]) < TriangleRouteID(routes[j])
	})
	return routes
}

// IsTriangleRoute reports whether route is a three-hop cycle through three distinct tokens.
func IsTriangleRoute(route domainquote.Route) bool {
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

// TriangleRouteID returns a stable identifier for a triangle route.
func TriangleRouteID(route domainquote.Route) string {
	tokens := triangleTokens(route)
	if len(tokens) != 3 {
		return fmt.Sprintf("triangle-%s", route.TokenIn.Hex())
	}
	return fmt.Sprintf("triangle-%s-%s-%s", tokens[0].Hex(), tokens[1].Hex(), tokens[2].Hex())
}

func triangleTokens(route domainquote.Route) []common.Address {
	if !IsTriangleRoute(route) {
		return nil
	}
	return []common.Address{route.TokenIn, route.Hops[0].TokenOut, route.Hops[1].TokenOut}
}
