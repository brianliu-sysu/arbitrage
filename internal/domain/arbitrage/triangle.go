package arbitrage

import (
	"sort"

	domainquote "github.com/brianliu-sysu/uniswapv3/internal/domain/quote"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

// FindTriangleRoutes discovers A->B->C->A routes with exactly three hops on a V3-only graph.
func FindTriangleRoutes(graph domainquote.PoolGraph, startToken common.Address) []domainquote.Route {
	unifiedRoutes := FindUnifiedTriangleRoutes(v3GraphToUnified(graph), startToken)
	routes := make([]domainquote.Route, 0, len(unifiedRoutes))
	for _, route := range unifiedRoutes {
		routes = append(routes, unifiedRouteToV3(route))
	}
	sort.Slice(routes, func(i, j int) bool {
		return TriangleRouteID(routes[i]) < TriangleRouteID(routes[j])
	})
	return routes
}

// IsTriangleRoute reports whether route is a three-hop cycle through three distinct tokens.
func IsTriangleRoute(route domainquote.Route) bool {
	return IsUnifiedTriangleRoute(v3RouteToUnified(route))
}

// TriangleRouteID returns a stable identifier for a triangle route.
func TriangleRouteID(route domainquote.Route) string {
	return UnifiedTriangleRouteID(v3RouteToUnified(route))
}

func triangleTokens(route domainquote.Route) []common.Address {
	return unifiedTriangleTokens(v3RouteToUnified(route))
}

// FindUnifiedTriangleRoutesOnV3Graph discovers triangle routes on a V3-only graph as unified routes.
func FindUnifiedTriangleRoutesOnV3Graph(graph domainquote.PoolGraph, startToken common.Address) []quoteunified.Route {
	return FindUnifiedTriangleRoutes(v3GraphToUnified(graph), startToken)
}
