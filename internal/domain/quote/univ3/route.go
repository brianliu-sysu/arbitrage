package univ3

import "github.com/ethereum/go-ethereum/common"

// RouteHop is a single hop in a V3 swap route.
type RouteHop struct {
	PoolAddress common.Address
	TokenIn     common.Address
	TokenOut    common.Address
}

// Route represents a token swap path through one or more V3 pools.
type Route struct {
	TokenIn  common.Address
	TokenOut common.Address
	Hops     []RouteHop
}

// NewDirectRoute builds a single-hop route.
func NewDirectRoute(pool common.Address, tokenIn, tokenOut common.Address) Route {
	return Route{
		TokenIn:  tokenIn,
		TokenOut: tokenOut,
		Hops: []RouteHop{{
			PoolAddress: pool,
			TokenIn:     tokenIn,
			TokenOut:    tokenOut,
		}},
	}
}

// Len returns the number of hops in the route.
func (r Route) Len() int {
	return len(r.Hops)
}
