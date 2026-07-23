package arbitrageapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// OpportunityExecuteResult is returned after building and optionally broadcasting a plan.
type OpportunityExecuteResult struct {
	OpportunityID    string
	Plan             domaincontract.ExecutionPlan
	Approvals        []domaincontract.TokenApproval
	TxHash           common.Hash
	ApprovalTxHashes []common.Hash
	Interrupted      bool
	ExecutionJSON    json.RawMessage
}

// OpportunityExecutor loads an opportunity by id, rebuilds an execution plan, audits it, then broadcasts.
type OpportunityExecutor struct {
	repo     domainarb.OpportunityRepository
	builder  ExecutionPlanBuilder
	executor ContractExecutor
	head     ExecutionHeadReader
	cfg      ExecutionConfig
	logger   *zap.Logger
	mu       sync.Mutex
	inFlight map[string]struct{}
	executed map[string]common.Hash
}

func NewOpportunityExecutor(
	repo domainarb.OpportunityRepository,
	builder ExecutionPlanBuilder,
	executor ContractExecutor,
	head ExecutionHeadReader,
	cfg ExecutionConfig,
	logger *zap.Logger,
) *OpportunityExecutor {
	if logger == nil {
		logger = zap.NewNop()
	}
	if builder == nil {
		builder = NewPayloadExecutionPlanBuilder()
	}
	return &OpportunityExecutor{
		repo:     repo,
		builder:  builder,
		executor: executor,
		head:     head,
		cfg:      cfg,
		logger:   logger,
		inFlight: make(map[string]struct{}),
		executed: make(map[string]common.Hash),
	}
}

type OpportunityExecuteRequest struct {
	OpportunityID string
	Confirm       bool
	AuthToken     string
}

func (e *OpportunityExecutor) ExecuteByID(ctx context.Context, opportunityID string) (OpportunityExecuteResult, error) {
	return e.Execute(ctx, OpportunityExecuteRequest{OpportunityID: opportunityID, Confirm: true})
}

func (e *OpportunityExecutor) Execute(ctx context.Context, req OpportunityExecuteRequest) (OpportunityExecuteResult, error) {
	if e == nil {
		return OpportunityExecuteResult{}, errors.New("opportunity executor is not configured")
	}
	opportunityID := strings.TrimSpace(req.OpportunityID)
	if opportunityID == "" {
		return OpportunityExecuteResult{}, errors.New("opportunity-id is required")
	}
	if !req.Confirm {
		return OpportunityExecuteResult{}, errors.New("confirm=true is required to broadcast opportunity execution")
	}
	if err := e.authorize(req.AuthToken); err != nil {
		return OpportunityExecuteResult{}, err
	}
	if e.repo == nil {
		return OpportunityExecuteResult{}, errors.New("opportunity repository is not configured")
	}
	if e.executor == nil {
		return OpportunityExecuteResult{}, errors.New("contract executor is not configured")
	}
	if !e.cfg.Enabled {
		return OpportunityExecuteResult{}, errors.New("arbitrage execution is disabled")
	}
	if strings.TrimSpace(e.cfg.RPCURL) == "" || strings.TrimSpace(e.cfg.PrivateKey) == "" || e.cfg.Executor == (common.Address{}) {
		return OpportunityExecuteResult{}, errors.New("arbitrage execution rpcUrl/privateKey/executor are required")
	}
	if hash, ok := e.begin(opportunityID); !ok {
		result := OpportunityExecuteResult{OpportunityID: opportunityID}
		if hash != (common.Hash{}) {
			result.TxHash = hash
			return result, errors.New("opportunity already executed")
		}
		return result, errors.New("opportunity execution already in progress")
	}
	finished := false
	defer func() {
		if !finished {
			e.finish(opportunityID, common.Hash{})
		}
	}()

	opportunity, err := e.repo.Get(ctx, opportunityID)
	if err != nil {
		return OpportunityExecuteResult{}, err
	}
	if err := e.validateOpportunity(ctx, opportunity); err != nil {
		return OpportunityExecuteResult{}, err
	}

	plan, approvals, err := e.builder.BuildExecutionPlan(ctx, opportunity)
	if err != nil {
		return OpportunityExecuteResult{}, err
	}
	disableContractBuilderPayment(&plan)
	approvals = domaincontract.MergeTokenApprovals(approvals, domaincontract.RequiredTokenApprovals(plan))
	if plan.MinProfit == nil {
		plan.MinProfit = cloneBigIntOrZero(opportunity.NetProfit)
	}
	if err := e.validatePlan(plan, approvals); err != nil {
		return OpportunityExecuteResult{}, err
	}

	executionJSON, err := marshalExecutionPlan(plan, approvals)
	if err != nil {
		return OpportunityExecuteResult{}, fmt.Errorf("marshal execution plan: %w", err)
	}
	e.logger.Info("opportunity execution plan generated",
		zap.String("opportunity_id", opportunity.ID),
		zap.String("strategy_id", opportunity.StrategyID),
		zap.Uint64("block_number", opportunity.BlockNumber),
		zap.ByteString("execution", executionJSON),
	)

	result := OpportunityExecuteResult{
		OpportunityID: opportunity.ID,
		Plan:          plan,
		Approvals:     approvals,
		ExecutionJSON: executionJSON,
	}

	gasPriceWei := cloneBigInt(e.cfg.GasPriceWei)

	approvalResp, err := e.executor.EnsureApprovals(ctx, domaincontract.EnsureApprovalsRequest{
		RPCURL:       strings.TrimSpace(e.cfg.RPCURL),
		PrivateKey:   strings.TrimSpace(e.cfg.PrivateKey),
		Executor:     e.cfg.Executor,
		Approvals:    approvals,
		GasLimit:     e.cfg.GasLimit,
		GasPriceWei:  gasPriceWei,
		SkipEstimate: e.cfg.SkipEstimate,
	})
	if err != nil {
		return result, fmt.Errorf("ensure approvals: %w", err)
	}
	if approvalResp.Broadcast {
		result.Interrupted = true
		result.ApprovalTxHashes = append([]common.Hash(nil), approvalResp.TxHashes...)
		e.logger.Info("opportunity execution interrupted after approvals confirmed",
			zap.String("opportunity_id", opportunity.ID),
			zap.Strings("approval_tx_hashes", hashesToHex(approvalResp.TxHashes)),
			zap.ByteString("execution", executionJSON),
		)
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	broadcastReq := domaincontract.BroadcastRequest{
		RPCURL:            strings.TrimSpace(e.cfg.RPCURL),
		PrivateKey:        strings.TrimSpace(e.cfg.PrivateKey),
		Executor:          e.cfg.Executor,
		Plan:              plan,
		GasLimit:          e.cfg.GasLimit,
		GasPriceWei:       gasPriceWei,
		BuilderPaymentWei: cloneBigInt(opportunity.BuilderPaymentWei),
		SkipEstimate:      e.cfg.SkipEstimate,
		SubmitRPCURL:      strings.TrimSpace(e.cfg.FlashbotsRPCURL),
	}
	resp, err := e.executor.Execute(ctx, broadcastReq)
	if err != nil {
		return result, fmt.Errorf("execute arbitrage: %w", err)
	}
	result.TxHash = resp.TxHash
	if err := e.markExecuted(ctx, opportunity, resp.TxHash); err != nil {
		e.logger.Warn("mark opportunity executed failed",
			zap.String("opportunity_id", opportunity.ID),
			zap.String("tx_hash", resp.TxHash.Hex()),
			zap.Error(err),
		)
	}
	e.finish(opportunityID, resp.TxHash)
	finished = true
	e.logger.Info("opportunity execution broadcast",
		zap.String("opportunity_id", opportunity.ID),
		zap.String("tx_hash", resp.TxHash.Hex()),
		zap.ByteString("execution", executionJSON),
	)
	return result, nil
}

func (e *OpportunityExecutor) authorize(token string) error {
	expected := strings.TrimSpace(e.cfg.BroadcastToken)
	if expected == "" {
		return nil
	}
	if strings.TrimSpace(token) != expected {
		return errors.New("invalid opportunity execution authorization token")
	}
	return nil
}

func (e *OpportunityExecutor) markExecuted(ctx context.Context, opportunity *domainarb.Opportunity, txHash common.Hash) error {
	return markOpportunityExecuted(ctx, e.repo, opportunity, txHash)
}

func markOpportunityExecuted(
	ctx context.Context,
	repo domainarb.OpportunityRepository,
	opportunity *domainarb.Opportunity,
	txHash common.Hash,
) error {
	if opportunity == nil || repo == nil {
		return nil
	}
	payload := make(map[string]any)
	if len(opportunity.Payload) > 0 {
		if err := json.Unmarshal(opportunity.Payload, &payload); err != nil {
			return fmt.Errorf("decode opportunity payload: %w", err)
		}
	}
	payload["status"] = string(domainarb.OpportunityStatusExecuted)
	payload["executionTxHash"] = txHash.Hex()
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode opportunity payload: %w", err)
	}
	opportunity.Status = domainarb.OpportunityStatusExecuted
	opportunity.Payload = rawPayload
	if err := repo.Save(ctx, opportunity); err != nil {
		return fmt.Errorf("save opportunity: %w", err)
	}
	return nil
}

func (e *OpportunityExecutor) begin(opportunityID string) (common.Hash, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if hash, ok := e.executed[opportunityID]; ok {
		return hash, false
	}
	if _, ok := e.inFlight[opportunityID]; ok {
		return common.Hash{}, false
	}
	e.inFlight[opportunityID] = struct{}{}
	return common.Hash{}, true
}

func (e *OpportunityExecutor) finish(opportunityID string, txHash common.Hash) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.inFlight, opportunityID)
	if txHash != (common.Hash{}) {
		e.executed[opportunityID] = txHash
	}
}

func (e *OpportunityExecutor) validateOpportunity(ctx context.Context, opportunity *domainarb.Opportunity) error {
	return validateOpportunityForExecution(ctx, opportunity, e.cfg, e.head)
}

func validateOpportunityForExecution(
	ctx context.Context,
	opportunity *domainarb.Opportunity,
	cfg ExecutionConfig,
	headReader ExecutionHeadReader,
) error {
	if opportunity == nil {
		return errors.New("opportunity is nil")
	}
	if err := opportunity.ApplyPayload(); err != nil {
		return err
	}
	switch opportunity.Status {
	case domainarb.OpportunityStatusAccepted:
		// ok
	case domainarb.OpportunityStatusDiscovered:
		// Legacy payloads encoded "discovered" before acceptance was applied.
		// Scanner only persists profit-accepted opportunities.
		opportunity.Status = domainarb.OpportunityStatusAccepted
	default:
		return fmt.Errorf("opportunity status must be accepted, got %q", opportunity.Status)
	}
	if opportunity.NetProfit == nil || opportunity.NetProfit.Sign() <= 0 {
		return errors.New("opportunity netProfit must be positive")
	}
	if cfg.MaxOpportunityAge == 0 {
		return nil
	}
	if headReader == nil {
		return errors.New("execution head reader is required when max opportunity age is configured")
	}
	head, err := headReader.GetLatestBlockHeader(ctx)
	if err != nil {
		return fmt.Errorf("latest block header: %w", err)
	}
	if head.Number < opportunity.BlockNumber {
		return fmt.Errorf("opportunity block %d is ahead of chain head %d", opportunity.BlockNumber, head.Number)
	}
	age := head.Number - opportunity.BlockNumber
	if age > cfg.MaxOpportunityAge {
		return fmt.Errorf("opportunity is stale: age %d blocks exceeds max %d", age, cfg.MaxOpportunityAge)
	}
	return nil
}

func (e *OpportunityExecutor) validatePlan(plan domaincontract.ExecutionPlan, approvals []domaincontract.TokenApproval) error {
	return validateExecutionPlanForConfig(plan, approvals, e.cfg)
}

func validateExecutionPlanForConfig(plan domaincontract.ExecutionPlan, approvals []domaincontract.TokenApproval, cfg ExecutionConfig) error {
	if plan.CoinbasePaymentBPS > 0 {
		if plan.ProfitToken != (common.Address{}) && plan.ProfitToken != plan.WrappedNativeToken && len(plan.SettlementRoutes) == 0 {
			return fmt.Errorf("coinbase payment requires native or wrapped-native profit token")
		}
		if plan.ProfitToken != (common.Address{}) && plan.WrappedNativeToken == (common.Address{}) {
			return fmt.Errorf("wrapped native token is required for coinbase payment")
		}
	}
	routes := make([]domaincontract.SwapRoute, 0, len(plan.Routes)+len(plan.SettlementRoutes))
	routes = append(routes, plan.Routes...)
	routes = append(routes, plan.SettlementRoutes...)
	allowedRouters := addressSet(cfg.AllowedRouters)
	if len(allowedRouters) > 0 {
		for i, route := range routes {
			if !allowedRouters[route.RouterAddress] {
				return fmt.Errorf("routes[%d].routerAddress %s is not allowed", i, route.RouterAddress.Hex())
			}
			if err := validateRouteFillSlot(i, route); err != nil {
				return err
			}
		}
	} else {
		for i, route := range routes {
			if err := validateRouteFillSlot(i, route); err != nil {
				return err
			}
		}
	}
	routeSpenders := make(map[common.Address]bool, len(routes))
	for _, route := range routes {
		routeSpenders[route.RouterAddress] = true
	}
	allowedSpenders := addressSet(cfg.AllowedSpenders)
	if len(allowedSpenders) == 0 {
		allowedSpenders = allowedRouters
	}
	for i, approval := range approvals {
		if !routeSpenders[approval.Spender] && !allowedSpenders[approval.Spender] {
			return fmt.Errorf("approvals[%d].spender %s is not used by execution routes", i, approval.Spender.Hex())
		}
	}
	return nil
}

func validateRouteFillSlot(index int, route domaincontract.SwapRoute) error {
	switch route.FillSource {
	case domaincontract.FillSourceNone:
		if route.PatchAmount || route.AmountAsCallValue || route.FillToken != (common.Address{}) {
			return fmt.Errorf("routes[%d] has fill options but fillSource is none", index)
		}
		return nil
	case domaincontract.FillSourceERC20Balance:
		if route.FillToken == (common.Address{}) || route.FillToken == domaincontract.NativeETHSentinel {
			return fmt.Errorf("routes[%d].fillToken must be an ERC20 token", index)
		}
		if route.AmountAsCallValue {
			return fmt.Errorf("routes[%d].amountAsCallValue cannot be used with ERC20 fill", index)
		}
		if route.Value != nil && route.Value.Sign() > 0 {
			return fmt.Errorf("routes[%d].value cannot be used with ERC20 fill", index)
		}
	case domaincontract.FillSourceNativeBalance:
		if route.FillToken != (common.Address{}) {
			return fmt.Errorf("routes[%d].fillToken must be empty for native fill", index)
		}
	default:
		return fmt.Errorf("routes[%d].fillSource is invalid", index)
	}
	if route.PatchAmount {
		if route.FillOffset <= uint64(len(route.Data)) && uint64(len(route.Data))-route.FillOffset >= 32 {
			return nil
		}
		return fmt.Errorf("routes[%d].fillOffset %d does not fit calldata length %d", index, route.FillOffset, len(route.Data))
	}
	return nil
}

func addressSet(values []common.Address) map[common.Address]bool {
	out := make(map[common.Address]bool, len(values))
	for _, value := range values {
		if value != (common.Address{}) {
			out[value] = true
		}
	}
	return out
}

func marshalExecutionPlan(plan domaincontract.ExecutionPlan, approvals []domaincontract.TokenApproval) (json.RawMessage, error) {
	type routeJSON struct {
		RouterAddress     string `json:"routerAddress"`
		Value             string `json:"value,omitempty"`
		Data              string `json:"data"`
		FillSource        string `json:"fillSource,omitempty"`
		FillToken         string `json:"fillToken,omitempty"`
		PatchAmount       bool   `json:"patchAmount,omitempty"`
		AmountAsCallValue bool   `json:"amountAsCallValue,omitempty"`
		FillOffset        uint64 `json:"fillOffset,omitempty"`
	}
	type approvalJSON struct {
		Token   string `json:"token"`
		Spender string `json:"spender"`
		Amount  string `json:"amount"`
	}
	type flashJSON struct {
		Protocol     string `json:"protocol"`
		Lender       string `json:"lender"`
		Token        string `json:"token"`
		Amount       string `json:"amount"`
		BorrowToken0 bool   `json:"borrowToken0,omitempty"`
	}
	type planJSON struct {
		FlashLoan        flashJSON      `json:"flashLoan"`
		Routes           []routeJSON    `json:"routes"`
		SettleCurrencies []string       `json:"settleCurrencies,omitempty"`
		ProfitToken      string         `json:"profitToken"`
		MinProfit        string         `json:"minProfit,omitempty"`
		Deadline         string         `json:"deadline,omitempty"`
		Approvals        []approvalJSON `json:"approvals,omitempty"`
	}

	out := planJSON{
		FlashLoan: flashJSON{
			Protocol:     string(plan.Loan.Protocol),
			Lender:       plan.Loan.Lender.Hex(),
			Token:        plan.Loan.Token.Hex(),
			Amount:       bigIntStringOrEmpty(plan.Loan.Amount),
			BorrowToken0: plan.Loan.BorrowToken0,
		},
		Routes:      make([]routeJSON, 0, len(plan.Routes)),
		ProfitToken: plan.ProfitToken.Hex(),
		MinProfit:   bigIntStringOrEmpty(plan.MinProfit),
		Deadline:    bigIntStringOrEmpty(plan.Deadline),
		Approvals:   make([]approvalJSON, 0, len(approvals)),
	}
	for _, currency := range plan.SettleCurrencies {
		out.SettleCurrencies = append(out.SettleCurrencies, currency.Hex())
	}
	for _, route := range plan.Routes {
		item := routeJSON{
			RouterAddress: route.RouterAddress.Hex(),
			Data:          "0x" + common.Bytes2Hex(route.Data),
			FillOffset:    route.FillOffset,
		}
		if route.Value != nil && route.Value.Sign() > 0 {
			item.Value = route.Value.String()
		}
		if route.FillToken != (common.Address{}) {
			item.FillToken = route.FillToken.Hex()
		}
		if route.FillSource != domaincontract.FillSourceNone {
			item.FillSource = fillSourceString(route.FillSource)
			item.PatchAmount = route.PatchAmount
			item.AmountAsCallValue = route.AmountAsCallValue
		}
		out.Routes = append(out.Routes, item)
	}
	for _, approval := range approvals {
		out.Approvals = append(out.Approvals, approvalJSON{
			Token:   approval.Token.Hex(),
			Spender: approval.Spender.Hex(),
			Amount:  bigIntStringOrEmpty(approval.Amount),
		})
	}
	return json.Marshal(out)
}

func fillSourceString(value domaincontract.FillSource) string {
	switch value {
	case domaincontract.FillSourceERC20Balance:
		return "erc20Balance"
	case domaincontract.FillSourceNativeBalance:
		return "nativeBalance"
	default:
		return ""
	}
}

func bigIntStringOrEmpty(value *big.Int) string {
	if value == nil {
		return ""
	}
	return value.String()
}
