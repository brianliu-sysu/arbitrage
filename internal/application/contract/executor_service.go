package contract

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"

	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	"github.com/ethereum/go-ethereum/common"
)

type Broadcaster interface {
	BroadcastExecution(ctx context.Context, req domaincontract.BroadcastRequest) (domaincontract.BroadcastResponse, error)
	SimulateExecution(ctx context.Context, req domaincontract.BroadcastRequest) error
	Allowance(ctx context.Context, rpcURL string, token, owner, spender common.Address) (*big.Int, error)
	BroadcastApprove(ctx context.Context, req domaincontract.BroadcastRequest, approval domaincontract.TokenApproval) (domaincontract.BroadcastResponse, error)
}

type AppService struct {
	broadcaster Broadcaster
	mu          sync.Mutex
	allowances  map[approvalKey]*big.Int
}

type approvalKey struct {
	RPCURL   string
	Executor common.Address
	Token    common.Address
	Spender  common.Address
}

func NewAppService(broadcaster Broadcaster) *AppService {
	return &AppService{
		broadcaster: broadcaster,
		allowances:  make(map[approvalKey]*big.Int),
	}
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

func (s *AppService) Simulate(ctx context.Context, req domaincontract.BroadcastRequest) error {
	if s == nil || s.broadcaster == nil {
		return errors.New("contract executor broadcaster is not configured")
	}
	if err := ValidateBroadcastRequest(req); err != nil {
		return err
	}
	return s.broadcaster.SimulateExecution(ctx, normalizeBroadcastRequest(req))
}

func (s *AppService) EnsureApprovals(ctx context.Context, req domaincontract.EnsureApprovalsRequest) (domaincontract.EnsureApprovalsResponse, error) {
	if s == nil || s.broadcaster == nil {
		return domaincontract.EnsureApprovalsResponse{}, errors.New("contract executor broadcaster is not configured")
	}
	if err := ValidateApprovalRequest(req); err != nil {
		return domaincontract.EnsureApprovalsResponse{}, err
	}
	normalized := normalizeApprovalRequest(req)

	missing, err := s.missingApprovals(ctx, normalized)
	if err != nil {
		return domaincontract.EnsureApprovalsResponse{}, err
	}
	if len(missing) == 0 {
		return domaincontract.EnsureApprovalsResponse{}, nil
	}

	txHashes := make([]common.Hash, 0, len(missing))
	for _, approval := range missing {
		resp, err := s.broadcaster.BroadcastApprove(ctx, approvalBroadcastRequest(normalized), approval)
		if err != nil {
			return domaincontract.EnsureApprovalsResponse{}, err
		}
		txHashes = append(txHashes, resp.TxHash)
		s.storeAllowance(normalized, approval, maxUint256())
	}
	return domaincontract.EnsureApprovalsResponse{TxHashes: txHashes, Broadcast: true}, nil
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

func ValidateApprovalRequest(req domaincontract.EnsureApprovalsRequest) error {
	if strings.TrimSpace(req.RPCURL) == "" {
		return errors.New("rpcUrl is required")
	}
	if strings.TrimSpace(req.PrivateKey) == "" {
		return errors.New("privateKey is required")
	}
	if req.Executor == (common.Address{}) {
		return errors.New("executor is required")
	}
	for i, approval := range req.Approvals {
		if approval.Token == (common.Address{}) {
			return fmt.Errorf("approvals[%d].token is required", i)
		}
		if approval.Spender == (common.Address{}) {
			return fmt.Errorf("approvals[%d].spender is required", i)
		}
		if approval.Amount == nil || approval.Amount.Sign() <= 0 {
			return fmt.Errorf("approvals[%d].amount must be positive", i)
		}
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
		if err := validateRouteFillSlot(i, route); err != nil {
			return err
		}
	}
	return nil
}

func validateRouteFillSlot(index int, route domaincontract.SwapRoute) error {
	if route.FillToken == (common.Address{}) {
		return nil
	}
	if route.FillOffset == 0 && route.FillToken == domaincontract.NativeETHSentinel {
		return nil
	}
	if route.FillOffset <= uint64(len(route.Data)) && uint64(len(route.Data))-route.FillOffset >= 32 {
		return nil
	}
	return fmt.Errorf("routes[%d].fillOffset %d does not fit calldata length %d", index, route.FillOffset, len(route.Data))
}

func normalizeBroadcastRequest(req domaincontract.BroadcastRequest) domaincontract.BroadcastRequest {
	req.Plan.Loan.Amount = zeroIfNil(req.Plan.Loan.Amount)
	req.Plan.MinProfit = zeroIfNil(req.Plan.MinProfit)
	req.Plan.Deadline = zeroIfNil(req.Plan.Deadline)
	req.GasPriceWei = cloneBigInt(req.GasPriceWei)
	req.SubmitRPCURL = strings.TrimSpace(req.SubmitRPCURL)
	for i := range req.Plan.Routes {
		req.Plan.Routes[i].Value = zeroIfNil(req.Plan.Routes[i].Value)
		req.Plan.Routes[i].Data = append([]byte(nil), req.Plan.Routes[i].Data...)
	}
	return req
}

func normalizeApprovalRequest(req domaincontract.EnsureApprovalsRequest) domaincontract.EnsureApprovalsRequest {
	req.RPCURL = strings.TrimSpace(req.RPCURL)
	req.PrivateKey = strings.TrimSpace(req.PrivateKey)
	req.GasPriceWei = cloneBigInt(req.GasPriceWei)
	req.SubmitRPCURL = strings.TrimSpace(req.SubmitRPCURL)
	req.Approvals = normalizeApprovals(req.Approvals)
	return req
}

func normalizeApprovals(approvals []domaincontract.TokenApproval) []domaincontract.TokenApproval {
	out := make([]domaincontract.TokenApproval, 0, len(approvals))
	seen := make(map[[2]common.Address]int, len(approvals))
	for _, approval := range approvals {
		approval.Amount = zeroIfNil(approval.Amount)
		key := [2]common.Address{approval.Token, approval.Spender}
		if index, ok := seen[key]; ok {
			if approval.Amount.Cmp(out[index].Amount) > 0 {
				out[index].Amount = approval.Amount
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, approval)
	}
	return out
}

func (s *AppService) missingApprovals(ctx context.Context, req domaincontract.EnsureApprovalsRequest) ([]domaincontract.TokenApproval, error) {
	missing := make([]domaincontract.TokenApproval, 0)
	for _, approval := range req.Approvals {
		cached := s.cachedAllowance(req, approval)
		if cached != nil && cached.Cmp(approval.Amount) >= 0 {
			continue
		}
		onChain, err := s.broadcaster.Allowance(ctx, req.RPCURL, approval.Token, req.Executor, approval.Spender)
		if err != nil {
			return nil, err
		}
		s.storeAllowance(req, approval, onChain)
		if onChain.Cmp(approval.Amount) < 0 {
			missing = append(missing, approval)
		}
	}
	return missing, nil
}

func (s *AppService) cachedAllowance(req domaincontract.EnsureApprovalsRequest, approval domaincontract.TokenApproval) *big.Int {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.allowances[approvalCacheKey(req, approval)]
	return cloneBigInt(value)
}

func (s *AppService) storeAllowance(req domaincontract.EnsureApprovalsRequest, approval domaincontract.TokenApproval, allowance *big.Int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.allowances == nil {
		s.allowances = make(map[approvalKey]*big.Int)
	}
	s.allowances[approvalCacheKey(req, approval)] = zeroIfNil(allowance)
}

func approvalCacheKey(req domaincontract.EnsureApprovalsRequest, approval domaincontract.TokenApproval) approvalKey {
	return approvalKey{
		RPCURL:   req.RPCURL,
		Executor: req.Executor,
		Token:    approval.Token,
		Spender:  approval.Spender,
	}
}

func approvalBroadcastRequest(req domaincontract.EnsureApprovalsRequest) domaincontract.BroadcastRequest {
	return domaincontract.BroadcastRequest{
		RPCURL:       req.RPCURL,
		PrivateKey:   req.PrivateKey,
		Executor:     req.Executor,
		GasLimit:     req.GasLimit,
		GasPriceWei:  cloneBigInt(req.GasPriceWei),
		SkipEstimate: req.SkipEstimate,
		SubmitRPCURL: req.SubmitRPCURL,
	}
}

func maxUint256() *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
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
