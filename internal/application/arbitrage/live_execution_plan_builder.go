package arbitrageapp

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	"github.com/ethereum/go-ethereum/common"
)

// LivePlanConfig supplies chain addresses needed to rebuild an execution plan from an opportunity.
type LivePlanConfig struct {
	WETH                common.Address
	BalancerVault       common.Address
	BalancerVaultV3     common.Address
	BalancerRouterV3    common.Address
	PoolManager         common.Address
	SwapRouterV3        common.Address
	SwapRouterPancakeV3 common.Address
	UniversalRouter     common.Address
	Executor            common.Address
}

// LiveExecutionPlanBuilder rebuilds an execution plan from the opportunity.
// Prefer an embedded payload.execution when present (refreshing amounts). Otherwise encode
// live swap calldata from the opportunity route when a LiveCalldataEncoder is configured.
type LiveExecutionPlanBuilder struct {
	payload *PayloadExecutionPlanBuilder
	encoder *LiveCalldataEncoder
	cfg     LivePlanConfig
}

func NewLiveExecutionPlanBuilder(cfg LivePlanConfig, encoder *LiveCalldataEncoder) *LiveExecutionPlanBuilder {
	if cfg.WETH == (common.Address{}) {
		cfg.WETH = asset.MainnetWETH
	}
	return &LiveExecutionPlanBuilder{
		payload: NewPayloadExecutionPlanBuilder(),
		encoder: encoder,
		cfg:     cfg,
	}
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
			approvals = domaincontract.MergeTokenApprovals(approvals, domaincontract.RequiredTokenApprovals(refreshed))
			return refreshed, approvals, nil
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
	return b.encoder.Encode(ctx, opportunity, loan)
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
