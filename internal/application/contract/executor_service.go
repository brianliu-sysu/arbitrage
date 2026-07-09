package contract

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	"github.com/ethereum/go-ethereum/common"
)

type Broadcaster interface {
	BroadcastExecution(ctx context.Context, req domaincontract.BroadcastRequest) (domaincontract.BroadcastResponse, error)
}

type AppService struct {
	broadcaster Broadcaster
}

func NewAppService(broadcaster Broadcaster) *AppService {
	return &AppService{broadcaster: broadcaster}
}

func (s *AppService) Execute(ctx context.Context, req domaincontract.BroadcastRequest) (domaincontract.BroadcastResponse, error) {
	if s == nil || s.broadcaster == nil {
		return domaincontract.BroadcastResponse{}, errors.New("contract executor broadcaster is not configured")
	}
	if err := ValidateBroadcastRequest(req); err != nil {
		return domaincontract.BroadcastResponse{}, err
	}
	return s.broadcaster.BroadcastExecution(ctx, normalizeBroadcastRequest(req))
}

func ValidateBroadcastRequest(req domaincontract.BroadcastRequest) error {
	if strings.TrimSpace(req.RPCURL) == "" {
		return errors.New("rpcUrl is required")
	}
	if strings.TrimSpace(req.PrivateKey) == "" {
		return errors.New("privateKey is required")
	}
	if req.Executor == (common.Address{}) {
		return errors.New("executor is required")
	}
	if err := validatePlan(req.Plan); err != nil {
		return err
	}
	if req.GasPriceWei != nil && req.GasPriceWei.Sign() <= 0 {
		return errors.New("gasPriceWei must be positive")
	}
	if req.SkipEstimate && req.GasLimit == 0 {
		return errors.New("gasLimit is required when skipEstimate is true")
	}
	return nil
}

func validatePlan(plan domaincontract.ExecutionPlan) error {
	switch plan.Loan.Protocol {
	case domaincontract.FlashLoanProtocolBalancer,
		domaincontract.FlashLoanProtocolUniswapV3,
		domaincontract.FlashLoanProtocolUniswapV4:
	default:
		return fmt.Errorf("unsupported flashLoan.protocol %q", plan.Loan.Protocol)
	}
	if plan.Loan.Lender == (common.Address{}) {
		return errors.New("flashLoan.lender is required")
	}
	if plan.Loan.Protocol != domaincontract.FlashLoanProtocolUniswapV4 && plan.Loan.Token == (common.Address{}) {
		return errors.New("flashLoan.token is required")
	}
	if plan.Loan.Amount == nil || plan.Loan.Amount.Sign() <= 0 {
		return errors.New("flashLoan.amount must be positive")
	}
	if plan.ProfitToken == (common.Address{}) {
		return errors.New("profitToken is required")
	}
	if plan.MinProfit != nil && plan.MinProfit.Sign() < 0 {
		return errors.New("minProfit must be non-negative")
	}
	if plan.Deadline != nil && plan.Deadline.Sign() < 0 {
		return errors.New("deadline must be non-negative")
	}
	for i, route := range plan.Routes {
		if route.RouterAddress == (common.Address{}) {
			return fmt.Errorf("routes[%d].routerAddress is required", i)
		}
		if route.Value != nil && route.Value.Sign() < 0 {
			return fmt.Errorf("routes[%d].value must be non-negative", i)
		}
	}
	return nil
}

func normalizeBroadcastRequest(req domaincontract.BroadcastRequest) domaincontract.BroadcastRequest {
	req.Plan.Loan.Amount = zeroIfNil(req.Plan.Loan.Amount)
	req.Plan.MinProfit = zeroIfNil(req.Plan.MinProfit)
	req.Plan.Deadline = zeroIfNil(req.Plan.Deadline)
	req.GasPriceWei = cloneBigInt(req.GasPriceWei)
	for i := range req.Plan.Routes {
		req.Plan.Routes[i].Value = zeroIfNil(req.Plan.Routes[i].Value)
		req.Plan.Routes[i].Data = append([]byte(nil), req.Plan.Routes[i].Data...)
	}
	return req
}

func zeroIfNil(v *big.Int) *big.Int {
	if v == nil {
		return new(big.Int)
	}
	return cloneBigInt(v)
}

func cloneBigInt(v *big.Int) *big.Int {
	if v == nil {
		return nil
	}
	return new(big.Int).Set(v)
}
