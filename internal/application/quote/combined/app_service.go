package combined

import (
	"context"
	"fmt"
	"math/big"

	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v3"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
	"github.com/ethereum/go-ethereum/common"
)

// ReadinessChecker gates quoting on pool and system readiness across protocols.
type ReadinessChecker interface {
	IsSystemReady() bool
	IsV3PoolReady(poolAddress common.Address) bool
	IsV4PoolReady(poolID marketv4.PoolID) bool
}

// AppService orchestrates unified V3/V4 route discovery and quoting.
type AppService struct {
	v3Pools     marketv3.PoolRepository
	v4Pools     marketv4.PoolRepository
	v3Registry  marketv3.PoolRegistry
	v4Registry  marketv4.PoolRegistry
	quotes      *quoteunified.QuoteService
	readiness   ReadinessChecker
	maxHops     int
}

func NewAppService(
	v3Pools marketv3.PoolRepository,
	v4Pools marketv4.PoolRepository,
	v3Registry marketv3.PoolRegistry,
	v4Registry marketv4.PoolRegistry,
	quotes *quoteunified.QuoteService,
	readiness ReadinessChecker,
	maxHops int,
) *AppService {
	if maxHops <= 0 {
		maxHops = 3
	}
	return &AppService{
		v3Pools:    v3Pools,
		v4Pools:    v4Pools,
		v3Registry: v3Registry,
		v4Registry: v4Registry,
		quotes:     quotes,
		readiness:  readiness,
		maxHops:    maxHops,
	}
}

// Quote executes the unified quote use case for the given request.
func (s *AppService) Quote(ctx context.Context, req Request) (Response, error) {
	if err := validateQuoteRequest(req); err != nil {
		return Response{}, err
	}
	if s.readiness != nil && !s.readiness.IsSystemReady() {
		return Response{}, fmt.Errorf("system is not ready for quoting")
	}

	if req.PoolAddress != nil {
		return s.quoteSingleV3Pool(ctx, req, *req.PoolAddress)
	}
	if req.PoolID != nil {
		return s.quoteSingleV4Pool(ctx, req, *req.PoolID)
	}
	if req.IsExactOutput() {
		return Response{}, fmt.Errorf("multi-hop exact-output quotes are not supported")
	}
	return s.quoteBestRoute(ctx, req)
}

func validateQuoteRequest(req Request) error {
	if req.TokenIn == (common.Address{}) || req.TokenOut == (common.Address{}) {
		return fmt.Errorf("tokenIn and tokenOut are required")
	}
	if req.TokenIn == req.TokenOut {
		return fmt.Errorf("tokenIn and tokenOut must differ")
	}
	if req.PoolAddress != nil && req.PoolID != nil {
		return fmt.Errorf("only one of poolAddress or poolId may be provided")
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

func (s *AppService) quoteSingleV3Pool(ctx context.Context, req Request, poolAddress common.Address) (Response, error) {
	if s.readiness != nil && !s.readiness.IsV3PoolReady(poolAddress) {
		return Response{}, fmt.Errorf("pool %s is not ready", poolAddress.Hex())
	}

	pool, err := s.v3Pools.Get(ctx, poolAddress)
	if err != nil {
		return Response{}, fmt.Errorf("load pool %s: %w", poolAddress.Hex(), err)
	}
	if pool == nil {
		return Response{}, fmt.Errorf("pool %s not found", poolAddress.Hex())
	}

	var result quoteshared.QuoteResult
	if req.IsExactInput() {
		result, err = s.quotes.QuoteExactInputV3(pool, req.TokenIn, req.TokenOut, req.AmountIn)
	} else {
		result, err = s.quotes.QuoteExactOutputV3(pool, req.TokenIn, req.TokenOut, req.AmountOut)
	}
	if err != nil {
		return Response{}, fmt.Errorf("quote pool %s: %w", poolAddress.Hex(), err)
	}

	route := quoteunified.NewDirectV3Route(poolAddress, req.TokenIn, req.TokenOut)
	return newSinglePoolResponse(req, route, result), nil
}

func (s *AppService) quoteSingleV4Pool(ctx context.Context, req Request, poolID marketv4.PoolID) (Response, error) {
	if s.readiness != nil && !s.readiness.IsV4PoolReady(poolID) {
		return Response{}, fmt.Errorf("pool %s is not ready", poolID.String())
	}

	pool, err := s.v4Pools.Get(ctx, poolID)
	if err != nil {
		return Response{}, fmt.Errorf("load pool %s: %w", poolID.String(), err)
	}
	if pool == nil {
		return Response{}, fmt.Errorf("pool %s not found", poolID.String())
	}

	var result quoteshared.QuoteResult
	if req.IsExactInput() {
		result, err = s.quotes.QuoteExactInputV4(pool, req.TokenIn, req.TokenOut, req.AmountIn)
	} else {
		result, err = s.quotes.QuoteExactOutputV4(pool, req.TokenIn, req.TokenOut, req.AmountOut)
	}
	if err != nil {
		return Response{}, fmt.Errorf("quote pool %s: %w", poolID.String(), err)
	}

	route := quoteunified.NewDirectV4Route(poolID, req.TokenIn, req.TokenOut)
	return newSinglePoolResponse(req, route, result), nil
}

func newSinglePoolResponse(req Request, route quoteunified.Route, result quoteshared.QuoteResult) Response {
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
	}
}

func (s *AppService) quoteBestRoute(ctx context.Context, req Request) (Response, error) {
	graph, err := s.buildPoolGraph(ctx)
	if err != nil {
		return Response{}, err
	}

	routeService := quoteunified.NewRouteService(graph, s.maxHops)
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

func (s *AppService) buildPoolGraph(ctx context.Context) (quoteunified.PoolGraph, error) {
	edges := make([]quoteunified.PoolEdge, 0)

	if s.v3Registry != nil && s.v3Pools != nil {
		addresses, err := s.v3Registry.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list v3 pools: %w", err)
		}
		for _, address := range addresses {
			pool, err := s.v3Pools.Get(ctx, address)
			if err != nil {
				return nil, fmt.Errorf("load v3 pool %s: %w", address.Hex(), err)
			}
			if pool == nil {
				continue
			}
			edges = append(edges, quoteunified.PoolEdge{
				Version: quoteunified.PoolVersionV3,
				PoolV3:  pool.Address,
				Token0:  pool.Token0,
				Token1:  pool.Token1,
			})
		}
	}

	if s.v4Registry != nil && s.v4Pools != nil {
		poolIDs, err := s.v4Registry.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list v4 pools: %w", err)
		}
		for _, poolID := range poolIDs {
			pool, err := s.v4Pools.Get(ctx, poolID)
			if err != nil {
				return nil, fmt.Errorf("load v4 pool %s: %w", poolID.String(), err)
			}
			if pool == nil {
				continue
			}
			edges = append(edges, quoteunified.PoolEdge{
				Version: quoteunified.PoolVersionV4,
				PoolV4:  pool.ID,
				Token0:  pool.Key.Currency0,
				Token1:  pool.Key.Currency1,
			})
		}
	}

	if len(edges) == 0 {
		return nil, fmt.Errorf("no pools available for routing")
	}

	return quoteunified.NewStaticPoolGraph(edges), nil
}

func (s *AppService) loadRoutePools(ctx context.Context, route quoteunified.Route) (quoteunified.RoutePools, error) {
	pools := quoteunified.RoutePools{
		V3: make(map[common.Address]*marketv3.Pool),
		V4: make(map[marketv4.PoolID]*marketv4.Pool),
	}

	for _, hop := range route.Hops {
		switch hop.Version {
		case quoteunified.PoolVersionV3:
			if _, ok := pools.V3[hop.PoolV3]; ok {
				continue
			}
			pool, err := s.v3Pools.Get(ctx, hop.PoolV3)
			if err != nil {
				return quoteunified.RoutePools{}, fmt.Errorf("load v3 pool %s: %w", hop.PoolV3.Hex(), err)
			}
			if pool == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("v3 pool %s not found", hop.PoolV3.Hex())
			}
			pools.V3[hop.PoolV3] = pool
		case quoteunified.PoolVersionV4:
			if _, ok := pools.V4[hop.PoolV4]; ok {
				continue
			}
			pool, err := s.v4Pools.Get(ctx, hop.PoolV4)
			if err != nil {
				return quoteunified.RoutePools{}, fmt.Errorf("load v4 pool %s: %w", hop.PoolV4.String(), err)
			}
			if pool == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("v4 pool %s not found", hop.PoolV4.String())
			}
			pools.V4[hop.PoolV4] = pool
		default:
			return quoteunified.RoutePools{}, fmt.Errorf("unsupported pool version %d", hop.Version)
		}
	}

	return pools, nil
}

func (s *AppService) ensureRouteReady(route quoteunified.Route) error {
	if s.readiness == nil {
		return nil
	}
	for _, hop := range route.Hops {
		switch hop.Version {
		case quoteunified.PoolVersionV3:
			if !s.readiness.IsV3PoolReady(hop.PoolV3) {
				return fmt.Errorf("v3 pool %s is not ready", hop.PoolV3.Hex())
			}
		case quoteunified.PoolVersionV4:
			if !s.readiness.IsV4PoolReady(hop.PoolV4) {
				return fmt.Errorf("v4 pool %s is not ready", hop.PoolV4.String())
			}
		default:
			return fmt.Errorf("unsupported pool version %d", hop.Version)
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
