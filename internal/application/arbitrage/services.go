package arbitrageapp

import (
	"context"
	"fmt"
	"math/big"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
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
	V4Pools               marketuniv4.PoolRepository
	BalancerPools         marketbalancer.PoolRepository
	Registry              marketuniv3.PoolRegistry
	PancakeRegistry       marketpancake.PoolRegistry
	V4Registry            marketuniv4.PoolRegistry
	BalancerRegistry      marketbalancer.PoolRegistry
	Quotes                *quoteunified.QuoteService
	Gas                   domainarb.GasEstimator
	Strategies            []domainarb.Strategy
	ConfiguredStartTokens []common.Address
	MinNetProfitWei       *big.Int
	Readiness             ReadinessChecker
	Repository            domainarb.OpportunityRepository
	MinAmount             *big.Int
	MaxAmount             *big.Int
	OptimizerIterations   int
	Routes                []domainarb.RouteRef
	PoolGraph             quoteunified.PoolGraph
}

type routeRefreshDeps struct {
	Registry         marketuniv3.PoolRegistry
	Pools            marketuniv3.PoolRepository
	PancakeRegistry  marketpancake.PoolRegistry
	PancakePools     marketpancake.PoolRepository
	V4Registry       marketuniv4.PoolRegistry
	V4Pools          marketuniv4.PoolRepository
	BalancerRegistry marketbalancer.PoolRegistry
	BalancerPools    marketbalancer.PoolRepository
}

// Services bundles arbitrage application services.
type Services struct {
	Scan          *ScanService
	Opportunities *OpportunityService
	Publish       *PublishService

	routeDeps             routeRefreshDeps
	configuredStartTokens []common.Address
	minNetProfitWei       *big.Int
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
	if len(configuredStartTokens) == 0 {
		configuredStartTokens = dedupeStartTokens(collectStrategyStartTokens(deps.Strategies))
	}
	minNetProfitWei := deps.MinNetProfitWei
	if minNetProfitWei == nil {
		minNetProfitWei = big.NewInt(1)
	}

	strategies := buildTriangleStrategies(deps, configuredStartTokens, minNetProfitWei)

	scan := NewScanService(domainarb.NewDependencyGraph())
	scan.RegisterRoutes(deps.Routes)
	if graph, err := loadPoolGraph(context.Background(), deps); err == nil {
		registerTriangleRoutes(scan, strategies, graph)
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
			deps.V4Pools,
			deps.BalancerPools,
			deps.Quotes,
			gas,
			strategies,
			deps.Readiness,
			minAmount,
			maxAmount,
			deps.OptimizerIterations,
			logger,
		),
		Publish: NewPublishService(publishers...),
		routeDeps: routeRefreshDeps{
			Registry:         deps.Registry,
			Pools:            deps.Pools,
			PancakeRegistry:  deps.PancakeRegistry,
			PancakePools:     deps.PancakePools,
			V4Registry:       deps.V4Registry,
			V4Pools:          deps.V4Pools,
			BalancerRegistry: deps.BalancerRegistry,
			BalancerPools:    deps.BalancerPools,
		},
		configuredStartTokens: configuredStartTokens,
		minNetProfitWei:       minNetProfitWei,
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

func (s *Services) updateTriangleStrategies(startTokens []common.Address) {
	strategies := TriangleStrategies(startTokens, s.minNetProfitWei)
	s.strategies = strategies
	if s.Opportunities != nil {
		s.Opportunities.SetStrategies(strategies)
	}
}

// StartTokens returns the active triangle start tokens.
func (s *Services) StartTokens() []common.Address {
	if s == nil {
		return nil
	}
	return collectStrategyStartTokens(s.strategies)
}

// RefreshTriangleRoutes rebuilds the pool graph from synced state and re-registers triangle routes.
func (s *Services) RefreshTriangleRoutes(ctx context.Context) (int, error) {
	if s == nil || s.Scan == nil {
		return 0, fmt.Errorf("arbitrage scan service is not configured")
	}

	graph, err := loadPoolGraph(ctx, routeRefreshDepsToServiceDeps(s.routeDeps))
	if err != nil {
		s.updateTriangleStrategies(s.configuredStartTokens)
		s.Scan.ClearTriangleRoutes()
		return 0, err
	}

	startTokens := ResolveTriangleStartTokens(s.configuredStartTokens, graph.Edges(), autoStartTokenCount)
	s.updateTriangleStrategies(startTokens)
	if len(startTokens) == 0 {
		s.Scan.ClearTriangleRoutes()
		return 0, nil
	}

	s.Scan.ClearTriangleRoutes()
	return registerTriangleRoutesOnGraph(s.Scan, graph, s.strategies), nil
}

func routeRefreshDepsToServiceDeps(deps routeRefreshDeps) ServiceDeps {
	return ServiceDeps{
		Registry:         deps.Registry,
		Pools:            deps.Pools,
		PancakeRegistry:  deps.PancakeRegistry,
		PancakePools:     deps.PancakePools,
		V4Registry:       deps.V4Registry,
		V4Pools:          deps.V4Pools,
		BalancerRegistry: deps.BalancerRegistry,
		BalancerPools:    deps.BalancerPools,
	}
}

func buildTriangleStrategies(deps ServiceDeps, configured []common.Address, minNetProfitWei *big.Int) []domainarb.Strategy {
	if len(deps.Strategies) > 0 {
		return deps.Strategies
	}
	if graph, err := loadPoolGraph(context.Background(), deps); err == nil {
		startTokens := ResolveTriangleStartTokens(configured, graph.Edges(), autoStartTokenCount)
		return TriangleStrategies(startTokens, minNetProfitWei)
	}
	return TriangleStrategies(configured, minNetProfitWei)
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
		deps.V4Registry,
		deps.V4Pools,
		deps.BalancerRegistry,
		deps.BalancerPools,
	)
}

func registerTriangleRoutes(scan *ScanService, strategies []domainarb.Strategy, graph quoteunified.PoolGraph) {
	if graph == nil || len(strategies) == 0 {
		return
	}
	registerTriangleRoutesOnGraph(scan, graph, strategies)
}

func registerTriangleRoutesOnGraph(scan *ScanService, graph quoteunified.PoolGraph, strategies []domainarb.Strategy) int {
	total := 0
	for _, strategy := range strategies {
		if strategy.Kind != domainarb.StrategyKindTriangle {
			continue
		}
		total += scan.RegisterUnifiedTriangleRoutes(graph, strategy.StartToken)
	}
	return total
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
