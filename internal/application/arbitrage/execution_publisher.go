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
	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

var ErrExecutionPlanUnavailable = errors.New("execution plan unavailable")

type ContractExecutor interface {
	EnsureApprovals(context.Context, domaincontract.EnsureApprovalsRequest) (domaincontract.EnsureApprovalsResponse, error)
	Simulate(context.Context, domaincontract.BroadcastRequest) error
	Execute(context.Context, domaincontract.BroadcastRequest) (domaincontract.BroadcastResponse, error)
}

type ExecutionPlanBuilder interface {
	BuildExecutionPlan(context.Context, *domainarb.Opportunity) (domaincontract.ExecutionPlan, []domaincontract.TokenApproval, error)
}

type ExecutionHeadReader interface {
	GetLatestBlockHeader(ctx context.Context) (domainchain.BlockHeader, error)
}

type ExecutionConfig struct {
	Enabled             bool
	RPCURL              string
	PrivateKey          string
	Executor            common.Address
	FlashbotsRPCURL     string
	FlashbotsPaymentBPS uint64
	WrappedNativeToken  common.Address
	GasLimit            uint64
	GasPriceWei         *big.Int
	SkipEstimate        bool
	BroadcastToken      string
	MaxOpportunityAge   uint64
	AllowedRouters      []common.Address
	AllowedSpenders     []common.Address
}

type ExecutionPublisher struct {
	cfg      ExecutionConfig
	repo     domainarb.OpportunityRepository
	builder  ExecutionPlanBuilder
	executor ContractExecutor
	head     ExecutionHeadReader
	logger   *zap.Logger
	mu       sync.Mutex
	inFlight map[string]struct{}
	executed map[string]common.Hash
}

func NewExecutionPublisher(
	cfg ExecutionConfig,
	builder ExecutionPlanBuilder,
	executor ContractExecutor,
	args ...any,
) *ExecutionPublisher {
	var repo domainarb.OpportunityRepository
	var head ExecutionHeadReader
	var logger *zap.Logger
	for _, arg := range args {
		switch value := arg.(type) {
		case domainarb.OpportunityRepository:
			repo = value
		case ExecutionHeadReader:
			head = value
		case *zap.Logger:
			logger = value
		}
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ExecutionPublisher{
		cfg:      cfg,
		repo:     repo,
		builder:  builder,
		executor: executor,
		head:     head,
		logger:   logger,
		inFlight: make(map[string]struct{}),
		executed: make(map[string]common.Hash),
	}
}

func (p *ExecutionPublisher) Publish(ctx context.Context, opportunity *domainarb.Opportunity) error {
	if p == nil || !p.cfg.Enabled || opportunity == nil {
		return nil
	}
	if p.builder == nil {
		return errors.New("arbitrage execution plan builder is not configured")
	}
	if p.executor == nil {
		return errors.New("contract executor is not configured")
	}
	if err := validateOpportunityForExecution(ctx, opportunity, p.cfg, p.head); err != nil {
		return fmt.Errorf("validate opportunity: %w", err)
	}
	if hash, ok := p.begin(opportunity.ID); !ok {
		if hash != (common.Hash{}) {
			p.logger.Info("arbitrage execution skipped",
				zap.String("opportunity", opportunity.ID),
				zap.String("reason", "already_executed"),
				zap.String("tx_hash", hash.Hex()),
			)
			return nil
		}
		p.logger.Info("arbitrage execution skipped",
			zap.String("opportunity", opportunity.ID),
			zap.String("reason", "already_in_progress"),
		)
		return nil
	}
	finished := false
	defer func() {
		if !finished {
			p.finish(opportunity.ID, common.Hash{})
		}
	}()

	plan, approvals, err := p.builder.BuildExecutionPlan(ctx, opportunity)
	if err != nil {
		if errors.Is(err, ErrExecutionPlanUnavailable) {
			p.logger.Info("arbitrage execution skipped",
				zap.String("opportunity", opportunity.ID),
				zap.String("reason", "execution_plan_unavailable"),
				zap.Error(err),
			)
			return nil
		}
		return fmt.Errorf("build execution plan: %w", err)
	}
	approvals = domaincontract.MergeTokenApprovals(approvals, domaincontract.RequiredTokenApprovals(plan))
	if plan.MinProfit == nil {
		plan.MinProfit = cloneBigIntOrZero(opportunity.NetProfit)
	}
	applyCoinbasePaymentConfig(&plan, p.cfg)
	if err := validateExecutionPlanForConfig(plan, approvals, p.cfg); err != nil {
		return fmt.Errorf("validate execution plan: %w", err)
	}

	gasPriceWei := cloneBigInt(p.cfg.GasPriceWei)

	approvalResp, err := p.executor.EnsureApprovals(ctx, domaincontract.EnsureApprovalsRequest{
		RPCURL:       strings.TrimSpace(p.cfg.RPCURL),
		PrivateKey:   strings.TrimSpace(p.cfg.PrivateKey),
		Executor:     p.cfg.Executor,
		Approvals:    approvals,
		GasLimit:     p.cfg.GasLimit,
		GasPriceWei:  gasPriceWei,
		SkipEstimate: p.cfg.SkipEstimate,
	})
	if err != nil {
		return fmt.Errorf("ensure approvals: %w", err)
	}
	if approvalResp.Broadcast {
		p.logger.Info("arbitrage approval broadcast, execution interrupted",
			zap.String("opportunity", opportunity.ID),
			zap.Int("approvals", len(approvalResp.TxHashes)),
			zap.Strings("tx_hashes", hashesToHex(approvalResp.TxHashes)),
		)
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	broadcastReq := domaincontract.BroadcastRequest{
		RPCURL:       strings.TrimSpace(p.cfg.RPCURL),
		PrivateKey:   strings.TrimSpace(p.cfg.PrivateKey),
		Executor:     p.cfg.Executor,
		Plan:         plan,
		GasLimit:     p.cfg.GasLimit,
		GasPriceWei:  gasPriceWei,
		SkipEstimate: p.cfg.SkipEstimate,
		SubmitRPCURL: strings.TrimSpace(p.cfg.FlashbotsRPCURL),
	}
	resp, err := p.executor.Execute(ctx, broadcastReq)
	if err != nil {
		return fmt.Errorf("execute arbitrage: %w", err)
	}
	if err := markOpportunityExecuted(ctx, p.repo, opportunity, resp.TxHash); err != nil {
		p.logger.Warn("mark opportunity executed failed",
			zap.String("opportunity", opportunity.ID),
			zap.String("tx_hash", resp.TxHash.Hex()),
			zap.Error(err),
		)
	}
	p.finish(opportunity.ID, resp.TxHash)
	finished = true
	p.logger.Info("arbitrage execution broadcast",
		zap.String("opportunity", opportunity.ID),
		zap.String("tx_hash", resp.TxHash.Hex()),
		zap.String("flashbots_rpc", strings.TrimSpace(p.cfg.FlashbotsRPCURL)),
	)
	return nil
}

func (p *ExecutionPublisher) begin(opportunityID string) (common.Hash, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if hash, ok := p.executed[opportunityID]; ok {
		return hash, false
	}
	if _, ok := p.inFlight[opportunityID]; ok {
		return common.Hash{}, false
	}
	p.inFlight[opportunityID] = struct{}{}
	return common.Hash{}, true
}

func (p *ExecutionPublisher) finish(opportunityID string, txHash common.Hash) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.inFlight, opportunityID)
	if txHash != (common.Hash{}) {
		p.executed[opportunityID] = txHash
	}
}

type PayloadExecutionPlanBuilder struct{}

func NewPayloadExecutionPlanBuilder() *PayloadExecutionPlanBuilder {
	return &PayloadExecutionPlanBuilder{}
}

type executionPayload struct {
	Execution *executionPlanPayload `json:"execution,omitempty"`
}

type executionPlanPayload struct {
	FlashLoan        flashLoanPayload   `json:"flashLoan"`
	Routes           []swapRoutePayload `json:"routes"`
	SettleCurrencies []string           `json:"settleCurrencies,omitempty"`
	ProfitToken      string             `json:"profitToken"`
	MinProfit        string             `json:"minProfit,omitempty"`
	Deadline         string             `json:"deadline,omitempty"`
	Approvals        []approvalPayload  `json:"approvals,omitempty"`
}

type flashLoanPayload struct {
	Protocol     string `json:"protocol"`
	Lender       string `json:"lender"`
	Token        string `json:"token"`
	Amount       string `json:"amount"`
	BorrowToken0 bool   `json:"borrowToken0,omitempty"`
}

type swapRoutePayload struct {
	RouterAddress     string `json:"routerAddress"`
	Value             string `json:"value,omitempty"`
	Data              string `json:"data"`
	FillSource        string `json:"fillSource,omitempty"`
	FillToken         string `json:"fillToken,omitempty"`
	PatchAmount       *bool  `json:"patchAmount,omitempty"`
	AmountAsCallValue *bool  `json:"amountAsCallValue,omitempty"`
	FillOffset        uint64 `json:"fillOffset,omitempty"`
}

type approvalPayload struct {
	Token   string `json:"token"`
	Spender string `json:"spender"`
	Amount  string `json:"amount"`
}

func (b *PayloadExecutionPlanBuilder) BuildExecutionPlan(_ context.Context, opportunity *domainarb.Opportunity) (domaincontract.ExecutionPlan, []domaincontract.TokenApproval, error) {
	if opportunity == nil {
		return domaincontract.ExecutionPlan{}, nil, errors.New("opportunity is nil")
	}
	if err := opportunity.ApplyPayload(); err != nil {
		return domaincontract.ExecutionPlan{}, nil, err
	}
	var payload executionPayload
	if len(opportunity.Payload) == 0 {
		return domaincontract.ExecutionPlan{}, nil, ErrExecutionPlanUnavailable
	}
	if err := json.Unmarshal(opportunity.Payload, &payload); err != nil {
		return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("decode opportunity payload: %w", err)
	}
	if payload.Execution == nil {
		return domaincontract.ExecutionPlan{}, nil, ErrExecutionPlanUnavailable
	}
	return payload.Execution.toDomain()
}

func (p executionPlanPayload) toDomain() (domaincontract.ExecutionPlan, []domaincontract.TokenApproval, error) {
	loan, err := p.FlashLoan.toDomain()
	if err != nil {
		return domaincontract.ExecutionPlan{}, nil, err
	}
	routes := make([]domaincontract.SwapRoute, 0, len(p.Routes))
	for i, route := range p.Routes {
		item, err := route.toDomain()
		if err != nil {
			return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("routes[%d]: %w", i, err)
		}
		routes = append(routes, item)
	}
	settleCurrencies := make([]common.Address, 0, len(p.SettleCurrencies))
	for i, raw := range p.SettleCurrencies {
		address, err := parsePayloadAddress(raw, fmt.Sprintf("settleCurrencies[%d]", i))
		if err != nil {
			return domaincontract.ExecutionPlan{}, nil, err
		}
		settleCurrencies = append(settleCurrencies, address)
	}
	profitToken, err := parsePayloadAddress(p.ProfitToken, "profitToken")
	if err != nil {
		return domaincontract.ExecutionPlan{}, nil, err
	}
	approvals := make([]domaincontract.TokenApproval, 0, len(p.Approvals))
	for i, approval := range p.Approvals {
		item, err := approval.toDomain()
		if err != nil {
			return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("approvals[%d]: %w", i, err)
		}
		approvals = append(approvals, item)
	}
	return domaincontract.ExecutionPlan{
		Loan:             loan,
		Routes:           routes,
		SettleCurrencies: settleCurrencies,
		ProfitToken:      profitToken,
		MinProfit:        parseOptionalPayloadBigInt(p.MinProfit),
		Deadline:         parseOptionalPayloadBigInt(p.Deadline),
	}, approvals, nil
}

func (p flashLoanPayload) toDomain() (domaincontract.FlashLoan, error) {
	lender, err := parsePayloadAddress(p.Lender, "flashLoan.lender")
	if err != nil {
		return domaincontract.FlashLoan{}, err
	}
	token, err := parsePayloadAddressAllowZero(p.Token, "flashLoan.token")
	if err != nil {
		return domaincontract.FlashLoan{}, err
	}
	return domaincontract.FlashLoan{
		Protocol:     domaincontract.FlashLoanProtocol(p.Protocol),
		Lender:       lender,
		Token:        token,
		Amount:       parseRequiredPayloadBigInt(p.Amount, "flashLoan.amount"),
		BorrowToken0: p.BorrowToken0,
	}, nil
}

func (p swapRoutePayload) toDomain() (domaincontract.SwapRoute, error) {
	router, err := parsePayloadAddress(p.RouterAddress, "routerAddress")
	if err != nil {
		return domaincontract.SwapRoute{}, err
	}
	data, err := parsePayloadHex(p.Data, "data")
	if err != nil {
		return domaincontract.SwapRoute{}, err
	}
	fillToken := common.Address{}
	if strings.TrimSpace(p.FillToken) != "" {
		fillToken, err = parsePayloadAddressAllowNative(p.FillToken, "fillToken")
		if err != nil {
			return domaincontract.SwapRoute{}, err
		}
	}
	fillSource, fillToken, patchAmount, amountAsCallValue, err := p.resolveFillOptions(fillToken)
	if err != nil {
		return domaincontract.SwapRoute{}, err
	}
	return domaincontract.SwapRoute{
		RouterAddress:     router,
		Value:             parseOptionalPayloadBigInt(p.Value),
		Data:              data,
		FillSource:        fillSource,
		FillToken:         fillToken,
		PatchAmount:       patchAmount,
		AmountAsCallValue: amountAsCallValue,
		FillOffset:        p.FillOffset,
	}, nil
}

func (p swapRoutePayload) resolveFillOptions(fillToken common.Address) (domaincontract.FillSource, common.Address, bool, bool, error) {
	rawSource := strings.TrimSpace(p.FillSource)
	if rawSource != "" {
		fillSource, err := parseFillSource(rawSource)
		if err != nil {
			return domaincontract.FillSourceNone, common.Address{}, false, false, err
		}
		patchAmount := boolValue(p.PatchAmount)
		amountAsCallValue := boolValue(p.AmountAsCallValue)
		if fillSource == domaincontract.FillSourceNativeBalance {
			fillToken = common.Address{}
		}
		return fillSource, fillToken, patchAmount, amountAsCallValue, nil
	}
	if fillToken == (common.Address{}) {
		return domaincontract.FillSourceNone, common.Address{}, false, false, nil
	}
	if fillToken == domaincontract.NativeETHSentinel {
		patchAmount := p.FillOffset != 0
		if p.PatchAmount != nil {
			patchAmount = *p.PatchAmount
		}
		amountAsCallValue := true
		if p.AmountAsCallValue != nil {
			amountAsCallValue = *p.AmountAsCallValue
		}
		return domaincontract.FillSourceNativeBalance, common.Address{}, patchAmount, amountAsCallValue, nil
	}
	patchAmount := true
	if p.PatchAmount != nil {
		patchAmount = *p.PatchAmount
	}
	return domaincontract.FillSourceERC20Balance, fillToken, patchAmount, boolValue(p.AmountAsCallValue), nil
}

func parseFillSource(raw string) (domaincontract.FillSource, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "none":
		return domaincontract.FillSourceNone, nil
	case "erc20", "erc20balance", "erc20_balance":
		return domaincontract.FillSourceERC20Balance, nil
	case "native", "nativebalance", "native_balance", "eth":
		return domaincontract.FillSourceNativeBalance, nil
	default:
		return domaincontract.FillSourceNone, fmt.Errorf("fillSource %q is invalid", raw)
	}
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func (p approvalPayload) toDomain() (domaincontract.TokenApproval, error) {
	token, err := parsePayloadAddress(p.Token, "token")
	if err != nil {
		return domaincontract.TokenApproval{}, err
	}
	spender, err := parsePayloadAddress(p.Spender, "spender")
	if err != nil {
		return domaincontract.TokenApproval{}, err
	}
	return domaincontract.TokenApproval{
		Token:   token,
		Spender: spender,
		Amount:  parseRequiredPayloadBigInt(p.Amount, "amount"),
	}, nil
}

func parsePayloadAddress(raw, field string) (common.Address, error) {
	address, err := parsePayloadAddressAllowZero(raw, field)
	if err != nil {
		return common.Address{}, err
	}
	if address == (common.Address{}) {
		return common.Address{}, fmt.Errorf("%s is required", field)
	}
	return address, nil
}

func parsePayloadAddressAllowZero(raw, field string) (common.Address, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return common.Address{}, nil
	}
	if !common.IsHexAddress(raw) {
		return common.Address{}, fmt.Errorf("%s must be an address", field)
	}
	return common.HexToAddress(raw), nil
}

func parsePayloadAddressAllowNative(raw, field string) (common.Address, error) {
	raw = strings.TrimSpace(raw)
	if strings.EqualFold(raw, "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE") {
		return common.HexToAddress(raw), nil
	}
	return parsePayloadAddressAllowZero(raw, field)
}

func parseRequiredPayloadBigInt(raw, field string) *big.Int {
	value := parseOptionalPayloadBigInt(raw)
	if value == nil || value.Sign() <= 0 {
		return big.NewInt(0)
	}
	return value
}

func parseOptionalPayloadBigInt(raw string) *big.Int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	value, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return nil
	}
	return value
}

func parsePayloadHex(raw, field string) ([]byte, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "0x")
	if raw == "" {
		return nil, fmt.Errorf("%s is required", field)
	}
	data, ok := new(big.Int).SetString(raw, 16)
	if !ok {
		return nil, fmt.Errorf("%s must be hex", field)
	}
	bytes := data.Bytes()
	if len(bytes)*2 < len(raw) {
		padded := make([]byte, (len(raw)+1)/2)
		copy(padded[len(padded)-len(bytes):], bytes)
		bytes = padded
	}
	return bytes, nil
}

func hashesToHex(hashes []common.Hash) []string {
	out := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		out = append(out, hash.Hex())
	}
	return out
}

func cloneBigInt(value *big.Int) *big.Int {
	if value == nil {
		return nil
	}
	return new(big.Int).Set(value)
}

func cloneBigIntOrZero(value *big.Int) *big.Int {
	if value == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(value)
}
