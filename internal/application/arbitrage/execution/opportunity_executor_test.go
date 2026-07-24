package execution

import (
	"context"
	"math/big"
	"strings"
	"testing"
	"time"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum/common"
)

type fakeOpportunityRepo struct {
	item *domainarb.Opportunity
}

func (r fakeOpportunityRepo) Save(context.Context, *domainarb.Opportunity) error { return nil }
func (r fakeOpportunityRepo) Delete(context.Context, string) error               { return nil }

func (r fakeOpportunityRepo) Get(context.Context, string) (*domainarb.Opportunity, error) {
	if r.item == nil {
		return nil, domainarb.ErrOpportunityNotFound
	}
	item := *r.item
	item.Payload = append([]byte(nil), r.item.Payload...)
	return &item, nil
}

func (r fakeOpportunityRepo) List(context.Context, int) ([]*domainarb.Opportunity, error) {
	return nil, nil
}

type fakeExecutionHead struct {
	number uint64
}

func (h fakeExecutionHead) GetLatestBlockHeader(context.Context) (domainchain.BlockHeader, error) {
	return domainchain.BlockHeader{Number: h.number}, nil
}

func TestOpportunityExecutorRequiresConfirm(t *testing.T) {
	executor := NewOpportunityExecutor(
		fakeOpportunityRepo{item: testExecutableOpportunity()},
		fakeExecutionBuilder{},
		&fakeExecutionExecutor{},
		fakeExecutionHead{number: 101},
		testSecureExecutionConfig(),
		nil,
	)

	_, err := executor.Execute(context.Background(), OpportunityExecuteRequest{OpportunityID: "opp-1"})
	if err == nil || !strings.Contains(err.Error(), "confirm=true") {
		t.Fatalf("expected confirm error, got %v", err)
	}
}

func TestOpportunityExecutorRejectsStaleOpportunity(t *testing.T) {
	executor := NewOpportunityExecutor(
		fakeOpportunityRepo{item: testExecutableOpportunity()},
		fakeExecutionBuilder{},
		&fakeExecutionExecutor{},
		fakeExecutionHead{number: 110},
		testSecureExecutionConfig(),
		nil,
	)

	_, err := executor.Execute(context.Background(), OpportunityExecuteRequest{OpportunityID: "opp-1", Confirm: true})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale error, got %v", err)
	}
}

func TestOpportunityExecutorRejectsDisallowedRouter(t *testing.T) {
	cfg := testSecureExecutionConfig()
	cfg.AllowedRouters = []common.Address{common.HexToAddress("0x0000000000000000000000000000000000000999")}
	executor := NewOpportunityExecutor(
		fakeOpportunityRepo{item: testExecutableOpportunity()},
		fakeExecutionBuilder{},
		&fakeExecutionExecutor{},
		fakeExecutionHead{number: 101},
		cfg,
		nil,
	)

	_, err := executor.Execute(context.Background(), OpportunityExecuteRequest{OpportunityID: "opp-1", Confirm: true})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected whitelist error, got %v", err)
	}
}

func TestOpportunityExecutorAcceptsLegacyDiscoveredStatus(t *testing.T) {
	opp := testExecutableOpportunity()
	opp.Status = domainarb.OpportunityStatusDiscovered
	contractExecutor := &fakeExecutionExecutor{}
	executor := NewOpportunityExecutor(
		fakeOpportunityRepo{item: opp},
		fakeExecutionBuilder{},
		contractExecutor,
		fakeExecutionHead{number: 101},
		testSecureExecutionConfig(),
		nil,
	)

	result, err := executor.Execute(context.Background(), OpportunityExecuteRequest{OpportunityID: "opp-1", Confirm: true})
	if err != nil {
		t.Fatalf("execute legacy discovered opportunity: %v", err)
	}
	if result.TxHash == (common.Hash{}) {
		t.Fatal("expected broadcast tx hash")
	}
	if contractExecutor.executeCalls != 1 {
		t.Fatalf("expected one broadcast, got %d", contractExecutor.executeCalls)
	}
	if contractExecutor.approvalReq.SubmitRPCURL != "" {
		t.Fatalf("approval submit rpc should be empty, got %q", contractExecutor.approvalReq.SubmitRPCURL)
	}
	if contractExecutor.executeReq.SubmitRPCURL != testSecureExecutionConfig().FlashbotsRPCURL {
		t.Fatalf("unexpected execution submit rpc %q", contractExecutor.executeReq.SubmitRPCURL)
	}
}

func TestOpportunityExecutorPreventsDuplicateBroadcast(t *testing.T) {
	contractExecutor := &fakeExecutionExecutor{}
	executor := NewOpportunityExecutor(
		fakeOpportunityRepo{item: testExecutableOpportunity()},
		fakeExecutionBuilder{},
		contractExecutor,
		fakeExecutionHead{number: 101},
		testSecureExecutionConfig(),
		nil,
	)

	if _, err := executor.Execute(context.Background(), OpportunityExecuteRequest{OpportunityID: "opp-1", Confirm: true}); err != nil {
		t.Fatalf("first execute: %v", err)
	}
	_, err := executor.Execute(context.Background(), OpportunityExecuteRequest{OpportunityID: "opp-1", Confirm: true})
	if err == nil || !strings.Contains(err.Error(), "already executed") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	if contractExecutor.executeCalls != 1 {
		t.Fatalf("expected one broadcast, got %d", contractExecutor.executeCalls)
	}
}

func testExecutableOpportunity() *domainarb.Opportunity {
	return &domainarb.Opportunity{
		ID:          "opp-1",
		Status:      domainarb.OpportunityStatusAccepted,
		BlockNumber: 100,
		NetProfit:   big.NewInt(100_000),
		CreatedAt:   time.Unix(0, 0),
	}
}

func testSecureExecutionConfig() ExecutionConfig {
	cfg := testExecutionConfig()
	cfg.MaxOpportunityAge = 3
	return cfg
}
