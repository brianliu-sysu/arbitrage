package arbitrageapp

import (
	"context"
	"fmt"
	"math/big"
	"time"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// ReadinessChecker gates scanning on pool and system readiness across protocols.
type ReadinessChecker interface {
	IsSystemReady() bool
	IsV3PoolReady(poolAddress common.Address) bool
	IsPancakeV3PoolReady(poolAddress common.Address) bool
	IsV4PoolReady(poolID marketuniv4.PoolID) bool
}

// OpportunityService generates opportunities from affected routes.
type OpportunityService struct {
	univ3Pools       marketuniv3.PoolRepository
	pancakePools  marketpancake.PoolRepository
	univ4Pools       marketuniv4.PoolRepository
	quotes     *quoteunified.QuoteService
	evaluator  *domainarb.Evaluator
	optimizer  *domainarb.Optimizer
	gas        domainarb.GasEstimator
	strategies []domainarb.Strategy
	readiness  ReadinessChecker
	now        func() time.Time
}

// GenerateRequest is the input for opportunity generation.
type GenerateRequest struct {
	BlockNumber uint64
	Routes      []domainarb.RouteRef
}

func NewOpportunityService(
	univ3Pools marketuniv3.PoolRepository,
	pancakePools marketpancake.PoolRepository,
	univ4Pools marketuniv4.PoolRepository,
	quotes *quoteunified.QuoteService,
	gas domainarb.GasEstimator,
	strategies []domainarb.Strategy,
	readiness ReadinessChecker,
	minAmount, maxAmount *big.Int,
	optimizerIterations int,
) *OpportunityService {
	return &OpportunityService{
		univ3Pools:      univ3Pools,
		pancakePools: pancakePools,
		univ4Pools:      univ4Pools,
		quotes:     quotes,
		evaluator:  domainarb.NewEvaluator(),
		optimizer:  domainarb.NewOptimizer(minAmount, maxAmount, optimizerIterations),
		gas:        gas,
		strategies: append([]domainarb.Strategy(nil), strategies...),
		readiness:  readiness,
		now:        time.Now,
	}
}

// SetStrategies replaces the active arbitrage strategies.
func (s *OpportunityService) SetStrategies(strategies []domainarb.Strategy) {
	s.strategies = append([]domainarb.Strategy(nil), strategies...)
}

// Generate evaluates affected routes and returns accepted opportunities.
func (s *OpportunityService) Generate(ctx context.Context, req GenerateRequest) ([]*domainarb.Opportunity, error) {
	if s.readiness != nil && !s.readiness.IsSystemReady() {
		return nil, nil
	}
	if len(req.Routes) == 0 {
		return nil, nil
	}
	if len(s.strategies) == 0 {
		return nil, nil
	}

	opportunities := make([]*domainarb.Opportunity, 0)
	for _, routeRef := range req.Routes {
		if err := s.ensureRouteReady(routeRef); err != nil {
			continue
		}

		for _, strategy := range s.strategies {
			if !matchesStrategy(strategy, routeRef) {
				continue
			}

			opp, err := s.generateForRoute(ctx, req.BlockNumber, strategy, routeRef)
			if err != nil {
				continue
			}
			if opp != nil {
				opportunities = append(opportunities, opp)
			}
		}
	}

	return opportunities, nil
}

func (s *OpportunityService) generateForRoute(
	ctx context.Context,
	blockNumber uint64,
	strategy domainarb.Strategy,
	routeRef domainarb.RouteRef,
) (*domainarb.Opportunity, error) {
	pools, err := s.loadRoutePools(ctx, routeRef.Route)
	if err != nil {
		return nil, err
	}

	quoter := routeQuoter{
		quotes: s.quotes,
		pools:  pools,
		route:  routeRef.Route,
	}
	optimized, err := s.optimizer.Optimize(quoter)
	if err != nil {
		return nil, err
	}
	if optimized.AmountIn.Sign() <= 0 {
		return nil, nil
	}

	gas, err := s.gas.Estimate(ctx, routeRef.Route.Len())
	if err != nil {
		return nil, err
	}

	evaluation := s.evaluator.Evaluate(domainarb.EvaluationInput{
		Strategy:    strategy,
		BlockNumber: blockNumber,
		Route:       routeRef.Route,
		AmountIn:    optimized.AmountIn,
		AmountOut:   optimized.AmountOut,
		GasCost:     gas.CostWei,
	})
	if !evaluation.Accepted {
		return nil, nil
	}

	id := fmt.Sprintf("%s-%d-%d", routeRef.ID, blockNumber, s.now().UnixNano())
	opp := domainarb.NewOpportunity(id, strategy, blockNumber, routeRef.Route, evaluation, gas, s.now().UTC())
	opp.Status = domainarb.OpportunityStatusAccepted
	return opp, nil
}

func matchesStrategy(strategy domainarb.Strategy, routeRef domainarb.RouteRef) bool {
	return domainarb.MatchesStrategy(strategy, routeRef.Route)
}

func (s *OpportunityService) ensureRouteReady(routeRef domainarb.RouteRef) error {
	if s.readiness == nil {
		return nil
	}
	for _, hop := range routeRef.Route.Hops {
		switch hop.Version {
		case quoteunified.PoolVersionV3:
			if !s.readiness.IsV3PoolReady(hop.PoolV3) {
				return fmt.Errorf("univ3 pool %s is not ready", hop.PoolV3.Hex())
			}
		case quoteunified.PoolVersionPancakeV3:
			if !s.readiness.IsPancakeV3PoolReady(hop.PoolPancakeV3) {
				return fmt.Errorf("pancakev3 pool %s is not ready", hop.PoolPancakeV3.Hex())
			}
		case quoteunified.PoolVersionV4:
			if !s.readiness.IsV4PoolReady(hop.PoolV4) {
				return fmt.Errorf("v4 pool %s is not ready", hop.PoolV4.String())
			}
		case quoteunified.PoolVersionWrapWETH, quoteunified.PoolVersionUnwrapWETH:
			continue
		default:
			return fmt.Errorf("unsupported pool version %d", hop.Version)
		}
	}
	return nil
}

func (s *OpportunityService) loadRoutePools(ctx context.Context, route quoteunified.Route) (quoteunified.RoutePools, error) {
	pools := quoteunified.RoutePools{
		V3:        make(map[common.Address]*marketuniv3.Pool),
		PancakeV3: make(map[common.Address]*marketpancake.Pool),
		V4:        make(map[marketuniv4.PoolID]*marketuniv4.Pool),
	}

	for _, hop := range route.Hops {
		switch hop.Version {
		case quoteunified.PoolVersionV3:
			if _, ok := pools.V3[hop.PoolV3]; ok {
				continue
			}
			if s.univ3Pools == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("v3 pool repository is nil")
			}
			pool, err := s.univ3Pools.Get(ctx, hop.PoolV3)
			if err != nil {
				return quoteunified.RoutePools{}, fmt.Errorf("load v3 pool %s: %w", hop.PoolV3.Hex(), err)
			}
			if pool == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("v3 pool %s not found", hop.PoolV3.Hex())
			}
			pools.V3[hop.PoolV3] = pool
		case quoteunified.PoolVersionPancakeV3:
			if _, ok := pools.PancakeV3[hop.PoolPancakeV3]; ok {
				continue
			}
			if s.pancakePools == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("pancakev3 pool repository is nil")
			}
			pool, err := s.pancakePools.Get(ctx, hop.PoolPancakeV3)
			if err != nil {
				return quoteunified.RoutePools{}, fmt.Errorf("load pancakev3 pool %s: %w", hop.PoolPancakeV3.Hex(), err)
			}
			if pool == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("pancakev3 pool %s not found", hop.PoolPancakeV3.Hex())
			}
			pools.PancakeV3[hop.PoolPancakeV3] = pool
		case quoteunified.PoolVersionV4:
			if _, ok := pools.V4[hop.PoolV4]; ok {
				continue
			}
			if s.univ4Pools == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("v4 pool repository is nil")
			}
			pool, err := s.univ4Pools.Get(ctx, hop.PoolV4)
			if err != nil {
				return quoteunified.RoutePools{}, fmt.Errorf("load v4 pool %s: %w", hop.PoolV4.String(), err)
			}
			if pool == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("v4 pool %s not found", hop.PoolV4.String())
			}
			pools.V4[hop.PoolV4] = pool
		case quoteunified.PoolVersionWrapWETH, quoteunified.PoolVersionUnwrapWETH:
			continue
		default:
			return quoteunified.RoutePools{}, fmt.Errorf("unsupported pool version %d", hop.Version)
		}
	}

	return pools, nil
}

type routeQuoter struct {
	quotes *quoteunified.QuoteService
	pools  quoteunified.RoutePools
	route  quoteunified.Route
}

func (q routeQuoter) QuoteAmountOut(amountIn *big.Int) (*big.Int, error) {
	result, err := q.quotes.QuoteRoute(q.pools, q.route, amountIn)
	if err != nil {
		return nil, err
	}
	return result.AmountOut, nil
}
