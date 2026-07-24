package combined

import (
	"context"
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

// PoolRegistry lists pools available to the combined quote service.
type PoolRegistry[PoolID comparable] interface {
	List(context.Context) ([]PoolID, error)
}

// BalancerPoolRegistry lists Balancer pools and resolves their specifications.
type BalancerPoolRegistry interface {
	PoolRegistry[marketbalancer.PoolID]
	GetSpec(context.Context, marketbalancer.PoolID) (marketbalancer.PoolSpec, error)
}

// AppService orchestrates route discovery and quoting across protocol adapters.
type AppService struct {
	protocols []ProtocolAdapter
	quotes    *quoteunified.QuoteService
	readiness SystemReadinessChecker
	maxHops   int
}

func NewAppService(
	protocols []ProtocolAdapter,
	quotes *quoteunified.QuoteService,
	readiness SystemReadinessChecker,
	maxHops int,
) *AppService {
	if maxHops <= 0 {
		maxHops = 3
	}
	return &AppService{
		protocols: compactProtocolAdapters(protocols),
		quotes:    quotes,
		readiness: readiness,
		maxHops:   maxHops,
	}
}

func compactProtocolAdapters(candidates []ProtocolAdapter) []ProtocolAdapter {
	protocols := make([]ProtocolAdapter, 0, len(candidates))
	for _, protocol := range candidates {
		if protocol != nil {
			protocols = append(protocols, protocol)
		}
	}
	return protocols
}

// Quote executes the unified quote use case for the given request.
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

	if req.PoolAddress != nil {
		return s.quoteSinglePoolByAddress(ctx, req, *req.PoolAddress)
	}
	if req.PoolID != nil {
		return s.quoteSinglePoolByID(ctx, req, *req.PoolID)
	}
	if req.BalancerPoolID != nil {
		return s.quoteSingleBalancerPool(ctx, req, *req.BalancerPoolID)
	}
	if req.IsExactOutput() {
		return Response{}, fmt.Errorf("multi-hop exact-output quotes are not supported")
	}
	return s.quoteBestRoute(ctx, req)
}

func quoteViewBlock(readiness SystemReadinessChecker) uint64 {
	if versioned, ok := readiness.(interface{ Generation() uint64 }); ok {
		return versioned.Generation()
	}
	if versioned, ok := readiness.(interface{ BlockNumber() uint64 }); ok {
		return versioned.BlockNumber()
	}
	return 0
}

func validateQuoteRequest(req Request) error {
	if !isCombinedQuoteToken(req.TokenIn) || !isCombinedQuoteToken(req.TokenOut) {
		return fmt.Errorf("tokenIn and tokenOut are required")
	}
	if req.TokenIn == req.TokenOut {
		return fmt.Errorf("tokenIn and tokenOut must differ")
	}
	selectors := 0
	if req.PoolAddress != nil {
		selectors++
	}
	if req.PoolID != nil {
		selectors++
	}
	if req.BalancerPoolID != nil {
		selectors++
	}
	if selectors > 1 {
		return fmt.Errorf("only one pool selector may be provided")
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
	return s.quoteDirect(ctx, req, DirectPoolSelector{Address: &poolAddress}, poolAddress.Hex())
}

func (s *AppService) quoteSinglePoolByID(ctx context.Context, req Request, poolID marketuniv4.PoolID) (Response, error) {
	return s.quoteDirect(ctx, req, DirectPoolSelector{Univ4ID: &poolID}, poolID.String())
}

func (s *AppService) quoteSingleBalancerPool(ctx context.Context, req Request, poolID marketbalancer.PoolID) (Response, error) {
	return s.quoteDirect(ctx, req, DirectPoolSelector{BalancerPoolID: &poolID}, poolID.String())
}

func (s *AppService) quoteDirect(ctx context.Context, req Request, selector DirectPoolSelector, selectorName string) (Response, error) {
	for _, protocol := range s.protocols {
		if protocol == nil {
			continue
		}
		response, handled, err := protocol.QuoteDirect(ctx, selector, req, s.quotes)
		if err != nil {
			return Response{}, fmt.Errorf("%s direct quote: %w", protocol.Name(), err)
		}
		if handled {
			return response, nil
		}
	}
	return Response{}, fmt.Errorf("pool %s not found", selectorName)
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
	for _, protocol := range s.protocols {
		if protocol == nil {
			continue
		}
		protocolEdges, err := protocol.LoadEdges(ctx)
		if err != nil {
			return nil, fmt.Errorf("load %s pool graph edges: %w", protocol.Name(), err)
		}
		edges = append(edges, protocolEdges...)
	}

	if len(edges) == 0 {
		return nil, fmt.Errorf("no pools available for routing")
	}

	return quoteunified.NewStaticPoolGraph(edges), nil
}

func (s *AppService) loadRoutePools(ctx context.Context, route quoteunified.Route) (quoteunified.RoutePools, error) {
	pools := quoteunified.RoutePools{
		V3:          make(map[common.Address]*marketuniv3.Pool),
		PancakeV3:   make(map[common.Address]*marketpancake.Pool),
		QuickSwapV3: make(map[common.Address]*marketquick.Pool),
		V4:          make(map[marketuniv4.PoolID]*marketuniv4.Pool),
		Balancer:    make(map[marketbalancer.PoolID]*marketbalancer.Pool),
	}

	for _, hop := range route.Hops {
		if hop.Version == quoteunified.PoolVersionWrapWETH || hop.Version == quoteunified.PoolVersionUnwrapWETH {
			continue
		}
		handled := false
		for _, protocol := range s.protocols {
			if protocol == nil {
				continue
			}
			matched, err := protocol.LoadRoutePool(ctx, hop, &pools)
			if err != nil {
				return quoteunified.RoutePools{}, fmt.Errorf("load %s route pool: %w", protocol.Name(), err)
			}
			if matched {
				handled = true
				break
			}
		}
		if !handled {
			return quoteunified.RoutePools{}, fmt.Errorf("unsupported pool version %d", hop.Version)
		}
	}

	return pools, nil
}

func (s *AppService) ensureRouteReady(route quoteunified.Route) error {
	for _, hop := range route.Hops {
		if hop.Version == quoteunified.PoolVersionWrapWETH || hop.Version == quoteunified.PoolVersionUnwrapWETH {
			continue
		}
		handled := false
		for _, protocol := range s.protocols {
			if protocol == nil {
				continue
			}
			matched, err := protocol.CheckRouteHopReady(hop)
			if err != nil {
				return fmt.Errorf("%s route readiness: %w", protocol.Name(), err)
			}
			if matched {
				handled = true
				break
			}
		}
		if !handled {
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
