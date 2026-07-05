package univ4

import (
	"fmt"

	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
	"github.com/ethereum/go-ethereum/common"
)

// PoolEdge describes a pool connection in the V4 routing graph.
type PoolEdge struct {
	PoolID marketv4.PoolID
	Token0 common.Address
	Token1 common.Address
}

// PoolGraph provides pool connectivity for route discovery.
type PoolGraph interface {
	Edges() []PoolEdge
}

// RouteService finds routes through a pool graph without quoting them.
type RouteService struct {
	graph   PoolGraph
	maxHops int
}

func NewRouteService(graph PoolGraph, maxHops int) *RouteService {
	if maxHops <= 0 {
		maxHops = 3
	}
	return &RouteService{
		graph:   graph,
		maxHops: maxHops,
	}
}

// FindRoutes returns all simple routes from tokenIn to tokenOut up to maxHops.
func (rs *RouteService) FindRoutes(tokenIn, tokenOut common.Address) ([]Route, error) {
	if tokenIn == tokenOut {
		return nil, fmt.Errorf("tokenIn and tokenOut must differ")
	}
	if rs.graph == nil {
		return nil, fmt.Errorf("pool graph is nil")
	}

	adjacency := buildAdjacency(rs.graph.Edges())
	if len(adjacency[tokenIn]) == 0 {
		return nil, nil
	}

	type searchState struct {
		token common.Address
		route Route
		seen  map[common.Address]struct{}
	}

	queue := []searchState{{
		token: tokenIn,
		route: Route{TokenIn: tokenIn, TokenOut: tokenOut},
		seen:  map[common.Address]struct{}{tokenIn: {}},
	}}

	var routes []Route
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.route.Len() >= rs.maxHops {
			continue
		}

		for _, edge := range adjacency[current.token] {
			nextToken := edge.otherToken(current.token)
			if _, visited := current.seen[nextToken]; visited {
				continue
			}

			hop := RouteHop{
				PoolID:   edge.pool,
				TokenIn:  current.token,
				TokenOut: nextToken,
			}
			nextRoute := Route{
				TokenIn:  tokenIn,
				TokenOut: tokenOut,
				Hops:     append(append([]RouteHop(nil), current.route.Hops...), hop),
			}

			if nextToken == tokenOut {
				routes = append(routes, nextRoute)
				continue
			}

			nextSeen := make(map[common.Address]struct{}, len(current.seen)+1)
			for token := range current.seen {
				nextSeen[token] = struct{}{}
			}
			nextSeen[nextToken] = struct{}{}

			queue = append(queue, searchState{
				token: nextToken,
				route: nextRoute,
				seen:  nextSeen,
			})
		}
	}

	return routes, nil
}

type adjacencyEdge struct {
	pool marketv4.PoolID
	a    common.Address
	b    common.Address
}

func (e adjacencyEdge) otherToken(token common.Address) common.Address {
	if token == e.a {
		return e.b
	}
	return e.a
}

func buildAdjacency(edges []PoolEdge) map[common.Address][]adjacencyEdge {
	adjacency := make(map[common.Address][]adjacencyEdge)
	for _, edge := range edges {
		link := adjacencyEdge{
			pool: edge.PoolID,
			a:    edge.Token0,
			b:    edge.Token1,
		}
		adjacency[edge.Token0] = append(adjacency[edge.Token0], link)
		adjacency[edge.Token1] = append(adjacency[edge.Token1], link)
	}
	return adjacency
}

// StaticPoolGraph is an in-memory PoolGraph backed by a fixed edge list.
type StaticPoolGraph struct {
	edges []PoolEdge
}

func NewStaticPoolGraph(edges []PoolEdge) *StaticPoolGraph {
	copied := make([]PoolEdge, len(edges))
	copy(copied, edges)
	return &StaticPoolGraph{edges: copied}
}

func (g *StaticPoolGraph) Edges() []PoolEdge {
	copied := make([]PoolEdge, len(g.edges))
	copy(copied, g.edges)
	return copied
}
