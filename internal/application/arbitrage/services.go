package arbitrageapp

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/brianliu-sysu/uniswapv3/internal/application/committedmarket"
	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

var ErrNoPoolsAvailable = errors.New("no pools available for routing")

type RoutingConfig struct {
	Strategies    []domainarb.Strategy
	InitialRoutes []domainarb.RouteRef
}

type OpportunityConfig struct {
	OptimizerIterations int
	FlashLoanOptions    []domainarb.FlashLoanOption
	WrappedNative       common.Address
	CoinbasePaymentBPS  uint16
}

type PublishingDeps struct {
	Repository        domainarb.OpportunityRepository
	Publishers        []OpportunityPublisher
	PoolGraphUpdaters []PoolGraphUpdater
}

// ServiceDeps contains the cohesive inputs required by arbitrage orchestration.
type ServiceDeps struct {
	Logger      *zap.Logger
	Market      committedmarket.Reader
	Quotes      *quoteunified.QuoteService
	Gas         domainarb.GasEstimator
	Routing     RoutingConfig
	Opportunity OpportunityConfig
	Publishing  PublishingDeps
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

	routeMu           sync.Mutex
	mu                sync.RWMutex
	market            committedmarket.Reader
	strategies        []domainarb.Strategy
	logger            *zap.Logger
	gasWrappedNative  common.Address
	poolGraph         quoteunified.PoolGraph
	poolGraphUpdaters []PoolGraphUpdater
}

func NewServices(deps ServiceDeps) *Services {
	gas := deps.Gas
	if gas == nil {
		gas = domainarb.NewStaticGasEstimator(100_000, 80_000, big.NewInt(10))
	}

	logger := deps.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	var poolGraph quoteunified.PoolGraph
	graph, graphErr := loadPoolGraph(deps.Market)
	if graphErr == nil {
		poolGraph = graph
	} else {
		logger.Debug("initial arbitrage pool graph deferred until pool bootstrap")
	}
	strategies := append([]domainarb.Strategy(nil), deps.Routing.Strategies...)

	scan := NewScanService(domainarb.NewDependencyGraph())
	scan.RegisterRoutes(deps.Routing.InitialRoutes)
	registerMonitoredRoutes(scan, strategies, poolGraph)

	publishers := []OpportunityPublisher{NewLogPublisher(logger)}
	if deps.Publishing.Repository != nil {
		publishers = append(publishers, NewRepositoryPublisher(deps.Publishing.Repository))
	}
	publishers = append(publishers, deps.Publishing.Publishers...)

	opportunities := NewOpportunityService(
		deps.Market,
		deps.Quotes,
		gas,
		strategies,
		deps.Opportunity.OptimizerIterations,
		deps.Opportunity.FlashLoanOptions,
		logger,
	)
	opportunities.SetGasCostConversion(poolGraph, deps.Opportunity.WrappedNative)
	if deps.Opportunity.CoinbasePaymentBPS > 0 {
		opportunities.SetCoinbasePaymentBPS(deps.Opportunity.CoinbasePaymentBPS)
	}

	services := &Services{
		Scan:              scan,
		Opportunities:     opportunities,
		Publish:           NewPublishService(publishers...),
		market:            deps.Market,
		strategies:        append([]domainarb.Strategy(nil), strategies...),
		logger:            logger,
		gasWrappedNative:  deps.Opportunity.WrappedNative,
		poolGraph:         poolGraph,
		poolGraphUpdaters: append([]PoolGraphUpdater(nil), deps.Publishing.PoolGraphUpdaters...),
	}
	if poolGraph != nil {
		for _, updater := range services.poolGraphUpdaters {
			if updater != nil {
				updater.SetPoolGraph(poolGraph)
			}
		}
	}
	return services
}

// NewScanScheduler builds the arbitrage scan lifecycle for these services.
func (s *Services) NewScanScheduler() *ScanScheduler {
	if s == nil {
		return nil
	}
	return NewScanScheduler(newBlockScanPipeline(
		&s.routeMu,
		s.Scan,
		s.Opportunities,
		s.Publish,
		s.logger,
	), s.logger)
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

	if err := ctx.Err(); err != nil {
		return 0, err
	}
	graph, err := loadPoolGraph(s.market)
	if err != nil {
		s.Scan.ReplaceMonitoredRoutes(nil)
		return 0, err
	}
	if s.Opportunities != nil {
		s.Opportunities.SetGasCostConversion(graph, s.gasWrappedNative)
	}
	s.poolGraph = graph
	for _, updater := range s.poolGraphUpdaters {
		if updater != nil {
			updater.SetPoolGraph(graph)
		}
	}

	strategies := s.strategiesSnapshot()

	return registerMonitoredRoutes(s.Scan, strategies, graph), nil
}

func (s *Services) strategiesSnapshot() []domainarb.Strategy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]domainarb.Strategy(nil), s.strategies...)
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

func loadPoolGraph(market committedmarket.Reader) (quoteunified.PoolGraph, error) {
	if market == nil {
		return nil, ErrNoPoolsAvailable
	}
	snapshot := market.Snapshot()
	if snapshot == nil || snapshot.Version().IsZero() {
		return nil, ErrNoPoolsAvailable
	}
	edges := snapshot.PoolEdges()
	if len(edges) == 0 {
		return nil, ErrNoPoolsAvailable
	}
	return quoteunified.NewStaticPoolGraph(edges), nil
}
