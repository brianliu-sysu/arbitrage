package quote

import (
	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	quoteuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ3"
)

type (
	// QuoteMode selects exact-input or exact-output quoting.
	QuoteMode = quoteshared.QuoteMode

	// QuoteResult is the outcome of quoting a pool or route.
	QuoteResult = quoteshared.QuoteResult

	// SwapStepResult holds the outcome of a single swap step.
	SwapStepResult = quoteshared.SwapStepResult

	// QuoteService quotes swaps against Uniswap V3 pool state.
	QuoteService = quoteuniv3.QuoteService

	// RouteHop is a single hop in a V3 swap route.
	RouteHop = quoteuniv3.RouteHop

	// Route represents a token swap path through one or more V3 pools.
	Route = quoteuniv3.Route

	// PoolEdge describes a pool connection in the V3 routing graph.
	PoolEdge = quoteuniv3.PoolEdge

	// PoolGraph provides pool connectivity for route discovery.
	PoolGraph = quoteuniv3.PoolGraph

	// RouteService finds routes through a pool graph without quoting them.
	RouteService = quoteuniv3.RouteService

	// StaticPoolGraph is an in-memory PoolGraph backed by a fixed edge list.
	StaticPoolGraph = quoteuniv3.StaticPoolGraph
)

const (
	QuoteModeExactInput  = quoteshared.QuoteModeExactInput
	QuoteModeExactOutput = quoteshared.QuoteModeExactOutput
)

var (
	NewQuoteService      = quoteuniv3.NewQuoteService
	NewDirectRoute       = quoteuniv3.NewDirectRoute
	NewRouteService      = quoteuniv3.NewRouteService
	NewStaticPoolGraph   = quoteuniv3.NewStaticPoolGraph
	NewQuoteResult       = quoteshared.NewQuoteResult
	DefaultSqrtPriceLimit = quoteshared.DefaultSqrtPriceLimit
	GetSqrtRatioAtTick   = quoteshared.GetSqrtRatioAtTick
	GetTickAtSqrtRatio   = quoteshared.GetTickAtSqrtRatio
	ComputeSwapStep      = quoteshared.ComputeSwapStep
	AddDelta             = quoteshared.AddDelta
)
