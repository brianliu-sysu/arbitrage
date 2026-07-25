package combined

import (
	"context"
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/uniswapv3/internal/application/committedmarket"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

// AppService quotes against one immutable committed market snapshot per request.
type AppService struct {
	market  committedmarket.Reader
	quotes  *quoteunified.QuoteService
	maxHops int
	allowed map[quoteunified.PoolVersion]struct{}
}

// NewAppService constructs the unified quote service. When versions is empty,
// every protocol contained in the committed market is available.
func NewAppService(
	market committedmarket.Reader,
	quotes *quoteunified.QuoteService,
	maxHops int,
	versions ...quoteunified.PoolVersion,
) *AppService {
	if maxHops <= 0 {
		maxHops = 3
	}
	if quotes == nil {
		quotes = quoteunified.NewQuoteService(nil, nil, nil)
	}
	allowed := make(map[quoteunified.PoolVersion]struct{}, len(versions))
	for _, version := range versions {
		allowed[version] = struct{}{}
	}
	return &AppService{market: market, quotes: quotes, maxHops: maxHops, allowed: allowed}
}

// Quote executes the unified quote use case against a single pinned snapshot.
func (s *AppService) Quote(ctx context.Context, req Request) (Response, error) {
	if err := validateQuoteRequest(req); err != nil {
		return Response{}, err
	}
	if s == nil || s.market == nil {
		return Response{}, fmt.Errorf("committed market is not configured")
	}
	snapshot := s.market.Snapshot()
	if snapshot == nil || snapshot.Version().IsZero() {
		return Response{}, fmt.Errorf("system is not ready for quoting")
	}
	return s.quoteSnapshot(ctx, snapshot, req)
}

func (s *AppService) quoteSnapshot(ctx context.Context, snapshot committedmarket.Snapshot, req Request) (Response, error) {
	if req.PoolAddress != nil {
		return s.quoteDirect(ctx, snapshot, req, *req.PoolAddress, nil, nil)
	}
	if req.PoolID != nil {
		return s.quoteDirect(ctx, snapshot, req, common.Address{}, req.PoolID, nil)
	}
	if req.BalancerPoolID != nil {
		return s.quoteDirect(ctx, snapshot, req, common.Address{}, nil, req.BalancerPoolID)
	}
	if req.IsExactOutput() {
		return Response{}, fmt.Errorf("multi-hop exact-output quotes are not supported")
	}
	return s.quoteBestRoute(ctx, snapshot, req)
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

func (s *AppService) quoteDirect(
	ctx context.Context,
	snapshot committedmarket.Snapshot,
	req Request,
	address common.Address,
	v4ID *marketuniv4.PoolID,
	balancerID *marketbalancer.PoolID,
) (Response, error) {
	edge, ok := s.findDirectEdge(snapshot.PoolEdges(), address, v4ID, balancerID)
	if !ok {
		return Response{}, fmt.Errorf("selected pool is not in committed market")
	}
	route := directRoute(edge, req.TokenIn, req.TokenOut)
	pools, err := snapshot.LoadRoutePools(ctx, route)
	if err != nil {
		return Response{}, err
	}
	result, err := s.quoteDirectRoute(pools, route.Hops[0], req)
	if err != nil {
		return Response{}, fmt.Errorf("quote selected pool: %w", err)
	}
	return newSinglePoolResponse(req, route, result), nil
}

func (s *AppService) findDirectEdge(
	edges []quoteunified.PoolEdge,
	address common.Address,
	v4ID *marketuniv4.PoolID,
	balancerID *marketbalancer.PoolID,
) (quoteunified.PoolEdge, bool) {
	for _, edge := range edges {
		if !s.allows(edge.Version) {
			continue
		}
		switch edge.Version {
		case quoteunified.PoolVersionV3:
			if v4ID == nil && balancerID == nil && edge.PoolV3 == address {
				return edge, true
			}
		case quoteunified.PoolVersionPancakeV3:
			if v4ID == nil && balancerID == nil && edge.PoolPancakeV3 == address {
				return edge, true
			}
		case quoteunified.PoolVersionQuickSwapV3:
			if v4ID == nil && balancerID == nil && edge.PoolQuickSwapV3 == address {
				return edge, true
			}
		case quoteunified.PoolVersionV4:
			if v4ID != nil && edge.PoolV4 == *v4ID {
				return edge, true
			}
		case quoteunified.PoolVersionBalancer:
			if balancerID != nil && edge.PoolBalancer == *balancerID {
				return edge, true
			}
			if v4ID == nil && balancerID == nil && edge.BalancerAddress == address {
				return edge, true
			}
		}
	}
	return quoteunified.PoolEdge{}, false
}

func directRoute(edge quoteunified.PoolEdge, tokenIn, tokenOut common.Address) quoteunified.Route {
	switch edge.Version {
	case quoteunified.PoolVersionV3:
		return quoteunified.NewDirectV3Route(edge.PoolV3, tokenIn, tokenOut)
	case quoteunified.PoolVersionPancakeV3:
		return quoteunified.NewDirectPancakeV3Route(edge.PoolPancakeV3, tokenIn, tokenOut)
	case quoteunified.PoolVersionQuickSwapV3:
		return quoteunified.NewDirectQuickSwapV3Route(edge.PoolQuickSwapV3, tokenIn, tokenOut)
	case quoteunified.PoolVersionV4:
		return quoteunified.NewDirectV4Route(edge.PoolV4, tokenIn, tokenOut)
	default:
		return quoteunified.NewDirectBalancerRoute(edge.PoolBalancer, tokenIn, tokenOut)
	}
}

func (s *AppService) quoteDirectRoute(pools quoteunified.RoutePools, hop quoteunified.RouteHop, req Request) (quoteshared.QuoteResult, error) {
	switch hop.Version {
	case quoteunified.PoolVersionV3:
		if req.IsExactInput() {
			return s.quotes.QuoteExactInputV3(pools.V3[hop.PoolV3], req.TokenIn, req.TokenOut, req.AmountIn)
		}
		return s.quotes.QuoteExactOutputV3(pools.V3[hop.PoolV3], req.TokenIn, req.TokenOut, req.AmountOut)
	case quoteunified.PoolVersionPancakeV3:
		if req.IsExactInput() {
			return s.quotes.QuoteExactInputPancakeV3(pools.PancakeV3[hop.PoolPancakeV3], req.TokenIn, req.TokenOut, req.AmountIn)
		}
		return s.quotes.QuoteExactOutputPancakeV3(pools.PancakeV3[hop.PoolPancakeV3], req.TokenIn, req.TokenOut, req.AmountOut)
	case quoteunified.PoolVersionQuickSwapV3:
		if req.IsExactInput() {
			return s.quotes.QuoteExactInputQuickSwapV3(pools.QuickSwapV3[hop.PoolQuickSwapV3], req.TokenIn, req.TokenOut, req.AmountIn)
		}
		return s.quotes.QuoteExactOutputQuickSwapV3(pools.QuickSwapV3[hop.PoolQuickSwapV3], req.TokenIn, req.TokenOut, req.AmountOut)
	case quoteunified.PoolVersionV4:
		if req.IsExactInput() {
			return s.quotes.QuoteExactInputV4(pools.V4[hop.PoolV4], req.TokenIn, req.TokenOut, req.AmountIn)
		}
		return s.quotes.QuoteExactOutputV4(pools.V4[hop.PoolV4], req.TokenIn, req.TokenOut, req.AmountOut)
	case quoteunified.PoolVersionBalancer:
		if req.IsExactInput() {
			return s.quotes.QuoteExactInputBalancer(pools.Balancer[hop.PoolBalancer], req.TokenIn, req.TokenOut, req.AmountIn)
		}
		return s.quotes.QuoteExactOutputBalancer(pools.Balancer[hop.PoolBalancer], req.TokenIn, req.TokenOut, req.AmountOut)
	default:
		return quoteshared.QuoteResult{}, fmt.Errorf("unsupported pool version %d", hop.Version)
	}
}

func (s *AppService) quoteBestRoute(ctx context.Context, snapshot committedmarket.Snapshot, req Request) (Response, error) {
	edges := s.filteredEdges(snapshot.PoolEdges())
	if len(edges) == 0 {
		return Response{}, fmt.Errorf("no pools available for routing")
	}
	routes, err := quoteunified.NewRouteService(quoteunified.NewStaticPoolGraph(edges), s.maxHops).FindRoutes(req.TokenIn, req.TokenOut)
	if err != nil {
		return Response{}, fmt.Errorf("find routes: %w", err)
	}
	routes = s.filteredRoutes(routes)
	if len(routes) == 0 {
		return Response{}, fmt.Errorf("no route found from %s to %s", req.TokenIn.Hex(), req.TokenOut.Hex())
	}

	routeQuotes := make([]RouteQuote, 0, len(routes))
	var best RouteQuote
	var bestAmountOut *big.Int
	for _, route := range routes {
		pools, err := snapshot.LoadRoutePools(ctx, route)
		if err != nil {
			return Response{}, err
		}
		result, err := s.quotes.QuoteRoute(pools, route, req.AmountIn)
		if err != nil {
			continue
		}
		candidate := RouteQuote{Route: route, AmountIn: cloneBigInt(result.AmountIn), AmountOut: cloneBigInt(result.AmountOut), FeeAmount: cloneBigInt(result.FeeAmount)}
		routeQuotes = append(routeQuotes, candidate)
		if bestAmountOut == nil || candidate.AmountOut.Cmp(bestAmountOut) > 0 {
			best, bestAmountOut = candidate, candidate.AmountOut
		}
	}
	if bestAmountOut == nil {
		return Response{}, fmt.Errorf("no quotable route found from %s to %s", req.TokenIn.Hex(), req.TokenOut.Hex())
	}
	return Response{
		TokenIn: req.TokenIn, TokenOut: req.TokenOut,
		AmountIn: cloneBigInt(best.AmountIn), AmountOut: cloneBigInt(best.AmountOut), FeeAmount: cloneBigInt(best.FeeAmount),
		BestRoute: best.Route, RouteQuotes: routeQuotes,
	}, nil
}

func (s *AppService) filteredRoutes(routes []quoteunified.Route) []quoteunified.Route {
	if len(s.allowed) == 0 {
		return routes
	}
	filtered := make([]quoteunified.Route, 0, len(routes))
	for _, route := range routes {
		allowed := true
		for _, hop := range route.Hops {
			if !s.allows(hop.Version) {
				allowed = false
				break
			}
		}
		if allowed {
			filtered = append(filtered, route)
		}
	}
	return filtered
}

func (s *AppService) filteredEdges(edges []quoteunified.PoolEdge) []quoteunified.PoolEdge {
	if len(s.allowed) == 0 {
		return edges
	}
	filtered := make([]quoteunified.PoolEdge, 0, len(edges))
	for _, edge := range edges {
		if s.allows(edge.Version) {
			filtered = append(filtered, edge)
		}
	}
	return filtered
}

func (s *AppService) allows(version quoteunified.PoolVersion) bool {
	if len(s.allowed) == 0 {
		return true
	}
	_, ok := s.allowed[version]
	return ok
}

func newSinglePoolResponse(req Request, route quoteunified.Route, result quoteshared.QuoteResult) Response {
	return Response{
		TokenIn: req.TokenIn, TokenOut: req.TokenOut,
		AmountIn: cloneBigInt(result.AmountIn), AmountOut: cloneBigInt(result.AmountOut), FeeAmount: cloneBigInt(result.FeeAmount),
		BestRoute:   route,
		RouteQuotes: []RouteQuote{{Route: route, AmountIn: cloneBigInt(result.AmountIn), AmountOut: cloneBigInt(result.AmountOut), FeeAmount: cloneBigInt(result.FeeAmount)}},
	}
}

func cloneBigInt(v *big.Int) *big.Int {
	if v == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(v)
}
