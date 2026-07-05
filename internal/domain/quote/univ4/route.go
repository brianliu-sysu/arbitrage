package univ4

import (
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// RouteHop is a single hop in a V4 swap route.
type RouteHop struct {
	PoolID   marketv4.PoolID
	TokenIn  common.Address
	TokenOut common.Address
}

// Route represents a token swap path through one or more V4 pools.
type Route struct {
	TokenIn  common.Address
	TokenOut common.Address
	Hops     []RouteHop
}

// NewDirectRoute builds a single-hop route.
func NewDirectRoute(poolID marketv4.PoolID, tokenIn, tokenOut common.Address) Route {
	return Route{
		TokenIn:  tokenIn,
		TokenOut: tokenOut,
		Hops: []RouteHop{{
			PoolID:   poolID,
			TokenIn:  tokenIn,
			TokenOut: tokenOut,
		}},
	}
}

// Len returns the number of hops in the route.
func (r Route) Len() int {
	return len(r.Hops)
}
