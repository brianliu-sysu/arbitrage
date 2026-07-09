package arbitrageapp

import (
	"context"
	"fmt"
	"math/big"
	"sync"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// ServiceDeps contains dependencies for arbitrage application services.
type ServiceDeps struct {
	Logger                *zap.Logger
	Pools                 marketuniv3.PoolRepository
	PancakePools          marketpancake.PoolRepository
	QuickSwapPools        marketquick.PoolRepository
	V4Pools               marketuniv4.PoolRepository
	BalancerPools         marketbalancer.PoolRepository
	Registry              marketuniv3.PoolRegistry
	PancakeRegistry       marketpancake.PoolRegistry
	QuickSwapRegistry     marketquick.PoolRegistry
	V4Registry            marketuniv4.PoolRegistry
	BalancerRegistry      marketbalancer.PoolRegistry
	Quotes                *quoteunified.QuoteService
	Gas                   domainarb.GasEstimator
	Strategies            []domainarb.Strategy
	TriangleEnabled       bool
	SpreadEnabled         bool
	ConfiguredStartTokens []common.Address
	SpreadStartTokens     []common.Address
	MinNetProfitWei       *big.Int
	SpreadMinNetProfitWei *big.Int
	Readiness             ReadinessChecker
	Repository            domainarb.OpportunityRepository
	FlashLoanOptions      []domainarb.FlashLoanOption
	MinAmount             *big.Int
	MaxAmount             *big.Int
	OptimizerIterations   int
	Routes                []domainarb.RouteRef
	PoolGraph             quoteunified.PoolGraph
}

type routeRefreshDeps struct {
	Registry          marketuniv3.PoolRegistry
	Pools             marketuniv3.PoolRepository
	PancakeRegistry   marketpancake.PoolRegistry
	PancakePools      marketpancake.PoolRepository
	QuickSwapRegistry marketquick.PoolRegistry
	QuickSwapPools    marketquick.PoolRepository
	V4Registry        marketuniv4.PoolRegistry
	V4Pools           marketuniv4.PoolRepository
	BalancerRegistry  marketbalancer.PoolRegistry
	BalancerPools     marketbalancer.PoolRepository
}

// Services bundles arbitrage application services.
type Services struct {
	Scan          *ScanService
	Opportunities *OpportunityService
	Publish       *PublishService

	routeMu               sync.Mutex
	mu                    sync.RWMutex
	routeDeps             routeRefreshDeps
	configuredStartTokens []common.Address
	spreadStartTokens     []common.Address
	minNetProfitWei       *big.Int
	spreadMinNetProfitWei *big.Int
	triangleEnabled       bool
	spreadEnabled         bool
	strategies            []domainarb.Strategy
	readiness             ReadinessChecker
	logger                *zap.Logger
}

func NewServices(deps ServiceDeps) *Services {
	minAmount := deps.MinAmount
	if minAmount == nil {
		minAmount = big.NewInt(1_000_000)
	}
	maxAmount := deps.MaxAmount
	if maxAmount == nil {
		maxAmount = big.NewInt(100_000_000_000_000)
	}

	gas := deps.Gas
	if gas == nil {
		gas = domainarb.NewStaticGasEstimator(100_000, 80_000, big.NewInt(10))
	}

	configuredStartTokens := append([]common.Address(nil), deps.ConfiguredStartTokens...)
	spreadStartTokens := append([]common.Address(nil), deps.SpreadStartTokens...)
	minNetProfitWei := deps.MinNetProfitWei
	if minNetProfitWei == nil {
		minNetProfitWei = big.NewInt(1)
	}
	spreadMinNetProfitWei := deps.SpreadMinNetProfitWei
	if spreadMinNetProfitWei == nil {
		spreadMinNetProfitWei = minNetProfitWei
	}

	strategies := buildArbitrageStrategies(deps, configuredStartTokens, spreadStartTokens, minNetProfitWei, spreadMinNetProfitWei)

	scan := NewScanService(domainarb.NewDependencyGraph())
	scan.RegisterRoutes(deps.Routes)
	if graph, err := loadPoolGraph(context.Background(), deps); err == nil {
		registerMonitoredRoutes(scan, strategies, graph)
	}

	logger := deps.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	publishers := []OpportunityPublisher{NewLogPublisher(logger)}
	if deps.Repository != nil {
		publishers = append(publishers, NewRepositoryPublisher(deps.Repository))
	}

	return &Services{
		Scan: scan,
		Opportunities: NewOpportunityService(
			deps.Pools,
			deps.PancakePools,
			deps.QuickSwapPools,
			deps.V4Pools,
			deps.BalancerPools,
			deps.Quotes,
			gas,
			strategies,
			deps.Readiness,
			minAmount,
			maxAmount,
			deps.OptimizerIterations,
			deps.FlashLoanOptions,
			logger,
		),
		Publish: NewPublishService(publishers...),
		routeDeps: routeRefreshDeps{
			Registry:          deps.Registry,
			Pools:             deps.Pools,
			PancakeRegistry:   deps.PancakeRegistry,
			PancakePools:      deps.PancakePools,
			QuickSwapRegistry: deps.QuickSwapRegistry,
			QuickSwapPools:    deps.QuickSwapPools,
			V4Registry:        deps.V4Registry,
			V4Pools:           deps.V4Pools,
			BalancerRegistry:  deps.BalancerRegistry,
			BalancerPools:     deps.BalancerPools,
		},
		configuredStartTokens: configuredStartTokens,
		spreadStartTokens:     spreadStartTokens,
		minNetProfitWei:       minNetProfitWei,
		spreadMinNetProfitWei: spreadMinNetProfitWei,
		triangleEnabled:       deps.TriangleEnabled,
		spreadEnabled:         deps.SpreadEnabled,
		strategies:            append([]domainarb.Strategy(nil), strategies...),
		readiness:             deps.Readiness,
		logger:                logger,
	}
}

func collectStrategyStartTokens(strategies []domainarb.Strategy) []common.Address {
	tokens := make([]common.Address, 0, len(strategies))
	for _, strategy := range strategies {
		if strategy.StartToken == (common.Address{}) {
			continue
		}
		tokens = append(tokens, strategy.StartToken)
	}
	return tokens
}

// StartTokens returns the active arbitrage start tokens across enabled strategies.
func (s *Services) StartTokens() []common.Address {
	if s == nil {
		return nil
	}
	return dedupeStartTokens(collectStrategyStartTokens(s.strategiesSnapshot()))
}

// RefreshArbitrageRoutes rebuilds monitored triangle and spread routes from synced pool state.
func (s *Services) RefreshArbitrageRoutes(ctx context.Context) (int, error) {
	if s == nil || s.Scan == nil {
		return 0, fmt.Errorf("arbitrage scan service is not configured")
	}
	s.routeMu.Lock()
	defer s.routeMu.Unlock()

	graph, err := loadPoolGraph(ctx, routeRefreshDepsToServiceDeps(s.routeDeps))
	if err != nil {
		s.rebuildStrategiesOnGraphError()
		s.Scan.ReplaceMonitoredRoutes(nil)
		return 0, err
	}

	triangleTokens := ResolveTriangleStartTokens(s.configuredStartTokens, graph.Edges(), autoStartTokenCount)
	spreadTokens := ResolveSpreadStartTokens(s.spreadStartTokens, triangleTokens, graph.Edges())
	s.updateArbitrageStrategies(triangleTokens, spreadTokens)
	strategies := s.strategiesSnapshot()

	return registerMonitoredRoutes(s.Scan, strategies, graph), nil
}

// RefreshTriangleRoutes rebuilds triangle routes only. Prefer RefreshArbitrageRoutes when spread is enabled.
func (s *Services) RefreshTriangleRoutes(ctx context.Context) (int, error) {
	return s.RefreshArbitrageRoutes(ctx)
}

func (s *Services) rebuildStrategiesOnGraphError() {
	s.updateArbitrageStrategies(s.configuredStartTokens, s.spreadStartTokens)
}

func (s *Services) updateArbitrageStrategies(triangleTokens, spreadTokens []common.Address) {
	strategies := SpreadAndTriangleStrategies(
		s.triangleEnabled,
		s.spreadEnabled,
		triangleTokens,
		spreadTokens,
		s.minNetProfitWei,
		s.spreadMinNetProfitWei,
	)
	s.mu.Lock()
	s.strategies = append([]domainarb.Strategy(nil), strategies...)
	s.mu.Unlock()
	if s.Opportunities != nil {
		s.Opportunities.SetStrategies(strategies)
	}
}

func (s *Services) strategiesSnapshot() []domainarb.Strategy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]domainarb.Strategy(nil), s.strategies...)
}

func routeRefreshDepsToServiceDeps(deps routeRefreshDeps) ServiceDeps {
	return ServiceDeps{
		Registry:          deps.Registry,
		Pools:             deps.Pools,
		PancakeRegistry:   deps.PancakeRegistry,
		PancakePools:      deps.PancakePools,
		QuickSwapRegistry: deps.QuickSwapRegistry,
		QuickSwapPools:    deps.QuickSwapPools,
		V4Registry:        deps.V4Registry,
		V4Pools:           deps.V4Pools,
		BalancerRegistry:  deps.BalancerRegistry,
		BalancerPools:     deps.BalancerPools,
	}
}

func buildArbitrageStrategies(
	deps ServiceDeps,
	configured []common.Address,
	spreadConfigured []common.Address,
	minNetProfitWei, spreadMinNetProfitWei *big.Int,
) []domainarb.Strategy {
	if len(deps.Strategies) > 0 {
		return deps.Strategies
	}
	if graph, err := loadPoolGraph(context.Background(), deps); err == nil {
		triangleTokens := ResolveTriangleStartTokens(configured, graph.Edges(), autoStartTokenCount)
		spreadTokens := ResolveSpreadStartTokens(spreadConfigured, triangleTokens, graph.Edges())
		return SpreadAndTriangleStrategies(
			deps.TriangleEnabled,
			deps.SpreadEnabled,
			triangleTokens,
			spreadTokens,
			minNetProfitWei,
			spreadMinNetProfitWei,
		)
	}
	return SpreadAndTriangleStrategies(
		deps.TriangleEnabled,
		deps.SpreadEnabled,
		configured,
		spreadConfigured,
		minNetProfitWei,
		spreadMinNetProfitWei,
	)
}

func registerMonitoredRoutes(scan *ScanService, strategies []domainarb.Strategy, graph quoteunified.PoolGraph) int {
	if scan == nil {
		return 0
	}
	return scan.ReplaceMonitoredRoutes(buildMonitoredRoutes(strategies, graph))
}

func buildMonitoredRoutes(strategies []domainarb.Strategy, graph quoteunified.PoolGraph) []domainarb.RouteRef {
	if graph == nil || len(strategies) == 0 {
		return nil
	}
	routes := make([]domainarb.RouteRef, 0)
	seen := make(map[string]struct{})
	for _, strategy := range strategies {
		switch strategy.Kind {
		case domainarb.StrategyKindTriangle:
			for _, route := range domainarb.FindUnifiedTriangleRoutes(graph, strategy.StartToken) {
				routeRef := domainarb.RouteRef{
					ID:    domainarb.UnifiedTriangleRouteIDWithPools(route),
					Route: route,
				}
				if _, ok := seen[routeRef.ID]; ok {
					continue
				}
				seen[routeRef.ID] = struct{}{}
				routes = append(routes, routeRef)
			}
		case domainarb.StrategyKindSpread:
			for _, route := range domainarb.FindUnifiedSpreadRoutes(graph, strategy.StartToken) {
				routeRef := domainarb.RouteRef{
					ID:    domainarb.UnifiedSpreadRouteIDWithPools(route),
					Route: route,
				}
				if _, ok := seen[routeRef.ID]; ok {
					continue
				}
				seen[routeRef.ID] = struct{}{}
				routes = append(routes, routeRef)
			}
		}
	}
	return routes
}

// SpreadAndTriangleStrategies builds enabled arbitrage strategies for the given start tokens.
func SpreadAndTriangleStrategies(
	triangleEnabled, spreadEnabled bool,
	triangleTokens, spreadTokens []common.Address,
	triangleMinNetProfit, spreadMinNetProfit *big.Int,
) []domainarb.Strategy {
	strategies := make([]domainarb.Strategy, 0)
	if triangleEnabled {
		strategies = append(strategies, TriangleStrategies(triangleTokens, triangleMinNetProfit)...)
	}
	if spreadEnabled {
		strategies = append(strategies, SpreadStrategies(spreadTokens, spreadMinNetProfit)...)
	}
	return strategies
}

func loadPoolGraph(ctx context.Context, deps ServiceDeps) (quoteunified.PoolGraph, error) {
	if deps.PoolGraph != nil {
		return deps.PoolGraph, nil
	}
	return BuildUnifiedPoolGraph(
		ctx,
		deps.Registry,
		deps.Pools,
		deps.PancakeRegistry,
		deps.PancakePools,
		deps.QuickSwapRegistry,
		deps.QuickSwapPools,
		deps.V4Registry,
		deps.V4Pools,
		deps.BalancerRegistry,
		deps.BalancerPools,
	)
}

func registerTriangleRoutes(scan *ScanService, strategies []domainarb.Strategy, graph quoteunified.PoolGraph) {
	registerMonitoredRoutes(scan, strategies, graph)
}

func registerTriangleRoutesOnGraph(scan *ScanService, graph quoteunified.PoolGraph, strategies []domainarb.Strategy) int {
	return registerMonitoredRoutes(scan, strategies, graph)
}

// TriangleStrategies builds triangle strategies for the given start tokens.
func TriangleStrategies(startTokens []common.Address, minNetProfitWei *big.Int) []domainarb.Strategy {
	deduped := dedupeStartTokens(startTokens)
	strategies := make([]domainarb.Strategy, 0, len(deduped))
	for i, token := range deduped {
		strategies = append(strategies, domainarb.NewTriangleStrategy(
			fmt.Sprintf("triangle-%d", i),
			token,
			minNetProfitWei,
		))
	}
	return strategies
}

// SpreadStrategies builds cross-pool spread strategies for the given start tokens.
func SpreadStrategies(startTokens []common.Address, minNetProfitWei *big.Int) []domainarb.Strategy {
	deduped := dedupeStartTokens(startTokens)
	strategies := make([]domainarb.Strategy, 0, len(deduped))
	for i, token := range deduped {
		strategies = append(strategies, domainarb.NewSpreadStrategy(
			fmt.Sprintf("spread-%d", i),
			token,
			minNetProfitWei,
		))
	}
	return strategies
}
