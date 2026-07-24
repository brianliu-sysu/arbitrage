package arbitrageapp

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
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
	Logger                    *zap.Logger
	Pools                     marketuniv3.PoolRepository
	PancakePools              marketpancake.PoolRepository
	QuickSwapPools            marketquick.PoolRepository
	V4Pools                   marketuniv4.PoolRepository
	BalancerPools             marketbalancer.PoolRepository
	Registry                  marketuniv3.PoolRegistry
	PancakeRegistry           marketpancake.PoolRegistry
	QuickSwapRegistry         marketquick.PoolRegistry
	V4Registry                marketuniv4.PoolRegistry
	BalancerRegistry          marketbalancer.PoolRegistry
	Quotes                    *quoteunified.QuoteService
	Gas                       domainarb.GasEstimator
	Strategies                []domainarb.Strategy
	TriangleEnabled           bool
	SpreadEnabled             bool
	ConfiguredStartTokens     []common.Address
	SpreadStartTokens         []common.Address
	MinNetProfitWei           *big.Int
	SpreadMinNetProfitWei     *big.Int
	Readiness                 ReadinessChecker
	Repository                domainarb.OpportunityRepository
	Executor                  ContractExecutor
	ExecutionHead             ExecutionHeadReader
	ExecutionBuilder          ExecutionPlanBuilder
	Execution                 ExecutionConfig
	LivePlan                  LivePlanConfig
	FlashLoanOptions          []domainarb.FlashLoanOption
	MinAmount                 *big.Int
	MaxAmount                 *big.Int
	OptimizerIterations       int
	Routes                    []domainarb.RouteRef
	PoolGraph                 quoteunified.PoolGraph
	EnabledProtocols          []SyncProtocol
	MarketStore               MarketPublisher
	MarketVersion             MarketVersionReader
	OpportunityPools          marketuniv3.PoolRepository
	OpportunityPancakePools   marketpancake.PoolRepository
	OpportunityQuickSwapPools marketquick.PoolRepository
	OpportunityV4Pools        marketuniv4.PoolRepository
	OpportunityBalancerPools  marketbalancer.PoolRepository
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

// PoolGraphUpdater receives the latest routing graph after pool synchronization.
type PoolGraphUpdater interface {
	SetPoolGraph(quoteunified.PoolGraph)
}

// Services bundles arbitrage application services.
type Services struct {
	Scan          *ScanService
	Opportunities *OpportunityService
	Publish       *PublishService
	Coordinator   *BlockCoordinator

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
	gasWrappedNative      common.Address
	poolGraph             quoteunified.PoolGraph
	poolGraphUpdaters     []PoolGraphUpdater
}

func NewServices(deps ServiceDeps) *Services {
	opportunityPools := deps.OpportunityPools
	if opportunityPools == nil {
		opportunityPools = deps.Pools
	}
	opportunityPancakePools := deps.OpportunityPancakePools
	if opportunityPancakePools == nil {
		opportunityPancakePools = deps.PancakePools
	}
	opportunityQuickSwapPools := deps.OpportunityQuickSwapPools
	if opportunityQuickSwapPools == nil {
		opportunityQuickSwapPools = deps.QuickSwapPools
	}
	opportunityV4Pools := deps.OpportunityV4Pools
	if opportunityV4Pools == nil {
		opportunityV4Pools = deps.V4Pools
	}
	opportunityBalancerPools := deps.OpportunityBalancerPools
	if opportunityBalancerPools == nil {
		opportunityBalancerPools = deps.BalancerPools
	}
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

	logger := deps.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	strategies := buildArbitrageStrategies(deps, configuredStartTokens, spreadStartTokens, minNetProfitWei, spreadMinNetProfitWei)

	scan := NewScanService(domainarb.NewDependencyGraph())
	scan.RegisterRoutes(deps.Routes)
	var poolGraph quoteunified.PoolGraph
	if graph, err := loadPoolGraph(context.Background(), deps); err == nil {
		poolGraph = graph
		registerMonitoredRoutes(scan, strategies, graph)
	} else if errors.Is(err, ErrNoPoolsAvailable) {
		logger.Debug("initial arbitrage pool graph deferred until pool bootstrap")
	} else {
		logger.Error("build initial arbitrage pool graph failed", zap.Error(err))
	}

	publishers := []OpportunityPublisher{NewLogPublisher(logger)}
	if deps.Repository != nil {
		publishers = append(publishers, NewRepositoryPublisher(deps.Repository))
	}
	var poolGraphUpdaters []PoolGraphUpdater
	if deps.Execution.Enabled {
		builder := deps.ExecutionBuilder
		if builder == nil {
			encoder := NewLiveCalldataEncoder(deps.LivePlan, NewRepositoryRoutePoolLoader(
				deps.Pools,
				deps.PancakePools,
				deps.QuickSwapPools,
				deps.V4Pools,
				deps.BalancerPools,
			))
			builder = NewLiveExecutionPlanBuilder(deps.LivePlan, encoder, poolGraph)
		}
		if updater, ok := builder.(PoolGraphUpdater); ok {
			poolGraphUpdaters = append(poolGraphUpdaters, updater)
		}
		publishers = append(publishers, NewExecutionPublisher(deps.Execution, builder, deps.Executor, deps.Repository, deps.ExecutionHead, logger))
	}

	opportunities := NewOpportunityService(
		opportunityPools,
		opportunityPancakePools,
		opportunityQuickSwapPools,
		opportunityV4Pools,
		opportunityBalancerPools,
		deps.Quotes,
		gas,
		strategies,
		deps.Readiness,
		minAmount,
		maxAmount,
		deps.OptimizerIterations,
		deps.FlashLoanOptions,
		logger,
		deps.MarketVersion,
	)
	opportunities.SetGasCostConversion(poolGraph, deps.LivePlan.WETH)
	if strings.TrimSpace(deps.Execution.FlashbotsRPCURL) != "" && deps.Execution.FlashbotsPaymentBPS > 0 {
		opportunities.SetCoinbasePaymentBPS(uint16(deps.Execution.FlashbotsPaymentBPS))
		opportunities.SetSettlementSlippageBPS(uint16(deps.Execution.SettlementSlippageBPS))
	}

	services := &Services{
		Scan:          scan,
		Opportunities: opportunities,
		Publish:       NewPublishService(publishers...),
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
		gasWrappedNative:      deps.LivePlan.WETH,
		poolGraph:             poolGraph,
		poolGraphUpdaters:     poolGraphUpdaters,
	}
	services.Coordinator = NewBlockCoordinator(
		deps.EnabledProtocols,
		&services.routeMu,
		services.Scan,
		services.Opportunities,
		services.Publish,
		deps.MarketStore,
		logger,
	)
	return services
}

// RegisterPoolGraphUpdater subscribes an execution-plan builder to graph refreshes.
func (s *Services) RegisterPoolGraphUpdater(updater PoolGraphUpdater) {
	if s == nil || updater == nil {
		return
	}
	s.routeMu.Lock()
	s.poolGraphUpdaters = append(s.poolGraphUpdaters, updater)
	graph := s.poolGraph
	s.routeMu.Unlock()
	if graph != nil {
		updater.SetPoolGraph(graph)
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
	if s.Opportunities != nil {
		s.Opportunities.SetGasCostConversion(graph, s.gasWrappedNative)
	}
	s.poolGraph = graph
	for _, updater := range s.poolGraphUpdaters {
		updater.SetPoolGraph(graph)
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
		poolEdgeSources(deps)...,
	)
}

func poolEdgeSources(deps ServiceDeps) []PoolEdgeSource {
	candidates := []PoolEdgeSource{
		NewUniv3PoolEdgeSource(deps.Registry, deps.Pools),
		NewPancakeV3PoolEdgeSource(deps.PancakeRegistry, deps.PancakePools),
		NewQuickSwapV3PoolEdgeSource(deps.QuickSwapRegistry, deps.QuickSwapPools),
		NewUniv4PoolEdgeSource(deps.V4Registry, deps.V4Pools),
		NewBalancerPoolEdgeSource(deps.BalancerRegistry, deps.BalancerPools),
	}
	sources := make([]PoolEdgeSource, 0, len(candidates))
	for _, source := range candidates {
		if source != nil {
			sources = append(sources, source)
		}
	}
	return sources
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
