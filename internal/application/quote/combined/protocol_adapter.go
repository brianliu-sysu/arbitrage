package combined

import (
	"context"
	"fmt"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

// SystemReadinessChecker gates all combined quoting.
type SystemReadinessChecker interface {
	IsSystemReady() bool
}

type Univ3ReadinessChecker interface {
	IsV3PoolReady(common.Address) bool
}

type PancakeV3ReadinessChecker interface {
	IsPancakeV3PoolReady(common.Address) bool
}

type QuickSwapV3ReadinessChecker interface {
	IsQuickSwapV3PoolReady(common.Address) bool
}

type Univ4ReadinessChecker interface {
	IsV4PoolReady(marketuniv4.PoolID) bool
}

type BalancerReadinessChecker interface {
	IsBalancerPoolReady(marketbalancer.PoolID) bool
}

// DirectPoolSelector identifies a pool requested for a direct quote.
type DirectPoolSelector struct {
	Address        *common.Address
	Univ4ID        *marketuniv4.PoolID
	BalancerPoolID *marketbalancer.PoolID
}

// ProtocolAdapter contains the protocol-specific behavior used by combined
// route discovery and quoting.
type ProtocolAdapter interface {
	Name() string
	LoadEdges(context.Context) ([]quoteunified.PoolEdge, error)
	QuoteDirect(context.Context, DirectPoolSelector, Request, *quoteunified.QuoteService) (Response, bool, error)
	LoadRoutePool(context.Context, quoteunified.RouteHop, *quoteunified.RoutePools) (bool, error)
	CheckRouteHopReady(quoteunified.RouteHop) (bool, error)
}

type univ3ProtocolAdapter struct {
	pools     marketuniv3.PoolRepository
	registry  PoolRegistry[common.Address]
	readiness Univ3ReadinessChecker
}

func NewUniv3ProtocolAdapter(pools marketuniv3.PoolRepository, registry PoolRegistry[common.Address], readiness Univ3ReadinessChecker) ProtocolAdapter {
	if pools == nil || registry == nil {
		return nil
	}
	return &univ3ProtocolAdapter{pools: pools, registry: registry, readiness: readiness}
}

func (a *univ3ProtocolAdapter) Name() string { return "univ3" }

func (a *univ3ProtocolAdapter) LoadEdges(ctx context.Context) ([]quoteunified.PoolEdge, error) {
	addresses, err := a.registry.List(ctx)
	if err != nil {
		return nil, err
	}
	edges := make([]quoteunified.PoolEdge, 0, len(addresses))
	for _, address := range addresses {
		pool, err := a.pools.Get(ctx, address)
		if err != nil {
			return nil, fmt.Errorf("load pool %s: %w", address.Hex(), err)
		}
		if pool != nil {
			edges = append(edges, quoteunified.PoolEdge{Version: quoteunified.PoolVersionV3, PoolV3: pool.Address, Token0: pool.Token0, Token1: pool.Token1})
		}
	}
	return edges, nil
}

func (a *univ3ProtocolAdapter) QuoteDirect(ctx context.Context, selector DirectPoolSelector, req Request, quotes *quoteunified.QuoteService) (Response, bool, error) {
	if selector.Address == nil {
		return Response{}, false, nil
	}
	pool, err := a.pools.Get(ctx, *selector.Address)
	if err != nil {
		return Response{}, true, fmt.Errorf("load pool %s: %w", selector.Address.Hex(), err)
	}
	if pool == nil {
		return Response{}, false, nil
	}
	if a.readiness != nil && !a.readiness.IsV3PoolReady(*selector.Address) {
		return Response{}, true, fmt.Errorf("pool %s is not ready", selector.Address.Hex())
	}
	result, err := quoteV3Direct(quotes, pool, req)
	if err != nil {
		return Response{}, true, fmt.Errorf("quote pool %s: %w", selector.Address.Hex(), err)
	}
	route := quoteunified.NewDirectV3Route(*selector.Address, req.TokenIn, req.TokenOut)
	return newSinglePoolResponse(req, route, result), true, nil
}

func (a *univ3ProtocolAdapter) LoadRoutePool(ctx context.Context, hop quoteunified.RouteHop, pools *quoteunified.RoutePools) (bool, error) {
	if hop.Version != quoteunified.PoolVersionV3 {
		return false, nil
	}
	if pools.V3[hop.PoolV3] != nil {
		return true, nil
	}
	pool, err := a.pools.Get(ctx, hop.PoolV3)
	if err != nil {
		return true, fmt.Errorf("load pool %s: %w", hop.PoolV3.Hex(), err)
	}
	if pool == nil {
		return true, fmt.Errorf("pool %s not found", hop.PoolV3.Hex())
	}
	pools.V3[hop.PoolV3] = pool
	return true, nil
}

func (a *univ3ProtocolAdapter) CheckRouteHopReady(hop quoteunified.RouteHop) (bool, error) {
	if hop.Version != quoteunified.PoolVersionV3 {
		return false, nil
	}
	if a.readiness != nil && !a.readiness.IsV3PoolReady(hop.PoolV3) {
		return true, fmt.Errorf("pool %s is not ready", hop.PoolV3.Hex())
	}
	return true, nil
}

type pancakeV3ProtocolAdapter struct {
	pools     marketpancake.PoolRepository
	registry  PoolRegistry[common.Address]
	readiness PancakeV3ReadinessChecker
}

func NewPancakeV3ProtocolAdapter(pools marketpancake.PoolRepository, registry PoolRegistry[common.Address], readiness PancakeV3ReadinessChecker) ProtocolAdapter {
	if pools == nil || registry == nil {
		return nil
	}
	return &pancakeV3ProtocolAdapter{pools: pools, registry: registry, readiness: readiness}
}

func (a *pancakeV3ProtocolAdapter) Name() string { return "pancakev3" }

func (a *pancakeV3ProtocolAdapter) LoadEdges(ctx context.Context) ([]quoteunified.PoolEdge, error) {
	addresses, err := a.registry.List(ctx)
	if err != nil {
		return nil, err
	}
	edges := make([]quoteunified.PoolEdge, 0, len(addresses))
	for _, address := range addresses {
		pool, err := a.pools.Get(ctx, address)
		if err != nil {
			return nil, fmt.Errorf("load pool %s: %w", address.Hex(), err)
		}
		if pool != nil {
			edges = append(edges, quoteunified.PoolEdge{Version: quoteunified.PoolVersionPancakeV3, PoolPancakeV3: pool.Address, Token0: pool.Token0, Token1: pool.Token1})
		}
	}
	return edges, nil
}

func (a *pancakeV3ProtocolAdapter) QuoteDirect(ctx context.Context, selector DirectPoolSelector, req Request, quotes *quoteunified.QuoteService) (Response, bool, error) {
	if selector.Address == nil {
		return Response{}, false, nil
	}
	pool, err := a.pools.Get(ctx, *selector.Address)
	if err != nil {
		return Response{}, true, fmt.Errorf("load pool %s: %w", selector.Address.Hex(), err)
	}
	if pool == nil {
		return Response{}, false, nil
	}
	if a.readiness != nil && !a.readiness.IsPancakeV3PoolReady(*selector.Address) {
		return Response{}, true, fmt.Errorf("pool %s is not ready", selector.Address.Hex())
	}
	result, err := quotePancakeV3Direct(quotes, pool, req)
	if err != nil {
		return Response{}, true, fmt.Errorf("quote pool %s: %w", selector.Address.Hex(), err)
	}
	route := quoteunified.NewDirectPancakeV3Route(*selector.Address, req.TokenIn, req.TokenOut)
	return newSinglePoolResponse(req, route, result), true, nil
}

func (a *pancakeV3ProtocolAdapter) LoadRoutePool(ctx context.Context, hop quoteunified.RouteHop, pools *quoteunified.RoutePools) (bool, error) {
	if hop.Version != quoteunified.PoolVersionPancakeV3 {
		return false, nil
	}
	if pools.PancakeV3[hop.PoolPancakeV3] != nil {
		return true, nil
	}
	pool, err := a.pools.Get(ctx, hop.PoolPancakeV3)
	if err != nil {
		return true, fmt.Errorf("load pool %s: %w", hop.PoolPancakeV3.Hex(), err)
	}
	if pool == nil {
		return true, fmt.Errorf("pool %s not found", hop.PoolPancakeV3.Hex())
	}
	pools.PancakeV3[hop.PoolPancakeV3] = pool
	return true, nil
}

func (a *pancakeV3ProtocolAdapter) CheckRouteHopReady(hop quoteunified.RouteHop) (bool, error) {
	if hop.Version != quoteunified.PoolVersionPancakeV3 {
		return false, nil
	}
	if a.readiness != nil && !a.readiness.IsPancakeV3PoolReady(hop.PoolPancakeV3) {
		return true, fmt.Errorf("pool %s is not ready", hop.PoolPancakeV3.Hex())
	}
	return true, nil
}

type quickSwapV3ProtocolAdapter struct {
	pools     marketquick.PoolRepository
	registry  PoolRegistry[common.Address]
	readiness QuickSwapV3ReadinessChecker
}

func NewQuickSwapV3ProtocolAdapter(pools marketquick.PoolRepository, registry PoolRegistry[common.Address], readiness QuickSwapV3ReadinessChecker) ProtocolAdapter {
	if pools == nil || registry == nil {
		return nil
	}
	return &quickSwapV3ProtocolAdapter{pools: pools, registry: registry, readiness: readiness}
}

func (a *quickSwapV3ProtocolAdapter) Name() string { return "quickswapv3" }

func (a *quickSwapV3ProtocolAdapter) LoadEdges(ctx context.Context) ([]quoteunified.PoolEdge, error) {
	addresses, err := a.registry.List(ctx)
	if err != nil {
		return nil, err
	}
	edges := make([]quoteunified.PoolEdge, 0, len(addresses))
	for _, address := range addresses {
		pool, err := a.pools.Get(ctx, address)
		if err != nil {
			return nil, fmt.Errorf("load pool %s: %w", address.Hex(), err)
		}
		if pool != nil {
			edges = append(edges, quoteunified.PoolEdge{Version: quoteunified.PoolVersionQuickSwapV3, PoolQuickSwapV3: pool.Address, Token0: pool.Token0, Token1: pool.Token1})
		}
	}
	return edges, nil
}

func (a *quickSwapV3ProtocolAdapter) QuoteDirect(ctx context.Context, selector DirectPoolSelector, req Request, quotes *quoteunified.QuoteService) (Response, bool, error) {
	if selector.Address == nil {
		return Response{}, false, nil
	}
	pool, err := a.pools.Get(ctx, *selector.Address)
	if err != nil {
		return Response{}, true, fmt.Errorf("load pool %s: %w", selector.Address.Hex(), err)
	}
	if pool == nil {
		return Response{}, false, nil
	}
	if a.readiness != nil && !a.readiness.IsQuickSwapV3PoolReady(*selector.Address) {
		return Response{}, true, fmt.Errorf("pool %s is not ready", selector.Address.Hex())
	}
	result, err := quoteQuickSwapV3Direct(quotes, pool, req)
	if err != nil {
		return Response{}, true, fmt.Errorf("quote pool %s: %w", selector.Address.Hex(), err)
	}
	route := quoteunified.NewDirectQuickSwapV3Route(*selector.Address, req.TokenIn, req.TokenOut)
	return newSinglePoolResponse(req, route, result), true, nil
}

func (a *quickSwapV3ProtocolAdapter) LoadRoutePool(ctx context.Context, hop quoteunified.RouteHop, pools *quoteunified.RoutePools) (bool, error) {
	if hop.Version != quoteunified.PoolVersionQuickSwapV3 {
		return false, nil
	}
	if pools.QuickSwapV3[hop.PoolQuickSwapV3] != nil {
		return true, nil
	}
	pool, err := a.pools.Get(ctx, hop.PoolQuickSwapV3)
	if err != nil {
		return true, fmt.Errorf("load pool %s: %w", hop.PoolQuickSwapV3.Hex(), err)
	}
	if pool == nil {
		return true, fmt.Errorf("pool %s not found", hop.PoolQuickSwapV3.Hex())
	}
	pools.QuickSwapV3[hop.PoolQuickSwapV3] = pool
	return true, nil
}

func (a *quickSwapV3ProtocolAdapter) CheckRouteHopReady(hop quoteunified.RouteHop) (bool, error) {
	if hop.Version != quoteunified.PoolVersionQuickSwapV3 {
		return false, nil
	}
	if a.readiness != nil && !a.readiness.IsQuickSwapV3PoolReady(hop.PoolQuickSwapV3) {
		return true, fmt.Errorf("pool %s is not ready", hop.PoolQuickSwapV3.Hex())
	}
	return true, nil
}

type univ4ProtocolAdapter struct {
	pools     marketuniv4.PoolRepository
	registry  PoolRegistry[marketuniv4.PoolID]
	readiness Univ4ReadinessChecker
}

func NewUniv4ProtocolAdapter(pools marketuniv4.PoolRepository, registry PoolRegistry[marketuniv4.PoolID], readiness Univ4ReadinessChecker) ProtocolAdapter {
	if pools == nil || registry == nil {
		return nil
	}
	return &univ4ProtocolAdapter{pools: pools, registry: registry, readiness: readiness}
}

func (a *univ4ProtocolAdapter) Name() string { return "univ4" }

func (a *univ4ProtocolAdapter) LoadEdges(ctx context.Context) ([]quoteunified.PoolEdge, error) {
	ids, err := a.registry.List(ctx)
	if err != nil {
		return nil, err
	}
	edges := make([]quoteunified.PoolEdge, 0, len(ids))
	for _, id := range ids {
		pool, err := a.pools.Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("load pool %s: %w", id.String(), err)
		}
		if pool != nil {
			edges = append(edges, quoteunified.PoolEdge{Version: quoteunified.PoolVersionV4, PoolV4: pool.ID, Token0: pool.Key.Currency0, Token1: pool.Key.Currency1})
		}
	}
	return edges, nil
}

func (a *univ4ProtocolAdapter) QuoteDirect(ctx context.Context, selector DirectPoolSelector, req Request, quotes *quoteunified.QuoteService) (Response, bool, error) {
	if selector.Univ4ID == nil {
		return Response{}, false, nil
	}
	pool, err := a.pools.Get(ctx, *selector.Univ4ID)
	if err != nil {
		return Response{}, true, fmt.Errorf("load pool %s: %w", selector.Univ4ID.String(), err)
	}
	if pool == nil {
		return Response{}, false, nil
	}
	if a.readiness != nil && !a.readiness.IsV4PoolReady(*selector.Univ4ID) {
		return Response{}, true, fmt.Errorf("pool %s is not ready", selector.Univ4ID.String())
	}
	result, err := quoteV4Direct(quotes, pool, req)
	if err != nil {
		return Response{}, true, fmt.Errorf("quote pool %s: %w", selector.Univ4ID.String(), err)
	}
	route := quoteunified.NewDirectV4Route(*selector.Univ4ID, req.TokenIn, req.TokenOut)
	return newSinglePoolResponse(req, route, result), true, nil
}

func (a *univ4ProtocolAdapter) LoadRoutePool(ctx context.Context, hop quoteunified.RouteHop, pools *quoteunified.RoutePools) (bool, error) {
	if hop.Version != quoteunified.PoolVersionV4 {
		return false, nil
	}
	if pools.V4[hop.PoolV4] != nil {
		return true, nil
	}
	pool, err := a.pools.Get(ctx, hop.PoolV4)
	if err != nil {
		return true, fmt.Errorf("load pool %s: %w", hop.PoolV4.String(), err)
	}
	if pool == nil {
		return true, fmt.Errorf("pool %s not found", hop.PoolV4.String())
	}
	pools.V4[hop.PoolV4] = pool
	return true, nil
}

func (a *univ4ProtocolAdapter) CheckRouteHopReady(hop quoteunified.RouteHop) (bool, error) {
	if hop.Version != quoteunified.PoolVersionV4 {
		return false, nil
	}
	if a.readiness != nil && !a.readiness.IsV4PoolReady(hop.PoolV4) {
		return true, fmt.Errorf("pool %s is not ready", hop.PoolV4.String())
	}
	return true, nil
}

type balancerProtocolAdapter struct {
	pools     marketbalancer.PoolRepository
	registry  BalancerPoolRegistry
	readiness BalancerReadinessChecker
}

func NewBalancerProtocolAdapter(pools marketbalancer.PoolRepository, registry BalancerPoolRegistry, readiness BalancerReadinessChecker) ProtocolAdapter {
	if pools == nil || registry == nil {
		return nil
	}
	return &balancerProtocolAdapter{pools: pools, registry: registry, readiness: readiness}
}

func (a *balancerProtocolAdapter) Name() string { return "balancer" }

func (a *balancerProtocolAdapter) LoadEdges(ctx context.Context) ([]quoteunified.PoolEdge, error) {
	ids, err := a.registry.List(ctx)
	if err != nil {
		return nil, err
	}
	var edges []quoteunified.PoolEdge
	for _, id := range ids {
		pool, err := a.pools.Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("load pool %s: %w", id.String(), err)
		}
		if pool == nil {
			continue
		}
		for i := 0; i < len(pool.Tokens); i++ {
			for j := i + 1; j < len(pool.Tokens); j++ {
				edges = append(edges, quoteunified.PoolEdge{Version: quoteunified.PoolVersionBalancer, PoolBalancer: pool.ID, Token0: pool.Tokens[i], Token1: pool.Tokens[j]})
			}
		}
	}
	return edges, nil
}

func (a *balancerProtocolAdapter) QuoteDirect(ctx context.Context, selector DirectPoolSelector, req Request, quotes *quoteunified.QuoteService) (Response, bool, error) {
	id, selected, err := a.resolveSelector(ctx, selector)
	if err != nil || !selected {
		return Response{}, selected, err
	}
	pool, err := a.pools.Get(ctx, id)
	if err != nil {
		return Response{}, true, fmt.Errorf("load pool %s: %w", id.String(), err)
	}
	if pool == nil {
		return Response{}, false, nil
	}
	if a.readiness != nil && !a.readiness.IsBalancerPoolReady(id) {
		return Response{}, true, fmt.Errorf("pool %s is not ready", id.String())
	}
	result, err := quoteBalancerDirect(quotes, pool, req)
	if err != nil {
		return Response{}, true, fmt.Errorf("quote pool %s: %w", id.String(), err)
	}
	route := quoteunified.NewDirectBalancerRoute(id, req.TokenIn, req.TokenOut)
	return newSinglePoolResponse(req, route, result), true, nil
}

func (a *balancerProtocolAdapter) resolveSelector(ctx context.Context, selector DirectPoolSelector) (marketbalancer.PoolID, bool, error) {
	if selector.BalancerPoolID != nil {
		return *selector.BalancerPoolID, true, nil
	}
	if selector.Univ4ID != nil {
		return marketbalancer.PoolID(selector.Univ4ID.Hash()), true, nil
	}
	if selector.Address == nil {
		return marketbalancer.PoolID{}, false, nil
	}
	ids, err := a.registry.List(ctx)
	if err != nil {
		return marketbalancer.PoolID{}, true, fmt.Errorf("list pools: %w", err)
	}
	for _, id := range ids {
		spec, err := a.registry.GetSpec(ctx, id)
		if err != nil {
			return marketbalancer.PoolID{}, true, fmt.Errorf("load pool spec %s: %w", id.String(), err)
		}
		if spec.Address == *selector.Address {
			return id, true, nil
		}
	}
	return marketbalancer.PoolID{}, false, nil
}

func (a *balancerProtocolAdapter) LoadRoutePool(ctx context.Context, hop quoteunified.RouteHop, pools *quoteunified.RoutePools) (bool, error) {
	if hop.Version != quoteunified.PoolVersionBalancer {
		return false, nil
	}
	if pools.Balancer[hop.PoolBalancer] != nil {
		return true, nil
	}
	pool, err := a.pools.Get(ctx, hop.PoolBalancer)
	if err != nil {
		return true, fmt.Errorf("load pool %s: %w", hop.PoolBalancer.String(), err)
	}
	if pool == nil {
		return true, fmt.Errorf("pool %s not found", hop.PoolBalancer.String())
	}
	pools.Balancer[hop.PoolBalancer] = pool
	return true, nil
}

func (a *balancerProtocolAdapter) CheckRouteHopReady(hop quoteunified.RouteHop) (bool, error) {
	if hop.Version != quoteunified.PoolVersionBalancer {
		return false, nil
	}
	if a.readiness != nil && !a.readiness.IsBalancerPoolReady(hop.PoolBalancer) {
		return true, fmt.Errorf("pool %s is not ready", hop.PoolBalancer.String())
	}
	return true, nil
}

func quoteV3Direct(quotes *quoteunified.QuoteService, pool *marketuniv3.Pool, req Request) (quoteshared.QuoteResult, error) {
	if req.IsExactInput() {
		return quotes.QuoteExactInputV3(pool, req.TokenIn, req.TokenOut, req.AmountIn)
	}
	return quotes.QuoteExactOutputV3(pool, req.TokenIn, req.TokenOut, req.AmountOut)
}

func quotePancakeV3Direct(quotes *quoteunified.QuoteService, pool *marketpancake.Pool, req Request) (quoteshared.QuoteResult, error) {
	if req.IsExactInput() {
		return quotes.QuoteExactInputPancakeV3(pool, req.TokenIn, req.TokenOut, req.AmountIn)
	}
	return quotes.QuoteExactOutputPancakeV3(pool, req.TokenIn, req.TokenOut, req.AmountOut)
}

func quoteQuickSwapV3Direct(quotes *quoteunified.QuoteService, pool *marketquick.Pool, req Request) (quoteshared.QuoteResult, error) {
	if req.IsExactInput() {
		return quotes.QuoteExactInputQuickSwapV3(pool, req.TokenIn, req.TokenOut, req.AmountIn)
	}
	return quotes.QuoteExactOutputQuickSwapV3(pool, req.TokenIn, req.TokenOut, req.AmountOut)
}

func quoteV4Direct(quotes *quoteunified.QuoteService, pool *marketuniv4.Pool, req Request) (quoteshared.QuoteResult, error) {
	if req.IsExactInput() {
		return quotes.QuoteExactInputV4(pool, req.TokenIn, req.TokenOut, req.AmountIn)
	}
	return quotes.QuoteExactOutputV4(pool, req.TokenIn, req.TokenOut, req.AmountOut)
}

func quoteBalancerDirect(quotes *quoteunified.QuoteService, pool *marketbalancer.Pool, req Request) (quoteshared.QuoteResult, error) {
	if req.IsExactInput() {
		return quotes.QuoteExactInputBalancer(pool, req.TokenIn, req.TokenOut, req.AmountIn)
	}
	return quotes.QuoteExactOutputBalancer(pool, req.TokenIn, req.TokenOut, req.AmountOut)
}
