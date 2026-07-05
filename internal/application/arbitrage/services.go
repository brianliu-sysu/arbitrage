package arbitrageapp

import (
	"context"
	"fmt"
	"math/big"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v3"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// ServiceDeps contains dependencies for arbitrage application services.
type ServiceDeps struct {
	Logger              *zap.Logger
	Pools               marketv3.PoolRepository
	V4Pools             marketv4.PoolRepository
	Registry            marketv3.PoolRegistry
	V4Registry          marketv4.PoolRegistry
	Quotes              *quoteunified.QuoteService
	Gas                 domainarb.GasEstimator
	Strategies          []domainarb.Strategy
	Readiness           ReadinessChecker
	Repository          domainarb.OpportunityRepository
	MinAmount           *big.Int
	MaxAmount           *big.Int
	OptimizerIterations int
	Routes              []domainarb.RouteRef
	PoolGraph           quoteunified.PoolGraph
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
			deps.V4Pools,
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
	if graph == nil {
		if built, err := BuildUnifiedPoolGraph(
			context.Background(),
			deps.Registry,
			deps.Pools,
			deps.V4Registry,
			deps.V4Pools,
		); err == nil {
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
		scan.RegisterUnifiedTriangleRoutes(graph, strategy.StartToken)
	}
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
