package clv3

import (
	"context"
	"fmt"
	"math/big"

	quotecontract "github.com/brianliu-sysu/uniswapv3/internal/application/quote/contract"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	quoteclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/clv3"
	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	"github.com/ethereum/go-ethereum/common"
)

// AppService orchestrates CLV3 route discovery and quoting.
type AppService struct {
	pools     quotecontract.PoolRepository[common.Address, marketclv3.Pool]
	registry  quotecontract.PoolRegistry[common.Address]
	quotes    *quoteclv3.QuoteService
	readiness quotecontract.PoolReadiness[common.Address]
	maxHops   int
}

func NewAppService(
	pools quotecontract.PoolRepository[common.Address, marketclv3.Pool],
	registry quotecontract.PoolRegistry[common.Address],
	quotes *quoteclv3.QuoteService,
	readiness quotecontract.PoolReadiness[common.Address],
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

// Quote executes the CLV3 quote use case for the given request.
func (s *AppService) Quote(ctx context.Context, req Request) (Response, error) {
	if err := validateQuoteRequest(req); err != nil {
		return Response{}, err
	}
	for {
		blockBefore := quotecontract.ViewRevision(s.readiness)
		response, err := s.quoteCurrentView(ctx, req)
		if blockBefore == quotecontract.ViewRevision(s.readiness) {
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

	if req.PoolAddress != nil {
		return s.quoteSinglePool(ctx, req, *req.PoolAddress)
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

func (s *AppService) quoteSinglePool(ctx context.Context, req Request, poolAddress common.Address) (Response, error) {
	if s.readiness != nil && !s.readiness.IsPoolReady(poolAddress) {
		return Response{}, fmt.Errorf("pool %s is not ready", poolAddress.Hex())
	}

	pool, err := s.pools.Get(ctx, poolAddress)
	if err != nil {
		return Response{}, fmt.Errorf("load pool %s: %w", poolAddress.Hex(), err)
	}
	if pool == nil {
		return Response{}, fmt.Errorf("pool %s not found", poolAddress.Hex())
	}

	var result quoteshared.QuoteResult
	if req.IsExactInput() {
		result, err = s.quotes.QuoteExactInput(pool, req.TokenIn, req.TokenOut, req.AmountIn)
	} else {
		result, err = s.quotes.QuoteExactOutput(pool, req.TokenIn, req.TokenOut, req.AmountOut)
	}
	if err != nil {
		return Response{}, fmt.Errorf("quote pool %s: %w", poolAddress.Hex(), err)
	}

	route := quoteclv3.NewDirectRoute(poolAddress, req.TokenIn, req.TokenOut)
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

	routeService := quoteclv3.NewRouteService(graph, s.maxHops)
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

func (s *AppService) buildPoolGraph(ctx context.Context) (quoteclv3.PoolGraph, error) {
	if s.registry == nil {
		return nil, fmt.Errorf("pool registry is nil")
	}

	addresses, err := s.registry.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pools: %w", err)
	}

	edges := make([]quoteclv3.PoolEdge, 0, len(addresses))
	for _, address := range addresses {
		pool, err := s.pools.Get(ctx, address)
		if err != nil {
			return nil, fmt.Errorf("load pool %s: %w", address.Hex(), err)
		}
		if pool == nil {
			continue
		}
		edges = append(edges, quoteclv3.PoolEdge{
			PoolAddress: pool.Address,
			Token0:      pool.Token0,
			Token1:      pool.Token1,
		})
	}
	return quoteclv3.NewStaticPoolGraph(edges), nil
}

func (s *AppService) loadRoutePools(ctx context.Context, route quoteclv3.Route) (map[common.Address]*marketclv3.Pool, error) {
	pools := make(map[common.Address]*marketclv3.Pool, route.Len())
	for _, hop := range route.Hops {
		if _, ok := pools[hop.PoolAddress]; ok {
			continue
		}
		pool, err := s.pools.Get(ctx, hop.PoolAddress)
		if err != nil {
			return nil, fmt.Errorf("load pool %s: %w", hop.PoolAddress.Hex(), err)
		}
		if pool == nil {
			return nil, fmt.Errorf("pool %s not found", hop.PoolAddress.Hex())
		}
		pools[hop.PoolAddress] = pool
	}
	return pools, nil
}

func (s *AppService) ensureRouteReady(route quoteclv3.Route) error {
	if s.readiness == nil {
		return nil
	}
	for _, hop := range route.Hops {
		if !s.readiness.IsPoolReady(hop.PoolAddress) {
			return fmt.Errorf("pool %s is not ready", hop.PoolAddress.Hex())
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
