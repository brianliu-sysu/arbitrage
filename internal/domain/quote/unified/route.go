package unified

import (
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// RouteHop is a single hop that may use a V3-style or V4 pool.
type RouteHop struct {
	Version         PoolVersion
	PoolV3          common.Address
	PoolPancakeV3   common.Address
	PoolQuickSwapV3 common.Address
	PoolV4          marketv4.PoolID
	PoolBalancer    marketbalancer.PoolID
	TokenIn         common.Address
	TokenOut        common.Address
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

// NewDirectPancakeV3Route builds a single-hop PancakeSwap V3 route.
func NewDirectPancakeV3Route(pool common.Address, tokenIn, tokenOut common.Address) Route {
	return Route{
		TokenIn:  tokenIn,
		TokenOut: tokenOut,
		Hops: []RouteHop{{
			Version:       PoolVersionPancakeV3,
			PoolPancakeV3: pool,
			TokenIn:       tokenIn,
			TokenOut:      tokenOut,
		}},
	}
}

// NewDirectQuickSwapV3Route builds a single-hop QuickSwap V3 route.
func NewDirectQuickSwapV3Route(pool common.Address, tokenIn, tokenOut common.Address) Route {
	return Route{
		TokenIn:  tokenIn,
		TokenOut: tokenOut,
		Hops: []RouteHop{{
			Version:         PoolVersionQuickSwapV3,
			PoolQuickSwapV3: pool,
			TokenIn:         tokenIn,
			TokenOut:        tokenOut,
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

// NewDirectBalancerRoute builds a single-hop Balancer route.
func NewDirectBalancerRoute(poolID marketbalancer.PoolID, tokenIn, tokenOut common.Address) Route {
	return Route{
		TokenIn:  tokenIn,
		TokenOut: tokenOut,
		Hops: []RouteHop{{
			Version:      PoolVersionBalancer,
			PoolBalancer: poolID,
			TokenIn:      tokenIn,
			TokenOut:     tokenOut,
		}},
	}
}

// Len returns the number of hops in the route.
func (r Route) Len() int {
	return len(r.Hops)
}
