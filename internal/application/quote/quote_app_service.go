package quoteapp

import (
	"context"
	"fmt"
	"math/big"

	domainquote "github.com/brianliu-sysu/uniswapv3/internal/domain/quote"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

// ReadinessChecker gates quoting on pool and system readiness.
type ReadinessChecker interface {
	IsSystemReady() bool
	IsPoolReady(poolAddress common.Address) bool
}

// QuoteAppService orchestrates route discovery and quoting.
type QuoteAppService struct {
	pools     market.PoolRepository
	registry  market.PoolRegistry
	quotes    *domainquote.QuoteService
	readiness ReadinessChecker
	maxHops   int
}

func NewQuoteAppService(
	pools market.PoolRepository,
	registry market.PoolRegistry,
	quotes *domainquote.QuoteService,
	readiness ReadinessChecker,
	maxHops int,
) *QuoteAppService {
	if maxHops <= 0 {
		maxHops = 3
	}
	return &QuoteAppService{
		pools:     pools,
		registry:  registry,
		quotes:    quotes,
		readiness: readiness,
		maxHops:   maxHops,
	}
}

// Quote executes the quote use case for the given request.
func (s *QuoteAppService) Quote(ctx context.Context, req QuoteRequest) (QuoteResponse, error) {
	if err := validateQuoteRequest(req); err != nil {
		return QuoteResponse{}, err
	}
	if s.readiness != nil && !s.readiness.IsSystemReady() {
		return QuoteResponse{}, fmt.Errorf("system is not ready for quoting")
	}

	if req.PoolAddress != nil {
		return s.quoteSinglePool(ctx, req, *req.PoolAddress)
	}
	if req.IsExactOutput() {
		return QuoteResponse{}, fmt.Errorf("multi-hop exact-output quotes are not supported")
	}
	return s.quoteBestRoute(ctx, req)
}

func validateQuoteRequest(req QuoteRequest) error {
	if req.TokenIn == (common.Address{}) || req.TokenOut == (common.Address{}) {
		return fmt.Errorf("tokenIn and tokenOut are required")
	}
	if req.TokenIn == req.TokenOut {
		return fmt.Errorf("tokenIn and tokenOut must differ")
	}

	switch req.Mode {
	case QuoteModeExactInput:
		if req.AmountIn == nil || req.AmountIn.Sign() <= 0 {
			return fmt.Errorf("amountIn must be positive for exact-input quotes")
		}
	case QuoteModeExactOutput:
		if req.AmountOut == nil || req.AmountOut.Sign() <= 0 {
			return fmt.Errorf("amountOut must be positive for exact-output quotes")
		}
	default:
		return fmt.Errorf("unsupported quote mode %d", req.Mode)
	}
	return nil
}

func (s *QuoteAppService) quoteSinglePool(ctx context.Context, req QuoteRequest, poolAddress common.Address) (QuoteResponse, error) {
	if s.readiness != nil && !s.readiness.IsPoolReady(poolAddress) {
		return QuoteResponse{}, fmt.Errorf("pool %s is not ready", poolAddress.Hex())
	}

	pool, err := s.pools.Get(ctx, poolAddress)
	if err != nil {
		return QuoteResponse{}, fmt.Errorf("load pool %s: %w", poolAddress.Hex(), err)
	}
	if pool == nil {
		return QuoteResponse{}, fmt.Errorf("pool %s not found", poolAddress.Hex())
	}

	var result domainquote.QuoteResult
	if req.IsExactInput() {
		result, err = s.quotes.QuoteExactInput(pool, req.TokenIn, req.TokenOut, req.AmountIn)
	} else {
		result, err = s.quotes.QuoteExactOutput(pool, req.TokenIn, req.TokenOut, req.AmountOut)
	}
	if err != nil {
		return QuoteResponse{}, fmt.Errorf("quote pool %s: %w", poolAddress.Hex(), err)
	}

	route := domainquote.NewDirectRoute(poolAddress, req.TokenIn, req.TokenOut)
	return QuoteResponse{
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

func (s *QuoteAppService) quoteBestRoute(ctx context.Context, req QuoteRequest) (QuoteResponse, error) {
	graph, err := s.buildPoolGraph(ctx)
	if err != nil {
		return QuoteResponse{}, err
	}

	routeService := domainquote.NewRouteService(graph, s.maxHops)
	routes, err := routeService.FindRoutes(req.TokenIn, req.TokenOut)
	if err != nil {
		return QuoteResponse{}, fmt.Errorf("find routes: %w", err)
	}
	if len(routes) == 0 {
		return QuoteResponse{}, fmt.Errorf("no route found from %s to %s", req.TokenIn.Hex(), req.TokenOut.Hex())
	}

	routeQuotes := make([]RouteQuote, 0, len(routes))
	var best RouteQuote
	var bestAmountOut *big.Int

	for _, route := range routes {
		pools, err := s.loadRoutePools(ctx, route)
		if err != nil {
			return QuoteResponse{}, err
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
		return QuoteResponse{}, fmt.Errorf("no quotable route found from %s to %s", req.TokenIn.Hex(), req.TokenOut.Hex())
	}

	return QuoteResponse{
		TokenIn:     req.TokenIn,
		TokenOut:    req.TokenOut,
		AmountIn:    cloneBigInt(best.AmountIn),
		AmountOut:   cloneBigInt(best.AmountOut),
		FeeAmount:   cloneBigInt(best.FeeAmount),
		BestRoute:   best.Route,
		RouteQuotes: routeQuotes,
	}, nil
}

func (s *QuoteAppService) buildPoolGraph(ctx context.Context) (domainquote.PoolGraph, error) {
	if s.registry == nil {
		return nil, fmt.Errorf("pool registry is nil")
	}

	addresses, err := s.registry.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pools: %w", err)
	}

	edges := make([]domainquote.PoolEdge, 0, len(addresses))
	for _, address := range addresses {
		pool, err := s.pools.Get(ctx, address)
		if err != nil {
			return nil, fmt.Errorf("load pool %s: %w", address.Hex(), err)
		}
		if pool == nil {
			continue
		}
		edges = append(edges, domainquote.PoolEdge{
			PoolAddress: pool.Address,
			Token0:      pool.Token0,
			Token1:      pool.Token1,
		})
	}
	return domainquote.NewStaticPoolGraph(edges), nil
}

func (s *QuoteAppService) loadRoutePools(ctx context.Context, route domainquote.Route) (map[common.Address]*market.Pool, error) {
	pools := make(map[common.Address]*market.Pool, route.Len())
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

func (s *QuoteAppService) ensureRouteReady(route domainquote.Route) error {
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
