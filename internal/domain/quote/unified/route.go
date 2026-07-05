package unified

import (
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
	"github.com/ethereum/go-ethereum/common"
)

// RouteHop is a single hop that may use either a V3 or V4 pool.
type RouteHop struct {
	Version  PoolVersion
	PoolV3   common.Address
	PoolV4   marketv4.PoolID
	TokenIn  common.Address
	TokenOut common.Address
}

// Route represents a token swap path through V3 and/or V4 pools.
type Route struct {
	TokenIn  common.Address
	TokenOut common.Address
	Hops     []RouteHop
}

// NewDirectV3Route builds a single-hop V3 route.
func NewDirectV3Route(pool common.Address, tokenIn, tokenOut common.Address) Route {
	return Route{
		TokenIn:  tokenIn,
		TokenOut: tokenOut,
		Hops: []RouteHop{{
			Version:  PoolVersionV3,
			PoolV3:   pool,
			TokenIn:  tokenIn,
			TokenOut: tokenOut,
		}},
	}
}

// NewDirectV4Route builds a single-hop V4 route.
func NewDirectV4Route(poolID marketv4.PoolID, tokenIn, tokenOut common.Address) Route {
	return Route{
		TokenIn:  tokenIn,
		TokenOut: tokenOut,
		Hops: []RouteHop{{
			Version:  PoolVersionV4,
			PoolV4:   poolID,
			TokenIn:  tokenIn,
			TokenOut: tokenOut,
		}},
	}
}

// Len returns the number of hops in the route.
func (r Route) Len() int {
	return len(r.Hops)
}
