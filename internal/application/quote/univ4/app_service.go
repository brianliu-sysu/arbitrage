package quoteuniv4

import (
	"context"
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	quoteuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// ReadinessChecker gates quoting on pool and system readiness.
type ReadinessChecker interface {
	IsSystemReady() bool
	IsPoolReady(poolID marketv4.PoolID) bool
}

type PoolRegistry interface {
	List(context.Context) ([]marketv4.PoolID, error)
}

// AppService orchestrates V4 route discovery and quoting.
type AppService struct {
	pools     marketv4.PoolRepository
	registry  PoolRegistry
	quotes    *quoteuniv4.QuoteService
	readiness ReadinessChecker
	maxHops   int
}

func NewAppService(
	pools marketv4.PoolRepository,
	registry PoolRegistry,
	quotes *quoteuniv4.QuoteService,
	readiness ReadinessChecker,
	maxHops int,
) *AppService {
	if maxHops <= 0 {
		maxHops = 3
	}
	return &AppService{
		pools:     pools,
		registry:  registry,
		quotes:    quotes,
		readiness: readiness,
		maxHops:   maxHops,
	}
}

// Quote executes the V4 quote use case for the given request.
func (s *AppService) Quote(ctx context.Context, req Request) (Response, error) {
	if err := validateQuoteRequest(req); err != nil {
		return Response{}, err
	}
	for {
		blockBefore := quoteViewBlock(s.readiness)
		response, err := s.quoteCurrentView(ctx, req)
		if blockBefore == quoteViewBlock(s.readiness) {
			return response, err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Response{}, ctxErr
		}
	}
}

func (s *AppService) quoteCurrentView(ctx context.Context, req Request) (Response, error) {
	if s.readiness != nil && !s.readiness.IsSystemReady() {
		return Response{}, fmt.Errorf("system is not ready for quoting")
	}

	if req.PoolID != nil {
		return s.quoteSinglePool(ctx, req, *req.PoolID)
	}
	if req.IsExactOutput() {
		return Response{}, fmt.Errorf("multi-hop exact-output quotes are not supported")
	}
	return s.quoteBestRoute(ctx, req)
}

func quoteViewBlock(readiness ReadinessChecker) uint64 {
	if versioned, ok := readiness.(interface{ Generation() uint64 }); ok {
		return versioned.Generation()
	}
	if versioned, ok := readiness.(interface{ BlockNumber() uint64 }); ok {
		return versioned.BlockNumber()
	}
	return 0
}

func validateQuoteRequest(req Request) error {
	if !isV4QuoteToken(req.TokenIn) || !isV4QuoteToken(req.TokenOut) {
		return fmt.Errorf("tokenIn and tokenOut are required")
	}
	if req.TokenIn == req.TokenOut {
		return fmt.Errorf("tokenIn and tokenOut must differ")
	}

	switch req.Mode {
	case quoteshared.QuoteModeExactInput:
		if req.AmountIn == nil || req.AmountIn.Sign() <= 0 {
			return fmt.Errorf("amountIn must be positive for exact-input quotes")
		}
	case quoteshared.QuoteModeExactOutput:
		if req.AmountOut == nil || req.AmountOut.Sign() <= 0 {
			return fmt.Errorf("amountOut must be positive for exact-output quotes")
		}
	default:
		return fmt.Errorf("unsupported quote mode %d", req.Mode)
	}
	return nil
}

func isV4QuoteToken(address common.Address) bool {
	return address != (common.Address{}) || asset.IsNativeETH(address)
}

func (s *AppService) quoteSinglePool(ctx context.Context, req Request, poolID marketv4.PoolID) (Response, error) {
	if s.readiness != nil && !s.readiness.IsPoolReady(poolID) {
		return Response{}, fmt.Errorf("pool %s is not ready", poolID.String())
	}

	pool, err := s.pools.Get(ctx, poolID)
	if err != nil {
		return Response{}, fmt.Errorf("load pool %s: %w", poolID.String(), err)
	}
	if pool == nil {
		return Response{}, fmt.Errorf("pool %s not found", poolID.String())
	}

	var result quoteshared.QuoteResult
	if req.IsExactInput() {
		result, err = s.quotes.QuoteExactInput(pool, req.TokenIn, req.TokenOut, req.AmountIn)
	} else {
		result, err = s.quotes.QuoteExactOutput(pool, req.TokenIn, req.TokenOut, req.AmountOut)
	}
	if err != nil {
		return Response{}, fmt.Errorf("quote pool %s: %w", poolID.String(), err)
	}

	route := quoteuniv4.NewDirectRoute(poolID, req.TokenIn, req.TokenOut)
	return Response{
		TokenIn:   req.TokenIn,
		TokenOut:  req.TokenOut,
		AmountIn:  cloneBigInt(result.AmountIn),
		AmountOut: cloneBigInt(result.AmountOut),
		FeeAmount: cloneBigInt(result.FeeAmount),
		BestRoute: route,
		RouteQuotes: []RouteQuote{{
			Route:     route,
			AmountIn:  cloneBigInt(result.AmountIn),
			AmountOut: cloneBigInt(result.AmountOut),
			FeeAmount: cloneBigInt(result.FeeAmount),
		}},
	}, nil
}

func (s *AppService) quoteBestRoute(ctx context.Context, req Request) (Response, error) {
	graph, err := s.buildPoolGraph(ctx)
	if err != nil {
		return Response{}, err
	}

	routeService := quoteuniv4.NewRouteService(graph, s.maxHops)
	routes, err := routeService.FindRoutes(req.TokenIn, req.TokenOut)
	if err != nil {
		return Response{}, fmt.Errorf("find routes: %w", err)
	}
	if len(routes) == 0 {
		return Response{}, fmt.Errorf("no route found from %s to %s", req.TokenIn.Hex(), req.TokenOut.Hex())
	}

	routeQuotes := make([]RouteQuote, 0, len(routes))
	var best RouteQuote
	var bestAmountOut *big.Int

	for _, route := range routes {
		pools, err := s.loadRoutePools(ctx, route)
		if err != nil {
			return Response{}, err
		}
		if err := s.ensureRouteReady(route); err != nil {
			continue
		}

		result, err := s.quotes.QuoteRoute(pools, route, req.AmountIn)
		if err != nil {
			continue
		}

		candidate := RouteQuote{
			Route:     route,
			AmountIn:  cloneBigInt(result.AmountIn),
			AmountOut: cloneBigInt(result.AmountOut),
			FeeAmount: cloneBigInt(result.FeeAmount),
		}
		routeQuotes = append(routeQuotes, candidate)

		if bestAmountOut == nil || candidate.AmountOut.Cmp(bestAmountOut) > 0 {
			best = candidate
			bestAmountOut = candidate.AmountOut
		}
	}

	if bestAmountOut == nil {
		return Response{}, fmt.Errorf("no quotable route found from %s to %s", req.TokenIn.Hex(), req.TokenOut.Hex())
	}

	return Response{
		TokenIn:     req.TokenIn,
		TokenOut:    req.TokenOut,
		AmountIn:    cloneBigInt(best.AmountIn),
		AmountOut:   cloneBigInt(best.AmountOut),
		FeeAmount:   cloneBigInt(best.FeeAmount),
		BestRoute:   best.Route,
		RouteQuotes: routeQuotes,
	}, nil
}

func (s *AppService) buildPoolGraph(ctx context.Context) (quoteuniv4.PoolGraph, error) {
	if s.registry == nil {
		return nil, fmt.Errorf("pool registry is nil")
	}

	poolIDs, err := s.registry.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pools: %w", err)
	}

	edges := make([]quoteuniv4.PoolEdge, 0, len(poolIDs))
	for _, poolID := range poolIDs {
		pool, err := s.pools.Get(ctx, poolID)
		if err != nil {
			return nil, fmt.Errorf("load pool %s: %w", poolID.String(), err)
		}
		if pool == nil {
			continue
		}
		edges = append(edges, quoteuniv4.PoolEdge{
			PoolID: pool.ID,
			Token0: pool.Key.Currency0,
			Token1: pool.Key.Currency1,
		})
	}
	return quoteuniv4.NewStaticPoolGraph(edges), nil
}

func (s *AppService) loadRoutePools(ctx context.Context, route quoteuniv4.Route) (map[marketv4.PoolID]*marketv4.Pool, error) {
	pools := make(map[marketv4.PoolID]*marketv4.Pool, route.Len())
	for _, hop := range route.Hops {
		if _, ok := pools[hop.PoolID]; ok {
			continue
		}
		pool, err := s.pools.Get(ctx, hop.PoolID)
		if err != nil {
			return nil, fmt.Errorf("load pool %s: %w", hop.PoolID.String(), err)
		}
		if pool == nil {
			return nil, fmt.Errorf("pool %s not found", hop.PoolID.String())
		}
		pools[hop.PoolID] = pool
	}
	return pools, nil
}

func (s *AppService) ensureRouteReady(route quoteuniv4.Route) error {
	if s.readiness == nil {
		return nil
	}
	for _, hop := range route.Hops {
		if !s.readiness.IsPoolReady(hop.PoolID) {
			return fmt.Errorf("pool %s is not ready", hop.PoolID.String())
		}
	}
	return nil
}

func cloneBigInt(v *big.Int) *big.Int {
	if v == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(v)
}
