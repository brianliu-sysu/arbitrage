package arbitrageapp

import (
	"context"
	"math/big"
	"testing"
	"time"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

func TestExecutionPublisherBroadcastsApprovalAndInterrupts(t *testing.T) {
	executor := &fakeExecutionExecutor{
		approvalResp: domaincontract.EnsureApprovalsResponse{
			Broadcast: true,
			TxHashes:  []common.Hash{common.HexToHash("0x1")},
		},
	}
	publisher := NewExecutionPublisher(testExecutionConfig(), fakeExecutionBuilder{}, executor, nil)

	if err := publisher.Publish(context.Background(), testOpportunity()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if executor.approvalCalls != 1 {
		t.Fatalf("expected approval call, got %d", executor.approvalCalls)
	}
	if executor.executeCalls != 0 {
		t.Fatalf("expected execution interrupted, got %d calls", executor.executeCalls)
	}
}

func TestExecutionPublisherPassesCoinbasePaymentPlan(t *testing.T) {
	executor := &fakeExecutionExecutor{}
	cfg := testExecutionConfig()
	cfg.FlashbotsPaymentBPS = 8000
	cfg.GasLimit = 100
	publisher := NewExecutionPublisher(cfg, fakeExecutionBuilder{}, executor, nil)

	if err := publisher.Publish(context.Background(), testOpportunity()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if executor.executeCalls != 1 {
		t.Fatalf("expected execution call, got %d", executor.executeCalls)
	}
	if executor.simulateCalls != 0 {
		t.Fatalf("expected bundle broadcaster to own simulation, got %d application simulations", executor.simulateCalls)
	}
	if executor.executeReq.Plan.CoinbasePaymentBPS != 8000 {
		t.Fatalf("expected coinbase payment bps 8000, got %d", executor.executeReq.Plan.CoinbasePaymentBPS)
	}
	if executor.executeReq.Plan.MinProfit.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected pre-evaluated min profit to remain 1, got %s", executor.executeReq.Plan.MinProfit)
	}
	if executor.executeReq.SubmitRPCURL != "https://relay.flashbots.net" {
		t.Fatalf("unexpected submit rpc %q", executor.executeReq.SubmitRPCURL)
	}
	if executor.approvalReq.SubmitRPCURL != "" {
		t.Fatalf("approval submit rpc should be empty, got %q", executor.approvalReq.SubmitRPCURL)
	}
}

func TestExecutionPublisherSkipsSimulationRevert(t *testing.T) {
	executor := &fakeExecutionExecutor{executeErr: domaincontract.ErrExecutionSimulationReverted}
	publisher := NewExecutionPublisher(testExecutionConfig(), fakeExecutionBuilder{}, executor, nil)

	if err := publisher.Publish(context.Background(), testOpportunity()); err != nil {
		t.Fatalf("expected simulation revert to skip opportunity, got %v", err)
	}
}

func TestApplyCoinbasePaymentConfigFallsBackForUnsettledERC20Profit(t *testing.T) {
	plan := domaincontract.ExecutionPlan{ProfitToken: common.HexToAddress("0x1")}
	cfg := testExecutionConfig()
	applyCoinbasePaymentConfig(&plan, cfg)

	if plan.CoinbasePaymentBPS != 0 {
		t.Fatalf("expected zero coinbase payment without WETH settlement, got %d", plan.CoinbasePaymentBPS)
	}
	if plan.WrappedNativeToken != (common.Address{}) {
		t.Fatalf("expected wrapped native token to remain unset, got %s", plan.WrappedNativeToken.Hex())
	}
}

func TestExecutionPublisherDerivesMissingV3Approvals(t *testing.T) {
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	weth := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	router := common.HexToAddress("0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45")
	data, err := domaincontract.PackExactInputSingle(domaincontract.ExactInputSingleParams{
		TokenIn:          usdc,
		TokenOut:         weth,
		Fee:              big.NewInt(500),
		Recipient:        common.HexToAddress("0x1"),
		AmountIn:         big.NewInt(0),
		AmountOutMinimum: big.NewInt(0),
	})
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	executor := &fakeExecutionExecutor{}
	cfg := testExecutionConfig()
	cfg.WrappedNativeToken = weth
	publisher := NewExecutionPublisher(
		cfg,
		fakeExecutionBuilder{plan: domaincontract.ExecutionPlan{
			Loan: domaincontract.FlashLoan{
				Protocol: domaincontract.FlashLoanProtocolBalancer,
				Lender:   common.HexToAddress("0xBA12222222228d8Ba445958a75a0704d566BF2C8"),
				Token:    weth,
				Amount:   big.NewInt(100),
			},
			Routes: []domaincontract.SwapRoute{{
				RouterAddress: router,
				Data:          data,
				FillSource:    domaincontract.FillSourceERC20Balance,
				FillToken:     usdc,
				PatchAmount:   true,
				FillOffset:    domaincontract.ExactInputSingleAmountInOffset,
			}},
			ProfitToken: weth,
			MinProfit:   big.NewInt(1),
		}},
		executor,
		nil,
	)
	if err := publisher.Publish(context.Background(), testOpportunity()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(executor.approvalReq.Approvals) != 1 {
		t.Fatalf("expected derived approval, got %+v", executor.approvalReq.Approvals)
	}
	if executor.approvalReq.Approvals[0].Token != usdc || executor.approvalReq.Approvals[0].Spender != router {
		t.Fatalf("unexpected derived approval %+v", executor.approvalReq.Approvals[0])
	}
}

func testExecutionConfig() ExecutionConfig {
	return ExecutionConfig{
		Enabled:             true,
		RPCURL:              "http://127.0.0.1:8545",
		PrivateKey:          "0xabc",
		Executor:            common.HexToAddress("0x00000000000000000000000000000000000000aa"),
		FlashbotsRPCURL:     "https://relay.flashbots.net",
		FlashbotsPaymentBPS: 8000,
		WrappedNativeToken:  common.HexToAddress("0x00000000000000000000000000000000000000cc"),
		GasLimit:            100,
		SkipEstimate:        true,
	}
}

func testOpportunity() *domainarb.Opportunity {
	return &domainarb.Opportunity{
		ID:        "opp-1",
		Route:     quoteunified.NewDirectV3Route(common.HexToAddress("0x00000000000000000000000000000000000000bb"), common.HexToAddress("0x00000000000000000000000000000000000000cc"), common.HexToAddress("0x00000000000000000000000000000000000000dd")),
		AmountIn:  big.NewInt(100),
		AmountOut: big.NewInt(1_100),
		NetProfit: big.NewInt(100_000),
		Status:    domainarb.OpportunityStatusAccepted,
		CreatedAt: time.Unix(0, 0),
	}
}

type fakeExecutionBuilder struct {
	plan      domaincontract.ExecutionPlan
	approvals []domaincontract.TokenApproval
}

func (b fakeExecutionBuilder) BuildExecutionPlan(context.Context, *domainarb.Opportunity) (domaincontract.ExecutionPlan, []domaincontract.TokenApproval, error) {
	if len(b.plan.Routes) > 0 || b.plan.ProfitToken != (common.Address{}) {
		return b.plan, append([]domaincontract.TokenApproval(nil), b.approvals...), nil
	}
	token := common.HexToAddress("0x00000000000000000000000000000000000000cc")
	router := common.HexToAddress("0x00000000000000000000000000000000000000dd")
	return domaincontract.ExecutionPlan{
			Loan: domaincontract.FlashLoan{
				Protocol: domaincontract.FlashLoanProtocolBalancer,
				Lender:   common.HexToAddress("0x00000000000000000000000000000000000000ee"),
				Token:    token,
				Amount:   big.NewInt(100),
			},
			Routes: []domaincontract.SwapRoute{{
				RouterAddress: router,
				Data:          []byte{0x12, 0x34},
			}},
			ProfitToken: token,
			MinProfit:   big.NewInt(1),
		},
		[]domaincontract.TokenApproval{{
			Token:   token,
			Spender: router,
			Amount:  big.NewInt(100),
		}}, nil
}

type fakeExecutionExecutor struct {
	approvalResp  domaincontract.EnsureApprovalsResponse
	approvalCalls int
	approvalReq   domaincontract.EnsureApprovalsRequest
	simulateCalls int
	executeCalls  int
	executeReq    domaincontract.BroadcastRequest
	executeErr    error
}

func (f *fakeExecutionExecutor) EnsureApprovals(_ context.Context, req domaincontract.EnsureApprovalsRequest) (domaincontract.EnsureApprovalsResponse, error) {
	f.approvalCalls++
	f.approvalReq = req
	return f.approvalResp, nil
}

func (f *fakeExecutionExecutor) Simulate(context.Context, domaincontract.BroadcastRequest) error {
	f.simulateCalls++
	return nil
}

func (f *fakeExecutionExecutor) Execute(_ context.Context, req domaincontract.BroadcastRequest) (domaincontract.BroadcastResponse, error) {
	f.executeCalls++
	f.executeReq = req
	if f.executeErr != nil {
		return domaincontract.BroadcastResponse{}, f.executeErr
	}
	return domaincontract.BroadcastResponse{TxHash: common.HexToHash("0x3")}, nil
}
