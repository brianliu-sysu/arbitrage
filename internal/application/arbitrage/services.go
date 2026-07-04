package arbitrageapp

import (
	"context"
	"fmt"
	"math/big"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	domainquote "github.com/brianliu-sysu/uniswapv3/internal/domain/quote"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v3"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// ServiceDeps contains dependencies for arbitrage application services.
type ServiceDeps struct {
	Logger              *zap.Logger
	Pools               marketv3.PoolRepository
	Registry            marketv3.PoolRegistry
	Quotes              *domainquote.QuoteService
	Gas                 domainarb.GasEstimator
	Strategies          []domainarb.Strategy
	Readiness           ReadinessChecker
	Repository          domainarb.OpportunityRepository
	MinAmount           *big.Int
	MaxAmount           *big.Int
	OptimizerIterations int
	Routes              []domainarb.RouteRef
	PoolGraph           domainquote.PoolGraph
}

// Services bundles arbitrage application services.
type Services struct {
	Scan          *ScanService
	Opportunities *OpportunityService
	Publish       *PublishService
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

	scan := NewScanService(domainarb.NewDependencyGraph())
	scan.RegisterRoutes(deps.Routes)
	registerTriangleRoutes(scan, deps)

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
			deps.Quotes,
			gas,
			deps.Strategies,
			deps.Readiness,
			minAmount,
			maxAmount,
			deps.OptimizerIterations,
		),
		Publish: NewPublishService(publishers...),
	}
}

func registerTriangleRoutes(scan *ScanService, deps ServiceDeps) {
	graph := deps.PoolGraph
	if graph == nil && deps.Registry != nil && deps.Pools != nil {
		if built, err := BuildPoolGraph(context.Background(), deps.Registry, deps.Pools); err == nil {
			graph = built
		}
	}
	if graph == nil {
		return
	}

	for _, strategy := range deps.Strategies {
		if strategy.Kind != domainarb.StrategyKindTriangle {
			continue
		}
		scan.RegisterTriangleRoutes(graph, strategy.StartToken)
	}
}

// BuildPoolGraph builds a routing graph from tracked pools.
func BuildPoolGraph(ctx context.Context, registry marketv3.PoolRegistry, pools marketv3.PoolRepository) (domainquote.PoolGraph, error) {
	if registry == nil {
		return nil, fmt.Errorf("pool registry is nil")
	}
	if pools == nil {
		return nil, fmt.Errorf("pool repository is nil")
	}

	addresses, err := registry.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pools: %w", err)
	}

	edges := make([]domainquote.PoolEdge, 0, len(addresses))
	for _, address := range addresses {
		pool, err := pools.Get(ctx, address)
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

// OnPoolsChanged implements sync ChangedPoolsListener.
func (s *Services) OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error {
	routes := s.Scan.FindAffected(pools)
	opportunities, err := s.Opportunities.Generate(ctx, GenerateRequest{
		BlockNumber: blockNumber,
		Routes:      routes,
	})
	if err != nil {
		return err
	}
	return s.Publish.Publish(ctx, opportunities)
}

// TriangleStrategies builds triangle strategies for the given start tokens.
func TriangleStrategies(startTokens []common.Address, minNetProfitWei *big.Int) []domainarb.Strategy {
	strategies := make([]domainarb.Strategy, 0, len(startTokens))
	for i, token := range startTokens {
		if token == (common.Address{}) {
			continue
		}
		strategies = append(strategies, domainarb.NewTriangleStrategy(
			fmt.Sprintf("triangle-%d", i),
			token,
			minNetProfitWei,
		))
	}
	return strategies
}
