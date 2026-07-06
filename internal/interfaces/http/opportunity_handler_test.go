package httpapi_test

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	httpapi "github.com/brianliu-sysu/uniswapv3/internal/interfaces/http"
	"github.com/ethereum/go-ethereum/common"
)

type stubOpportunityRepo struct {
	items []*domainarb.Opportunity
}

func (r *stubOpportunityRepo) Save(context.Context, *domainarb.Opportunity) error { return nil }
func (r *stubOpportunityRepo) Delete(context.Context, string) error               { return nil }

func (r *stubOpportunityRepo) List(_ context.Context, limit int) ([]*domainarb.Opportunity, error) {
	if limit > 0 && len(r.items) > limit {
		return r.items[:limit], nil
	}
	return r.items, nil
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
