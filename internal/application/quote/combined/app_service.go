package combined

import (
	"context"
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// ReadinessChecker gates quoting on pool and system readiness across protocols.
type ReadinessChecker interface {
	IsSystemReady() bool
	IsV3PoolReady(poolAddress common.Address) bool
	IsPancakeV3PoolReady(poolAddress common.Address) bool
	IsV4PoolReady(poolID marketuniv4.PoolID) bool
}

// AppService orchestrates unified V3/PancakeV3/V4 route discovery and quoting.
type AppService struct {
	univ3Pools          marketuniv3.PoolRepository
	pancakePools     marketpancake.PoolRepository
	univ4Pools          marketuniv4.PoolRepository
	v3Registry       marketuniv3.PoolRegistry
	pancakeRegistry  marketpancake.PoolRegistry
	v4Registry       marketuniv4.PoolRegistry
	quotes           *quoteunified.QuoteService
	readiness        ReadinessChecker
	maxHops          int
}

func NewAppService(
	univ3Pools marketuniv3.PoolRepository,
	pancakePools marketpancake.PoolRepository,
	univ4Pools marketuniv4.PoolRepository,
	v3Registry marketuniv3.PoolRegistry,
	pancakeRegistry marketpancake.PoolRegistry,
	v4Registry marketuniv4.PoolRegistry,
	quotes *quoteunified.QuoteService,
	readiness ReadinessChecker,
	maxHops int,
) *AppService {
	if maxHops <= 0 {
		maxHops = 3
	}
	return &AppService{
		univ3Pools:         univ3Pools,
		pancakePools:    pancakePools,
		univ4Pools:         univ4Pools,
		v3Registry:      v3Registry,
		pancakeRegistry: pancakeRegistry,
		v4Registry:      v4Registry,
		quotes:          quotes,
		readiness:       readiness,
		maxHops:         maxHops,
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
		return s.quoteSinglePoolByAddress(ctx, req, *req.PoolAddress)
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
	if !isCombinedQuoteToken(req.TokenIn) || !isCombinedQuoteToken(req.TokenOut) {
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

func isCombinedQuoteToken(address common.Address) bool {
	return address != (common.Address{}) || asset.IsNativeETH(address)
}

func (s *AppService) quoteSinglePoolByAddress(ctx context.Context, req Request, poolAddress common.Address) (Response, error) {
	if s.univ3Pools != nil {
		pool, err := s.univ3Pools.Get(ctx, poolAddress)
		if err != nil {
			return Response{}, fmt.Errorf("load pool %s: %w", poolAddress.Hex(), err)
		}
		if pool != nil {
			return s.quoteSingleV3Pool(ctx, req, poolAddress, pool)
		}
	}

	if s.pancakePools != nil {
		pool, err := s.pancakePools.Get(ctx, poolAddress)
		if err != nil {
			return Response{}, fmt.Errorf("load pool %s: %w", poolAddress.Hex(), err)
		}
		if pool != nil {
			return s.quoteSinglePancakeV3Pool(ctx, req, poolAddress, pool)
		}
	}

	return Response{}, fmt.Errorf("pool %s not found", poolAddress.Hex())
}

func (s *AppService) quoteSingleV3Pool(ctx context.Context, req Request, poolAddress common.Address, pool *marketuniv3.Pool) (Response, error) {
	if s.readiness != nil && !s.readiness.IsV3PoolReady(poolAddress) {
		return Response{}, fmt.Errorf("pool %s is not ready", poolAddress.Hex())
	}

	var result quoteshared.QuoteResult
	var err error
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

func (s *AppService) quoteSinglePancakeV3Pool(ctx context.Context, req Request, poolAddress common.Address, pool *marketpancake.Pool) (Response, error) {
	_ = ctx
	if s.readiness != nil && !s.readiness.IsPancakeV3PoolReady(poolAddress) {
		return Response{}, fmt.Errorf("pool %s is not ready", poolAddress.Hex())
	}

	var result quoteshared.QuoteResult
	var err error
	if req.IsExactInput() {
		result, err = s.quotes.QuoteExactInputPancakeV3(pool, req.TokenIn, req.TokenOut, req.AmountIn)
	} else {
		result, err = s.quotes.QuoteExactOutputPancakeV3(pool, req.TokenIn, req.TokenOut, req.AmountOut)
	}
	if err != nil {
		return Response{}, fmt.Errorf("quote pool %s: %w", poolAddress.Hex(), err)
	}

	route := quoteunified.NewDirectPancakeV3Route(poolAddress, req.TokenIn, req.TokenOut)
	return newSinglePoolResponse(req, route, result), nil
}

func (s *AppService) quoteSingleV4Pool(ctx context.Context, req Request, poolID marketuniv4.PoolID) (Response, error) {
	if s.readiness != nil && !s.readiness.IsV4PoolReady(poolID) {
		return Response{}, fmt.Errorf("pool %s is not ready", poolID.String())
	}

	pool, err := s.univ4Pools.Get(ctx, poolID)
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

	if s.v3Registry != nil && s.univ3Pools != nil {
		addresses, err := s.v3Registry.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list univ3 pools: %w", err)
		}
		for _, address := range addresses {
			pool, err := s.univ3Pools.Get(ctx, address)
			if err != nil {
				return nil, fmt.Errorf("load univ3 pool %s: %w", address.Hex(), err)
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

	if s.pancakeRegistry != nil && s.pancakePools != nil {
		addresses, err := s.pancakeRegistry.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list pancakev3 pools: %w", err)
		}
		for _, address := range addresses {
			pool, err := s.pancakePools.Get(ctx, address)
			if err != nil {
				return nil, fmt.Errorf("load pancakev3 pool %s: %w", address.Hex(), err)
			}
			if pool == nil {
				continue
			}
			edges = append(edges, quoteunified.PoolEdge{
				Version:       quoteunified.PoolVersionPancakeV3,
				PoolPancakeV3: pool.Address,
				Token0:        pool.Token0,
				Token1:        pool.Token1,
			})
		}
	}

	if s.v4Registry != nil && s.univ4Pools != nil {
		poolIDs, err := s.v4Registry.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list univ4 pools: %w", err)
		}
		for _, poolID := range poolIDs {
			pool, err := s.univ4Pools.Get(ctx, poolID)
			if err != nil {
				return nil, fmt.Errorf("load univ4 pool %s: %w", poolID.String(), err)
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
		V3:        make(map[common.Address]*marketuniv3.Pool),
		PancakeV3: make(map[common.Address]*marketpancake.Pool),
		V4:        make(map[marketuniv4.PoolID]*marketuniv4.Pool),
	}

	for _, hop := range route.Hops {
		switch hop.Version {
		case quoteunified.PoolVersionV3:
			if _, ok := pools.V3[hop.PoolV3]; ok {
				continue
			}
			pool, err := s.univ3Pools.Get(ctx, hop.PoolV3)
			if err != nil {
				return quoteunified.RoutePools{}, fmt.Errorf("load univ3 pool %s: %w", hop.PoolV3.Hex(), err)
			}
			if pool == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("univ3 pool %s not found", hop.PoolV3.Hex())
			}
			pools.V3[hop.PoolV3] = pool
		case quoteunified.PoolVersionPancakeV3:
			if _, ok := pools.PancakeV3[hop.PoolPancakeV3]; ok {
				continue
			}
			pool, err := s.pancakePools.Get(ctx, hop.PoolPancakeV3)
			if err != nil {
				return quoteunified.RoutePools{}, fmt.Errorf("load pancakev3 pool %s: %w", hop.PoolPancakeV3.Hex(), err)
			}
			if pool == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("pancakev3 pool %s not found", hop.PoolPancakeV3.Hex())
			}
			pools.PancakeV3[hop.PoolPancakeV3] = pool
		case quoteunified.PoolVersionV4:
			if _, ok := pools.V4[hop.PoolV4]; ok {
				continue
			}
			pool, err := s.univ4Pools.Get(ctx, hop.PoolV4)
			if err != nil {
				return quoteunified.RoutePools{}, fmt.Errorf("load univ4 pool %s: %w", hop.PoolV4.String(), err)
			}
			if pool == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("univ4 pool %s not found", hop.PoolV4.String())
			}
			pools.V4[hop.PoolV4] = pool
		case quoteunified.PoolVersionWrapWETH, quoteunified.PoolVersionUnwrapWETH:
			continue
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
				return fmt.Errorf("univ3 pool %s is not ready", hop.PoolV3.Hex())
			}
		case quoteunified.PoolVersionPancakeV3:
			if !s.readiness.IsPancakeV3PoolReady(hop.PoolPancakeV3) {
				return fmt.Errorf("pancakev3 pool %s is not ready", hop.PoolPancakeV3.Hex())
			}
		case quoteunified.PoolVersionV4:
			if !s.readiness.IsV4PoolReady(hop.PoolV4) {
				return fmt.Errorf("univ4 pool %s is not ready", hop.PoolV4.String())
			}
		case quoteunified.PoolVersionWrapWETH, quoteunified.PoolVersionUnwrapWETH:
			continue
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
