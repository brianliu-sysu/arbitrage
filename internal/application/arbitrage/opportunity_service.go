package arbitrageapp

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// ReadinessChecker gates scanning on pool and system readiness across protocols.
type ReadinessChecker interface {
	IsSystemReady() bool
	IsV3PoolReady(poolAddress common.Address) bool
	IsPancakeV3PoolReady(poolAddress common.Address) bool
	IsQuickSwapV3PoolReady(poolAddress common.Address) bool
	IsV4PoolReady(poolID marketuniv4.PoolID) bool
	IsBalancerPoolReady(poolID marketbalancer.PoolID) bool
}

// OpportunityService generates opportunities from affected routes.
type OpportunityService struct {
	univ3Pools            marketuniv3.PoolRepository
	pancakePools          marketpancake.PoolRepository
	quickSwapPools        marketquick.PoolRepository
	univ4Pools            marketuniv4.PoolRepository
	balancerPools         marketbalancer.PoolRepository
	quotes                *quoteunified.QuoteService
	evaluator             *domainarb.Evaluator
	optimizer             *domainarb.Optimizer
	gas                   domainarb.GasEstimator
	flashLoans            []domainarb.FlashLoanOption
	mu                    sync.RWMutex
	strategies            []domainarb.Strategy
	readiness             ReadinessChecker
	logger                *zap.Logger
	now                   func() time.Time
	marketVersion         MarketVersionReader
	poolGraph             quoteunified.PoolGraph
	wrappedNative         common.Address
	coinbasePaymentBPS    uint16
	settlementSlippageBPS uint16
}

func (s *OpportunityService) SetCoinbasePaymentBPS(paymentBPS uint16) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.coinbasePaymentBPS = paymentBPS
	s.mu.Unlock()
}

func (s *OpportunityService) SetSettlementSlippageBPS(slippageBPS uint16) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.settlementSlippageBPS = slippageBPS
	s.mu.Unlock()
}

// SetGasCostConversion configures the routing graph used to value native gas in each strategy's profit token.
func (s *OpportunityService) SetGasCostConversion(graph quoteunified.PoolGraph, wrappedNative common.Address) {
	if s == nil {
		return
	}
	if wrappedNative == (common.Address{}) {
		wrappedNative = asset.MainnetWETH
	}
	s.mu.Lock()
	s.poolGraph = graph
	s.wrappedNative = wrappedNative
	s.mu.Unlock()
}

type MarketVersionReader interface {
	Version() domainchain.MarketVersion
}

// GenerateRequest is the input for opportunity generation.
type GenerateRequest struct {
	BlockNumber uint64
	Version     domainchain.MarketVersion
	Routes      []domainarb.RouteRef
}

func NewOpportunityService(
	univ3Pools marketuniv3.PoolRepository,
	pancakePools marketpancake.PoolRepository,
	quickSwapPools marketquick.PoolRepository,
	univ4Pools marketuniv4.PoolRepository,
	balancerPools marketbalancer.PoolRepository,
	quotes *quoteunified.QuoteService,
	gas domainarb.GasEstimator,
	strategies []domainarb.Strategy,
	readiness ReadinessChecker,
	minAmount, maxAmount *big.Int,
	optimizerIterations int,
	flashLoans []domainarb.FlashLoanOption,
	logger *zap.Logger,
	marketVersion MarketVersionReader,
) *OpportunityService {
	if logger == nil {
		logger = zap.NewNop()
	}
	if len(flashLoans) == 0 {
		flashLoans = domainarb.DefaultFlashLoanOptions()
	}
	return &OpportunityService{
		univ3Pools:     univ3Pools,
		pancakePools:   pancakePools,
		quickSwapPools: quickSwapPools,
		univ4Pools:     univ4Pools,
		balancerPools:  balancerPools,
		quotes:         quotes,
		evaluator:      domainarb.NewEvaluator(),
		optimizer:      domainarb.NewOptimizer(minAmount, maxAmount, optimizerIterations),
		gas:            gas,
		flashLoans:     append([]domainarb.FlashLoanOption(nil), flashLoans...),
		strategies:     append([]domainarb.Strategy(nil), strategies...),
		readiness:      readiness,
		logger:         logger,
		now:            time.Now,
		marketVersion:  marketVersion,
	}
}

// SetStrategies replaces the active arbitrage strategies.
func (s *OpportunityService) SetStrategies(strategies []domainarb.Strategy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.strategies = append([]domainarb.Strategy(nil), strategies...)
}

// Generate evaluates affected routes and returns accepted opportunities.
func (s *OpportunityService) Generate(ctx context.Context, req GenerateRequest) ([]*domainarb.Opportunity, error) {
	started := time.Now()
	if s.marketVersion != nil && !req.Version.IsZero() {
		current := s.marketVersion.Version()
		if current.Generation != req.Version.Generation || !current.SameBlock(req.Version) {
			return nil, fmt.Errorf("committed market version changed: got %+v, want %+v", current, req.Version)
		}
	}
	strategies := s.strategiesSnapshot()
	if s.readiness != nil && !s.readiness.IsSystemReady() {
		s.logger.Debug("arbitrage scan skipped",
			zap.Uint64("block", req.BlockNumber),
			zap.String("reason", "system_not_ready"),
			zap.Int("routes", len(req.Routes)),
			zap.Int("strategies", len(strategies)),
			zap.Int64("duration_ms", time.Since(started).Milliseconds()),
		)
		return nil, nil
	}
	if len(req.Routes) == 0 {
		s.logger.Debug("arbitrage scan skipped",
			zap.Uint64("block", req.BlockNumber),
			zap.String("reason", "no_affected_routes"),
			zap.Int("strategies", len(strategies)),
			zap.Int64("duration_ms", time.Since(started).Milliseconds()),
		)
		return nil, nil
	}
	if len(strategies) == 0 {
		s.logger.Debug("arbitrage scan skipped",
			zap.Uint64("block", req.BlockNumber),
			zap.String("reason", "no_strategies"),
			zap.Int("routes", len(req.Routes)),
			zap.Int64("duration_ms", time.Since(started).Milliseconds()),
		)
		return nil, nil
	}

	strategiesByStart := indexStrategiesByStartToken(strategies)
	opportunities := make([]*domainarb.Opportunity, 0)
	routeNotReady := 0
	strategyMismatches := 0
	quoteErrors := 0
	nonProfitable := 0
	for _, routeRef := range req.Routes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := s.ensureRouteReady(routeRef); err != nil {
			routeNotReady++
			s.logger.Debug("arbitrage route skipped",
				zap.Uint64("block", req.BlockNumber),
				zap.String("route", routeRef.ID),
				zap.String("reason", "route_not_ready"),
				zap.Error(err),
			)
			continue
		}

		candidates := strategiesByStart[routeRef.Route.TokenIn]
		for _, strategy := range candidates {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !matchesStrategy(strategy, routeRef) {
				strategyMismatches++
				continue
			}

			opp, err := s.generateForRoute(ctx, req.BlockNumber, strategy, routeRef)
			if err != nil {
				quoteErrors++
				s.logger.Debug("arbitrage route skipped",
					zap.Uint64("block", req.BlockNumber),
					zap.String("route", routeRef.ID),
					zap.String("strategy", strategy.ID),
					zap.String("reason", "generate_failed"),
					zap.Error(err),
				)
				continue
			}
			if opp != nil {
				opportunities = append(opportunities, opp)
			} else {
				nonProfitable++
			}
		}
	}

	s.logger.Debug("arbitrage scan completed",
		zap.Uint64("block", req.BlockNumber),
		zap.Int("routes", len(req.Routes)),
		zap.Int("strategies", len(strategies)),
		zap.Int("route_not_ready", routeNotReady),
		zap.Int("strategy_mismatches", strategyMismatches),
		zap.Int("generate_errors", quoteErrors),
		zap.Int("rejected", nonProfitable),
		zap.Int("opportunities", len(opportunities)),
		zap.Int64("duration_ms", time.Since(started).Milliseconds()),
	)
	return opportunities, nil
}

func indexStrategiesByStartToken(strategies []domainarb.Strategy) map[common.Address][]domainarb.Strategy {
	out := make(map[common.Address][]domainarb.Strategy, len(strategies))
	for _, strategy := range strategies {
		out[strategy.StartToken] = append(out[strategy.StartToken], strategy)
	}
	return out
}

func (s *OpportunityService) strategiesSnapshot() []domainarb.Strategy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]domainarb.Strategy(nil), s.strategies...)
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
	promising, err := s.optimizer.ProbePositiveGrossProfit(ctx, quoter)
	if err != nil {
		return nil, err
	}
	if !promising {
		return nil, nil
	}
	optimized, err := s.optimizer.OptimizeContext(ctx, quoter)
	if err != nil {
		return nil, err
	}
	quoteSteps, quoteStepsErr := s.quotes.QuoteRouteSteps(pools, routeRef.Route, optimized.AmountIn)
	if optimized.AmountIn.Sign() <= 0 {
		return nil, nil
	}

	flashLoanOptions := domainarb.FlashLoanOptionsForRoute(routeRef.Route, pools, s.flashLoans)
	flashLoan, err := domainarb.SelectBestFlashLoan(optimized.AmountIn, flashLoanOptions)
	if err != nil {
		return nil, err
	}

	gas, err := s.gas.Estimate(ctx, routeRef.Route.Len())
	if err != nil {
		return nil, err
	}

	gasCost, err := s.gasCostInToken(ctx, gas.CostWei, strategy.StartToken)
	if err != nil {
		return nil, fmt.Errorf("convert gas cost to profit token %s: %w", strategy.StartToken.Hex(), err)
	}

	s.mu.RLock()
	coinbasePaymentBPS := s.coinbasePaymentBPS
	settlementSlippageBPS := s.settlementSlippageBPS
	wrappedNative := s.wrappedNative
	s.mu.RUnlock()
	if strategy.StartToken == (common.Address{}) || strategy.StartToken == wrappedNative {
		settlementSlippageBPS = 0
	}
	evaluation := s.evaluator.Evaluate(domainarb.EvaluationInput{
		Strategy:              strategy,
		BlockNumber:           blockNumber,
		Route:                 routeRef.Route,
		AmountIn:              optimized.AmountIn,
		AmountOut:             optimized.AmountOut,
		GasCost:               gasCost,
		CoinbasePaymentBPS:    coinbasePaymentBPS,
		SettlementSlippageBPS: settlementSlippageBPS,
		FlashLoan:             flashLoan,
		QuoteSteps:            opportunityQuoteSteps(quoteSteps),
	})
	if !evaluation.Accepted {
		s.logger.Debug("arbitrage route rejected",
			zap.Uint64("block", blockNumber),
			zap.String("route", routeRef.ID),
			zap.String("strategy", strategy.ID),
			zap.String("reason", "profit_filter"),
			zap.String("amount_in", evaluation.AmountIn.String()),
			zap.String("amount_out", evaluation.AmountOut.String()),
			zap.String("gross_profit", evaluation.GrossProfit.String()),
			zap.String("gas_cost_wei", gas.CostWei.String()),
			zap.String("gas_cost_profit_token", gasCost.String()),
			zap.String("flash_loan_protocol", string(flashLoan.Protocol)),
			zap.String("flash_loan_pool", flashLoan.PoolRef.Key()),
			zap.String("flash_loan_fee", flashLoan.Fee.String()),
			zap.String("coinbase_payment", evaluation.CoinbasePayment.String()),
			zap.String("net_profit", evaluation.NetProfit.String()),
			zap.Bool("profitable", evaluation.Profitable),
			zap.String("min_net_profit", bigIntString(strategy.MinNetProfitWei)),
			zap.String("quote_steps", formatQuoteSteps(quoteSteps, quoteStepsErr)),
		)
		return nil, nil
	}

	id := fmt.Sprintf("%s-%d-%d", routeRef.ID, blockNumber, s.now().UnixNano())
	opp := domainarb.NewOpportunity(id, strategy, blockNumber, routeRef.Route, evaluation, gas, s.now().UTC())
	if err := opp.SetStatus(domainarb.OpportunityStatusAccepted); err != nil {
		return nil, fmt.Errorf("set opportunity status: %w", err)
	}
	s.logger.Debug("arbitrage route accepted",
		zap.Uint64("block", blockNumber),
		zap.String("route", routeRef.ID),
		zap.String("strategy", strategy.ID),
		zap.String("flash_loan_protocol", string(flashLoan.Protocol)),
		zap.String("flash_loan_pool", flashLoan.PoolRef.Key()),
		zap.String("flash_loan_fee", flashLoan.Fee.String()),
		zap.String("coinbase_payment", evaluation.CoinbasePayment.String()),
		zap.String("amount_in", evaluation.AmountIn.String()),
		zap.String("amount_out", evaluation.AmountOut.String()),
		zap.String("net_profit", evaluation.NetProfit.String()),
		zap.String("quote_steps", formatQuoteSteps(quoteSteps, quoteStepsErr)),
	)
	return opp, nil
}

func (s *OpportunityService) gasCostInToken(ctx context.Context, costWei *big.Int, token common.Address) (*big.Int, error) {
	if costWei == nil || costWei.Sign() <= 0 {
		return new(big.Int), nil
	}
	s.mu.RLock()
	graph := s.poolGraph
	wrappedNative := s.wrappedNative
	s.mu.RUnlock()
	if wrappedNative == (common.Address{}) {
		wrappedNative = asset.MainnetWETH
	}
	if token == (common.Address{}) || token == wrappedNative {
		return new(big.Int).Set(costWei), nil
	}
	if graph == nil {
		return nil, errors.New("pool graph is not configured")
	}
	routes, err := quoteunified.NewRouteService(graph, 3).FindRoutes(wrappedNative, token)
	if err != nil {
		return nil, err
	}
	var bestAmountOut *big.Int
	for _, route := range routes {
		pools, loadErr := s.loadRoutePools(ctx, route)
		if loadErr != nil {
			continue
		}
		quote, quoteErr := s.quotes.QuoteRoute(pools, route, costWei)
		if quoteErr != nil || quote.AmountOut == nil || quote.AmountOut.Sign() <= 0 {
			continue
		}
		if bestAmountOut == nil || quote.AmountOut.Cmp(bestAmountOut) > 0 {
			bestAmountOut = new(big.Int).Set(quote.AmountOut)
		}
	}
	if bestAmountOut == nil {
		return nil, errors.New("no quotable route from wrapped native token")
	}
	return bestAmountOut, nil
}

func matchesStrategy(strategy domainarb.Strategy, routeRef domainarb.RouteRef) bool {
	return domainarb.MatchesStrategy(strategy, routeRef.Route)
}

func bigIntString(value *big.Int) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func formatQuoteSteps(steps []quoteunified.RouteQuoteStep, err error) string {
	if err != nil {
		return "error=" + err.Error()
	}
	if len(steps) == 0 {
		return ""
	}
	parts := make([]string, 0, len(steps))
	for _, step := range steps {
		parts = append(parts, fmt.Sprintf(
			"hop=%d version=%s tokenIn=%s tokenOut=%s amountIn=%s amountOut=%s fee=%s",
			step.Index,
			step.Hop.Version.String(),
			step.Hop.TokenIn.Hex(),
			step.Hop.TokenOut.Hex(),
			bigIntString(step.AmountIn),
			bigIntString(step.AmountOut),
			bigIntString(step.FeeAmount),
		))
	}
	return strings.Join(parts, " | ")
}

func opportunityQuoteSteps(steps []quoteunified.RouteQuoteStep) []domainarb.OpportunityQuoteStep {
	if len(steps) == 0 {
		return nil
	}
	out := make([]domainarb.OpportunityQuoteStep, 0, len(steps))
	for _, step := range steps {
		out = append(out, domainarb.OpportunityQuoteStep{
			Index:     step.Index,
			Version:   step.Hop.Version.String(),
			TokenIn:   step.Hop.TokenIn,
			TokenOut:  step.Hop.TokenOut,
			AmountIn:  cloneBigIntOrZero(step.AmountIn),
			AmountOut: cloneBigIntOrZero(step.AmountOut),
			FeeAmount: cloneBigIntOrZero(step.FeeAmount),
		})
	}
	return out
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
		case quoteunified.PoolVersionQuickSwapV3:
			if !s.readiness.IsQuickSwapV3PoolReady(hop.PoolQuickSwapV3) {
				return fmt.Errorf("quickswapv3 pool %s is not ready", hop.PoolQuickSwapV3.Hex())
			}
		case quoteunified.PoolVersionV4:
			if !s.readiness.IsV4PoolReady(hop.PoolV4) {
				return fmt.Errorf("v4 pool %s is not ready", hop.PoolV4.String())
			}
		case quoteunified.PoolVersionBalancer:
			if !s.readiness.IsBalancerPoolReady(hop.PoolBalancer) {
				return fmt.Errorf("balancer pool %s is not ready", hop.PoolBalancer.String())
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
		V3:          make(map[common.Address]*marketuniv3.Pool),
		PancakeV3:   make(map[common.Address]*marketpancake.Pool),
		QuickSwapV3: make(map[common.Address]*marketquick.Pool),
		V4:          make(map[marketuniv4.PoolID]*marketuniv4.Pool),
		Balancer:    make(map[marketbalancer.PoolID]*marketbalancer.Pool),
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
		case quoteunified.PoolVersionQuickSwapV3:
			if _, ok := pools.QuickSwapV3[hop.PoolQuickSwapV3]; ok {
				continue
			}
			if s.quickSwapPools == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("quickswapv3 pool repository is nil")
			}
			pool, err := s.quickSwapPools.Get(ctx, hop.PoolQuickSwapV3)
			if err != nil {
				return quoteunified.RoutePools{}, fmt.Errorf("load quickswapv3 pool %s: %w", hop.PoolQuickSwapV3.Hex(), err)
			}
			if pool == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("quickswapv3 pool %s not found", hop.PoolQuickSwapV3.Hex())
			}
			pools.QuickSwapV3[hop.PoolQuickSwapV3] = pool
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
		case quoteunified.PoolVersionBalancer:
			if _, ok := pools.Balancer[hop.PoolBalancer]; ok {
				continue
			}
			if s.balancerPools == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("balancer pool repository is nil")
			}
			pool, err := s.balancerPools.Get(ctx, hop.PoolBalancer)
			if err != nil {
				return quoteunified.RoutePools{}, fmt.Errorf("load balancer pool %s: %w", hop.PoolBalancer.String(), err)
			}
			if pool == nil {
				return quoteunified.RoutePools{}, fmt.Errorf("balancer pool %s not found", hop.PoolBalancer.String())
			}
			pools.Balancer[hop.PoolBalancer] = pool
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
		if isSoftQuoteFailure(err) {
			return big.NewInt(0), nil
		}
		return nil, err
	}
	return result.AmountOut, nil
}

func isSoftQuoteFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, quoteunified.ErrNonPositiveAmount) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "amountIn must be positive") ||
		strings.Contains(msg, "amount must be positive")
}
