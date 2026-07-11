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

func TestExecutionPublisherPaysFlashbotsViaGasPrice(t *testing.T) {
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
	if executor.simulateCalls != 1 {
		t.Fatalf("expected simulate call, got %d", executor.simulateCalls)
	}
	if executor.executeReq.GasPriceWei.Cmp(big.NewInt(800)) != 0 {
		t.Fatalf("expected flashbots gas price 800, got %s", executor.executeReq.GasPriceWei)
	}
	if executor.executeReq.SubmitRPCURL != "https://relay.flashbots.net" {
		t.Fatalf("unexpected submit rpc %q", executor.executeReq.SubmitRPCURL)
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

type fakeExecutionBuilder struct{}

func (fakeExecutionBuilder) BuildExecutionPlan(context.Context, *domainarb.Opportunity) (domaincontract.ExecutionPlan, []domaincontract.TokenApproval, error) {
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
	simulateCalls int
	executeCalls  int
	executeReq    domaincontract.BroadcastRequest
}

func (f *fakeExecutionExecutor) EnsureApprovals(context.Context, domaincontract.EnsureApprovalsRequest) (domaincontract.EnsureApprovalsResponse, error) {
	f.approvalCalls++
	return f.approvalResp, nil
}

func (f *fakeExecutionExecutor) Simulate(context.Context, domaincontract.BroadcastRequest) error {
	f.simulateCalls++
	return nil
}

func (f *fakeExecutionExecutor) Execute(_ context.Context, req domaincontract.BroadcastRequest) (domaincontract.BroadcastResponse, error) {
	f.executeCalls++
	f.executeReq = req
	return domaincontract.BroadcastResponse{TxHash: common.HexToHash("0x3")}, nil
}
