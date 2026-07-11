package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	arbitrageapp "github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage"
	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	httpapi "github.com/brianliu-sysu/uniswapv3/internal/interfaces/http"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

type stubOpportunityRepo struct {
	items []*domainarb.Opportunity
}

func (r *stubOpportunityRepo) Save(context.Context, *domainarb.Opportunity) error { return nil }
func (r *stubOpportunityRepo) Delete(context.Context, string) error               { return nil }

func (r *stubOpportunityRepo) Get(_ context.Context, id string) (*domainarb.Opportunity, error) {
	for _, item := range r.items {
		if item != nil && item.ID == id {
			copyItem := *item
			return &copyItem, nil
		}
	}
	return nil, domainarb.ErrOpportunityNotFound
}

func (r *stubOpportunityRepo) List(_ context.Context, limit int) ([]*domainarb.Opportunity, error) {
	if limit > 0 && len(r.items) > limit {
		return r.items[:limit], nil
	}
	return r.items, nil
}

type stubContractExecutor struct {
	executeCalls int
	approveCalls int
	txHash       common.Hash
	allowance    *big.Int
	lastPlan     domaincontract.ExecutionPlan
}

func (s *stubContractExecutor) EnsureApprovals(
	context.Context,
	domaincontract.EnsureApprovalsRequest,
) (domaincontract.EnsureApprovalsResponse, error) {
	if s.allowance != nil && s.allowance.Sign() == 0 {
		s.approveCalls++
		return domaincontract.EnsureApprovalsResponse{
			TxHashes:  []common.Hash{s.txHash},
			Broadcast: true,
		}, nil
	}
	return domaincontract.EnsureApprovalsResponse{}, nil
}

func (s *stubContractExecutor) Simulate(context.Context, domaincontract.BroadcastRequest) error {
	return nil
}

func (s *stubContractExecutor) Execute(_ context.Context, req domaincontract.BroadcastRequest) (domaincontract.BroadcastResponse, error) {
	s.executeCalls++
	s.lastPlan = req.Plan
	return domaincontract.BroadcastResponse{TxHash: s.txHash}, nil
}

type stubHeadReader struct {
	number uint64
}

func (s stubHeadReader) GetLatestBlockHeader(context.Context) (domainchain.BlockHeader, error) {
	return domainchain.BlockHeader{Number: s.number}, nil
}

func TestOpportunityHandlerList(t *testing.T) {
	repo := &stubOpportunityRepo{
		items: []*domainarb.Opportunity{
			{
				ID:          "opp-1",
				StrategyID:  "triangle-0",
				Status:      domainarb.OpportunityStatusAccepted,
				PoolAddress: common.HexToAddress("0x000000000000000000000000000000000000000a"),
				BlockNumber: 42,
				NetProfit:   big.NewInt(80),
				CreatedAt:   time.Unix(0, 0).UTC(),
			},
		},
	}

	router := httpapi.NewRouter(httpapi.Handlers{
		Opportunities: httpapi.NewOpportunityHandler(repo),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/opportunities?limit=10", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Count int `json:"count"`
		Items []struct {
			ID        string `json:"id"`
			NetProfit string `json:"netProfit"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Count != 1 || len(resp.Items) != 1 {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if resp.Items[0].ID != "opp-1" || resp.Items[0].NetProfit != "80" {
		t.Fatalf("unexpected item: %#v", resp.Items[0])
	}
}

func TestOpportunityHandlerRejectsInvalidLimit(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Handlers{
		Opportunities: httpapi.NewOpportunityHandler(&stubOpportunityRepo{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/opportunities?limit=abc", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestOpportunityHandlerExecuteByID(t *testing.T) {
	token := common.HexToAddress("0x00000000000000000000000000000000000000cc")
	lender := common.HexToAddress("0x00000000000000000000000000000000000000bb")
	routerAddr := common.HexToAddress("0x00000000000000000000000000000000000000dd")
	payload, err := json.Marshal(map[string]any{
		"execution": map[string]any{
			"flashLoan": map[string]any{
				"protocol": "balancer",
				"lender":   lender.Hex(),
				"token":    token.Hex(),
				"amount":   "1000",
			},
			"routes": []map[string]any{{
				"routerAddress": routerAddr.Hex(),
				"data":          "0x1234",
			}},
			"profitToken": token.Hex(),
			"minProfit":   "1",
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	repo := &stubOpportunityRepo{
		items: []*domainarb.Opportunity{{
			ID:          "opp-exec-1",
			Status:      domainarb.OpportunityStatusAccepted,
			BlockNumber: 100,
			NetProfit:   big.NewInt(50),
			FlashLoan: domainarb.FlashLoanQuote{
				Protocol: domainarb.FlashLoanProtocolBalancer,
				Amount:   big.NewInt(2000),
			},
			Payload: payload,
		}},
	}
	contractExec := &stubContractExecutor{txHash: common.HexToHash("0xabc")}
	executor := arbitrageapp.NewOpportunityExecutor(
		repo,
		arbitrageapp.NewLiveExecutionPlanBuilder(arbitrageapp.LivePlanConfig{}, nil),
		contractExec,
		stubHeadReader{number: 101},
		arbitrageapp.ExecutionConfig{
			Enabled:           true,
			RPCURL:            "http://127.0.0.1:8545",
			PrivateKey:        "0xabc123",
			Executor:          common.HexToAddress("0x00000000000000000000000000000000000000aa"),
			BroadcastToken:    "secret",
			MaxOpportunityAge: 3,
		},
		zap.NewNop(),
	)

	handler := httpapi.NewOpportunityChainHandler(
		[]httpapi.ChainInfo{{Name: "chain-1", ChainID: 1, Primary: true}},
		map[string]domainarb.OpportunityRepository{"chain-1": repo},
		map[string]*arbitrageapp.OpportunityExecutor{"chain-1": executor},
	)
	router := httpapi.NewRouter(httpapi.Handlers{Opportunities: handler})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/opportunities/opp-exec-1/execute?chain=chain-1",
		bytes.NewReader([]byte(`{"confirm":true}`)),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OpportunityID string          `json:"opportunityId"`
		TxHash        string          `json:"txHash"`
		Execution     json.RawMessage `json:"execution"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OpportunityID != "opp-exec-1" {
		t.Fatalf("unexpected opportunity id %q", resp.OpportunityID)
	}
	if resp.TxHash != contractExec.txHash.Hex() {
		t.Fatalf("unexpected tx hash %q", resp.TxHash)
	}
	if contractExec.executeCalls != 1 {
		t.Fatalf("expected one execute call, got %d", contractExec.executeCalls)
	}
	if contractExec.lastPlan.Loan.Amount.Cmp(big.NewInt(2000)) != 0 {
		t.Fatalf("expected refreshed loan amount 2000, got %s", contractExec.lastPlan.Loan.Amount)
	}
	if len(resp.Execution) == 0 {
		t.Fatalf("expected execution payload in response")
	}
}

func TestOpportunityHandlerExecuteByIDNotFound(t *testing.T) {
	repo := &stubOpportunityRepo{}
	executor := arbitrageapp.NewOpportunityExecutor(
		repo,
		arbitrageapp.NewPayloadExecutionPlanBuilder(),
		&stubContractExecutor{txHash: common.HexToHash("0xabc")},
		stubHeadReader{number: 1},
		arbitrageapp.ExecutionConfig{
			Enabled:           true,
			RPCURL:            "http://127.0.0.1:8545",
			PrivateKey:        "0xabc123",
			Executor:          common.HexToAddress("0x00000000000000000000000000000000000000aa"),
			MaxOpportunityAge: 3,
		},
		zap.NewNop(),
	)
	handler := httpapi.NewOpportunityChainHandler(
		[]httpapi.ChainInfo{{Name: "chain-1", ChainID: 1, Primary: true}},
		map[string]domainarb.OpportunityRepository{"chain-1": repo},
		map[string]*arbitrageapp.OpportunityExecutor{"chain-1": executor},
	)
	router := httpapi.NewRouter(httpapi.Handlers{Opportunities: handler})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/opportunities/missing/execute?chain=chain-1", bytes.NewReader([]byte(`{"confirm":true}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}
