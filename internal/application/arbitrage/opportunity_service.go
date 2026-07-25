package arbitrageapp

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/application/committedmarket"
	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// OpportunityService generates opportunities from affected routes.
type OpportunityService struct {
	market             committedmarket.Reader
	quotes             *quoteunified.QuoteService
	evaluator          *domainarb.Evaluator
	optimizer          *domainarb.Optimizer
	gas                domainarb.GasEstimator
	flashLoans         []domainarb.FlashLoanOption
	mu                 sync.RWMutex
	strategies         []domainarb.Strategy
	logger             *zap.Logger
	now                func() time.Time
	poolGraph          quoteunified.PoolGraph
	wrappedNative      common.Address
	coinbasePaymentBPS uint16
}

func (s *OpportunityService) SetCoinbasePaymentBPS(paymentBPS uint16) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.coinbasePaymentBPS = paymentBPS
	s.mu.Unlock()
}

// SetGasCostConversion configures the routing graph used to value native gas in each strategy's profit token.
func (s *OpportunityService) SetGasCostConversion(graph quoteunified.PoolGraph, wrappedNative common.Address) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.poolGraph = graph
	s.wrappedNative = wrappedNative
	s.mu.Unlock()
}

func (s *OpportunityService) IsMarketReady() bool {
	if s == nil || s.market == nil {
		return false
	}
	snapshot := s.market.Snapshot()
	return snapshot != nil && !snapshot.Version().IsZero()
}

// GenerateRequest is the input for opportunity generation.
type GenerateRequest struct {
	Version domainchain.MarketVersion
	Routes  []domainarb.RouteRef
}

func NewOpportunityService(
	market committedmarket.Reader,
	quotes *quoteunified.QuoteService,
	gas domainarb.GasEstimator,
	strategies []domainarb.Strategy,
	minAmount, maxAmount *big.Int,
	optimizerIterations int,
	flashLoans []domainarb.FlashLoanOption,
	logger *zap.Logger,
) *OpportunityService {
	if logger == nil {
		logger = zap.NewNop()
	}
	if len(flashLoans) == 0 {
		flashLoans = domainarb.DefaultFlashLoanOptions()
	}
	return &OpportunityService{
		market:     market,
		quotes:     quotes,
		evaluator:  domainarb.NewEvaluator(),
		optimizer:  domainarb.NewOptimizer(minAmount, maxAmount, optimizerIterations),
		gas:        gas,
		flashLoans: append([]domainarb.FlashLoanOption(nil), flashLoans...),
		strategies: append([]domainarb.Strategy(nil), strategies...),
		logger:     logger,
		now:        time.Now,
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
	if s.market == nil {
		return nil, fmt.Errorf("committed market reader is not configured")
	}
	snapshot := s.market.Snapshot()
	if snapshot == nil {
		return nil, fmt.Errorf("committed market snapshot is not available")
	}
	current := snapshot.Version()
	if current.IsZero() {
		return nil, nil
	}
	if current.Generation != req.Version.Generation || !current.SameBlock(req.Version) {
		return nil, fmt.Errorf("committed market version changed: got %+v, want %+v", current, req.Version)
	}
	blockNumber := current.Number
	strategies := s.strategiesSnapshot()
	if len(req.Routes) == 0 {
		s.logger.Debug("arbitrage scan skipped",
			zap.Uint64("block", blockNumber),
			zap.String("reason", "no_affected_routes"),
			zap.Int("strategies", len(strategies)),
			zap.Int64("duration_ms", time.Since(started).Milliseconds()),
		)
		return nil, nil
	}
	if len(strategies) == 0 {
		s.logger.Debug("arbitrage scan skipped",
			zap.Uint64("block", blockNumber),
			zap.String("reason", "no_strategies"),
			zap.Int("routes", len(req.Routes)),
			zap.Int64("duration_ms", time.Since(started).Milliseconds()),
		)
		return nil, nil
	}

	strategiesByStart := indexStrategiesByStartToken(strategies)
	opportunities := make([]*domainarb.Opportunity, 0)
	strategyMismatches := 0
	quoteErrors := 0
	nonProfitable := 0
	for _, routeRef := range req.Routes {
		if err := ctx.Err(); err != nil {
			return nil, err
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

			opp, err := s.generateForRoute(ctx, snapshot, blockNumber, strategy, routeRef)
			if err != nil {
				quoteErrors++
				s.logger.Debug("arbitrage route skipped",
					zap.Uint64("block", blockNumber),
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
		zap.Uint64("block", blockNumber),
		zap.Int("routes", len(req.Routes)),
		zap.Int("strategies", len(strategies)),
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
	snapshot committedmarket.Snapshot,
	blockNumber uint64,
	strategy domainarb.Strategy,
	routeRef domainarb.RouteRef,
) (*domainarb.Opportunity, error) {
	pools, err := snapshot.LoadRoutePools(ctx, routeRef.Route)
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

	gasCost, err := s.gasCostInToken(ctx, snapshot, gas.CostWei, strategy.StartToken)
	if err != nil {
		return nil, fmt.Errorf("convert gas cost to profit token %s: %w", strategy.StartToken.Hex(), err)
	}

	s.mu.RLock()
	coinbasePaymentBPS := s.coinbasePaymentBPS
	s.mu.RUnlock()
	evaluation := s.evaluator.Evaluate(domainarb.EvaluationInput{
		Strategy:           strategy,
		BlockNumber:        blockNumber,
		Route:              routeRef.Route,
		AmountIn:           optimized.AmountIn,
		AmountOut:          optimized.AmountOut,
		GasCost:            gasCost,
		CoinbasePaymentBPS: coinbasePaymentBPS,
		FlashLoan:          flashLoan,
		QuoteSteps:         opportunityQuoteSteps(quoteSteps),
	})
	if evaluation.CoinbasePayment.Sign() > 0 {
		builderPaymentWei, conversionErr := s.tokenAmountInWrappedNative(ctx, snapshot, evaluation.CoinbasePayment, strategy.StartToken)
		if conversionErr != nil {
			return nil, fmt.Errorf("convert builder payment to native token: %w", conversionErr)
		}
		evaluation.BuilderPaymentWei = builderPaymentWei
	}
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

func (s *OpportunityService) tokenAmountInWrappedNative(
	ctx context.Context,
	snapshot committedmarket.Snapshot,
	amount *big.Int,
	token common.Address,
) (*big.Int, error) {
	if amount == nil || amount.Sign() <= 0 {
		return new(big.Int), nil
	}
	s.mu.RLock()
	graph := s.poolGraph
	wrappedNative := s.wrappedNative
	s.mu.RUnlock()
	if wrappedNative == (common.Address{}) {
		return nil, errors.New("wrapped native token is not configured")
	}
	if token == (common.Address{}) || token == wrappedNative {
		return new(big.Int).Set(amount), nil
	}
	if graph == nil {
		return nil, errors.New("pool graph is not configured")
	}
	routes, err := quoteunified.NewRouteService(graph, 3).FindRoutes(token, wrappedNative)
	if err != nil {
		return nil, err
	}
	var bestAmountOut *big.Int
	for _, route := range routes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pools, loadErr := snapshot.LoadRoutePools(ctx, route)
		if loadErr != nil {
			continue
		}
		quote, quoteErr := s.quotes.QuoteRoute(pools, route, amount)
		if quoteErr == nil && quote.AmountOut != nil && quote.AmountOut.Sign() > 0 {
			if bestAmountOut == nil || quote.AmountOut.Cmp(bestAmountOut) > 0 {
				bestAmountOut = new(big.Int).Set(quote.AmountOut)
			}
		}
	}
	if bestAmountOut == nil {
		return nil, errors.New("no quotable route to wrapped native token")
	}
	return bestAmountOut, nil
}

func (s *OpportunityService) gasCostInToken(
	ctx context.Context,
	snapshot committedmarket.Snapshot,
	costWei *big.Int,
	token common.Address,
) (*big.Int, error) {
	if costWei == nil || costWei.Sign() <= 0 {
		return new(big.Int), nil
	}
	s.mu.RLock()
	graph := s.poolGraph
	wrappedNative := s.wrappedNative
	s.mu.RUnlock()
	if wrappedNative == (common.Address{}) {
		return nil, errors.New("wrapped native token is not configured")
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
		pools, loadErr := snapshot.LoadRoutePools(ctx, route)
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

func cloneBigIntOrZero(value *big.Int) *big.Int {
	if value == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(value)
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
