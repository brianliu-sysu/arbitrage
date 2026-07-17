package arbitrageapp

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

// LivePlanConfig supplies chain addresses needed to rebuild an execution plan from an opportunity.
type LivePlanConfig struct {
	WETH                  common.Address
	BalancerVault         common.Address
	BalancerVaultV3       common.Address
	BalancerRouterV3      common.Address
	PoolManager           common.Address
	SwapRouterV3          common.Address
	SwapRouterPancakeV3   common.Address
	UniversalRouter       common.Address
	Executor              common.Address
	RequireWETHProfit     bool
	CoinbasePaymentBPS    uint64
	SettlementSlippageBPS uint64
}

// LiveExecutionPlanBuilder rebuilds an execution plan from the opportunity.
// Prefer an embedded payload.execution when present (refreshing amounts). Otherwise encode
// live swap calldata from the opportunity route when a LiveCalldataEncoder is configured.
type LiveExecutionPlanBuilder struct {
	payload *PayloadExecutionPlanBuilder
	encoder *LiveCalldataEncoder
	cfg     LivePlanConfig
	graph   quoteunified.PoolGraph
	quotes  *quoteunified.QuoteService
	graphMu sync.RWMutex
}

// SetPoolGraph atomically replaces the graph used for WETH settlement route discovery.
func (b *LiveExecutionPlanBuilder) SetPoolGraph(graph quoteunified.PoolGraph) {
	if b == nil {
		return
	}
	b.graphMu.Lock()
	b.graph = graph
	b.graphMu.Unlock()
}

func (b *LiveExecutionPlanBuilder) poolGraph() quoteunified.PoolGraph {
	if b == nil {
		return nil
	}
	b.graphMu.RLock()
	defer b.graphMu.RUnlock()
	return b.graph
}

func NewLiveExecutionPlanBuilder(
	cfg LivePlanConfig,
	encoder *LiveCalldataEncoder,
	graphs ...quoteunified.PoolGraph,
) *LiveExecutionPlanBuilder {
	if cfg.WETH == (common.Address{}) {
		cfg.WETH = asset.MainnetWETH
	}
	builder := &LiveExecutionPlanBuilder{
		payload: NewPayloadExecutionPlanBuilder(),
		encoder: encoder,
		cfg:     cfg,
		quotes:  quoteunified.NewQuoteService(nil, nil, nil),
	}
	if len(graphs) > 0 {
		builder.graph = graphs[0]
	}
	return builder
}

func (b *LiveExecutionPlanBuilder) BuildExecutionPlan(
	ctx context.Context,
	opportunity *domainarb.Opportunity,
) (domaincontract.ExecutionPlan, []domaincontract.TokenApproval, error) {
	if opportunity == nil {
		return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("opportunity is nil")
	}
	if err := opportunity.ApplyPayload(); err != nil {
		return domaincontract.ExecutionPlan{}, nil, err
	}

	if b.payload != nil {
		plan, approvals, err := b.payload.BuildExecutionPlan(ctx, opportunity)
		if err == nil {
			refreshed, err := refreshPlanAmounts(plan, opportunity)
			if err != nil {
				return domaincontract.ExecutionPlan{}, nil, err
			}
			return b.addWETHSettlement(ctx, opportunity, refreshed, approvals)
		}
		if !isExecutionUnavailable(err) {
			return domaincontract.ExecutionPlan{}, nil, err
		}
	}

	loan, err := b.buildFlashLoanFromOpportunity(opportunity)
	if err != nil {
		return domaincontract.ExecutionPlan{}, nil, err
	}
	if b.encoder == nil {
		return domaincontract.ExecutionPlan{}, nil, fmt.Errorf(
			"%w: opportunity %q has no embedded execution plan; live swap calldata encoder is not configured",
			ErrExecutionPlanUnavailable,
			opportunity.ID,
		)
	}
	plan, approvals, err := b.encoder.Encode(ctx, opportunity, loan)
	if err != nil {
		return domaincontract.ExecutionPlan{}, nil, err
	}
	return b.addWETHSettlement(ctx, opportunity, plan, approvals)
}

func (b *LiveExecutionPlanBuilder) addWETHSettlement(
	ctx context.Context,
	opportunity *domainarb.Opportunity,
	plan domaincontract.ExecutionPlan,
	approvals []domaincontract.TokenApproval,
) (domaincontract.ExecutionPlan, []domaincontract.TokenApproval, error) {
	if plan.ProfitToken == (common.Address{}) || plan.ProfitToken == b.cfg.WETH {
		return plan, domaincontract.MergeTokenApprovals(approvals, domaincontract.RequiredTokenApprovals(plan)), nil
	}
	if !b.cfg.RequireWETHProfit {
		return plan, domaincontract.MergeTokenApprovals(approvals, domaincontract.RequiredTokenApprovals(plan)), nil
	}
	graph := b.poolGraph()
	if graph == nil || b.encoder == nil || b.encoder.loader == nil || plan.MinProfit == nil || plan.MinProfit.Sign() <= 0 {
		return domaincontract.ExecutionPlan{}, nil, fmt.Errorf(
			"%w: no WETH settlement planner for profit token %s", ErrExecutionPlanUnavailable, plan.ProfitToken.Hex(),
		)
	}

	routes, err := quoteunified.NewRouteService(graph, 3).FindRoutes(plan.ProfitToken, b.cfg.WETH)
	if err != nil {
		return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("find WETH settlement routes: %w", err)
	}
	var bestRoutes []domaincontract.SwapRoute
	var bestApprovals []domaincontract.TokenApproval
	var bestAmountOut *big.Int
	for _, route := range routes {
		pools, err := b.encoder.loader.LoadRoutePools(ctx, route)
		if err != nil {
			continue
		}
		settlementInput := expectedProfitAfterFlashLoan(opportunity)
		if settlementInput.Sign() <= 0 {
			settlementInput = plan.MinProfit
		}
		quote, err := b.quotes.QuoteRoute(pools, route, settlementInput)
		if err != nil || quote.AmountOut == nil || quote.AmountOut.Sign() <= 0 {
			continue
		}
		synthetic := &domainarb.Opportunity{Route: route, NetProfit: quote.AmountOut}
		encoded, settlementApprovals, err := b.encoder.Encode(ctx, synthetic, domaincontract.FlashLoan{})
		if err != nil {
			continue
		}
		if bestAmountOut == nil || quote.AmountOut.Cmp(bestAmountOut) > 0 {
			bestAmountOut = new(big.Int).Set(quote.AmountOut)
			bestRoutes = encoded.Routes
			bestApprovals = settlementApprovals
		}
	}
	if bestAmountOut == nil {
		return domaincontract.ExecutionPlan{}, nil, fmt.Errorf(
			"%w: no quotable WETH settlement route for profit token %s", ErrExecutionPlanUnavailable, plan.ProfitToken.Hex(),
		)
	}
	plan.SettlementRoutes = bestRoutes
	plan.SettlementMinProfit = applyBPSReduction(bestAmountOut, b.cfg.SettlementSlippageBPS)
	plan.SettlementMinProfit = applyBPSReduction(plan.SettlementMinProfit, b.cfg.CoinbasePaymentBPS)
	approvals = domaincontract.MergeTokenApprovals(approvals, bestApprovals)
	approvals = domaincontract.MergeTokenApprovals(approvals, domaincontract.RequiredTokenApprovals(plan))
	return plan, approvals, nil
}

func expectedProfitAfterFlashLoan(opportunity *domainarb.Opportunity) *big.Int {
	if opportunity == nil {
		return new(big.Int)
	}
	profit := new(big.Int).Sub(cloneBigIntOrZero(opportunity.AmountOut), cloneBigIntOrZero(opportunity.AmountIn))
	profit.Sub(profit, cloneBigIntOrZero(opportunity.FlashLoan.Fee))
	return profit
}

func applyBPSReduction(amount *big.Int, bps uint64) *big.Int {
	if amount == nil || amount.Sign() <= 0 {
		return new(big.Int)
	}
	if bps > 10_000 {
		bps = 10_000
	}
	result := new(big.Int).Mul(amount, new(big.Int).SetUint64(10_000-bps))
	return result.Div(result, big.NewInt(10_000))
}

func (b *LiveExecutionPlanBuilder) buildFlashLoanFromOpportunity(
	opportunity *domainarb.Opportunity,
) (domaincontract.FlashLoan, error) {
	amount := opportunity.FlashLoan.Amount
	if amount == nil || amount.Sign() <= 0 {
		amount = opportunity.AmountIn
	}
	if amount == nil || amount.Sign() <= 0 {
		return domaincontract.FlashLoan{}, fmt.Errorf("opportunity flash loan amount is required")
	}
	token := opportunity.Route.TokenIn
	if token == (common.Address{}) {
		return domaincontract.FlashLoan{}, fmt.Errorf("opportunity route.tokenIn is required for flash loan token")
	}

	switch opportunity.FlashLoan.Protocol {
	case domainarb.FlashLoanProtocolBalancer:
		if b.cfg.BalancerVault == (common.Address{}) {
			return domaincontract.FlashLoan{}, fmt.Errorf("balancer vault address is not configured")
		}
		return domaincontract.FlashLoan{
			Protocol: domaincontract.FlashLoanProtocolBalancer,
			Lender:   b.cfg.BalancerVault,
			Token:    token,
			Amount:   new(big.Int).Set(amount),
		}, nil
	case domainarb.FlashLoanProtocolUniv3:
		lender := opportunity.FlashLoan.PoolRef.V3
		if lender == (common.Address{}) {
			lender = opportunity.PoolAddress
		}
		if lender == (common.Address{}) {
			return domaincontract.FlashLoan{}, fmt.Errorf("univ3 flash loan pool address is required")
		}
		return domaincontract.FlashLoan{
			Protocol:     domaincontract.FlashLoanProtocolUniswapV3,
			Lender:       lender,
			Token:        token,
			Amount:       new(big.Int).Set(amount),
			BorrowToken0: opportunity.FlashLoan.BorrowToken0,
		}, nil
	case domainarb.FlashLoanProtocolUniv4:
		if b.cfg.PoolManager == (common.Address{}) {
			return domaincontract.FlashLoan{}, fmt.Errorf("univ4 pool manager address is not configured")
		}
		return domaincontract.FlashLoan{
			Protocol: domaincontract.FlashLoanProtocolUniswapV4,
			Lender:   b.cfg.PoolManager,
			Token:    token,
			Amount:   new(big.Int).Set(amount),
		}, nil
	default:
		return domaincontract.FlashLoan{}, fmt.Errorf("unsupported flash loan protocol %q", opportunity.FlashLoan.Protocol)
	}
}

func refreshPlanAmounts(plan domaincontract.ExecutionPlan, opportunity *domainarb.Opportunity) (domaincontract.ExecutionPlan, error) {
	if opportunity == nil {
		return plan, nil
	}
	originalLoanAmount := cloneBigInt(plan.Loan.Amount)
	if opportunity.FlashLoan.Amount != nil && opportunity.FlashLoan.Amount.Sign() > 0 {
		plan.Loan.Amount = new(big.Int).Set(opportunity.FlashLoan.Amount)
	} else if opportunity.AmountIn != nil && opportunity.AmountIn.Sign() > 0 {
		plan.Loan.Amount = new(big.Int).Set(opportunity.AmountIn)
	}
	if originalLoanAmount != nil && plan.Loan.Amount != nil && originalLoanAmount.Cmp(plan.Loan.Amount) != 0 {
		for i, route := range plan.Routes {
			if route.FillSource == domaincontract.FillSourceNone {
				return domaincontract.ExecutionPlan{}, fmt.Errorf(
					"embedded execution route[%d] has fixed calldata amount; refusing to change loan amount from %s to %s",
					i,
					originalLoanAmount.String(),
					plan.Loan.Amount.String(),
				)
			}
		}
	}
	if opportunity.NetProfit != nil {
		plan.MinProfit = new(big.Int).Set(opportunity.NetProfit)
	}
	return plan, nil
}

func isExecutionUnavailable(err error) bool {
	return errors.Is(err, ErrExecutionPlanUnavailable)
}
