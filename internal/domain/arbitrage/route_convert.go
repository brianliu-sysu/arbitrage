package arbitrage

import (
	domainquote "github.com/brianliu-sysu/uniswapv3/internal/domain/quote"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
)

// V3GraphToUnified converts a V3-only pool graph to a unified graph.
func V3GraphToUnified(graph domainquote.PoolGraph) quoteunified.PoolGraph {
	return v3GraphToUnified(graph)
}

func v3GraphToUnified(graph domainquote.PoolGraph) quoteunified.PoolGraph {
	if graph == nil {
		return nil
	}
	edges := make([]quoteunified.PoolEdge, 0, len(graph.Edges()))
	for _, edge := range graph.Edges() {
		edges = append(edges, quoteunified.PoolEdge{
			Version: quoteunified.PoolVersionV3,
			PoolV3:  edge.PoolAddress,
			Token0:  edge.Token0,
			Token1:  edge.Token1,
		})
	}
	return quoteunified.NewStaticPoolGraph(edges)
}

func unifiedRouteToV3(route quoteunified.Route) domainquote.Route {
	hops := make([]domainquote.RouteHop, 0, len(route.Hops))
	for _, hop := range route.Hops {
		hops = append(hops, domainquote.RouteHop{
			PoolAddress: hop.PoolV3,
			TokenIn:     hop.TokenIn,
			TokenOut:    hop.TokenOut,
		})
	}
	return domainquote.Route{
		TokenIn:  route.TokenIn,
		TokenOut: route.TokenOut,
		Hops:     hops,
	}
}

func v3RouteToUnified(route domainquote.Route) quoteunified.Route {
	hops := make([]quoteunified.RouteHop, 0, len(route.Hops))
	for _, hop := range route.Hops {
		hops = append(hops, quoteunified.RouteHop{
			Version:  quoteunified.PoolVersionV3,
			PoolV3:   hop.PoolAddress,
			TokenIn:  hop.TokenIn,
			TokenOut: hop.TokenOut,
		})
	}
	return quoteunified.Route{
		TokenIn:  route.TokenIn,
		TokenOut: route.TokenOut,
		Hops:     hops,
	}
}
