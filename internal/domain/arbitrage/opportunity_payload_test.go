package arbitrage

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

func TestOpportunityEnsurePayloadIncludesWrapHop(t *testing.T) {
	weth := asset.MainnetWETH
	native := common.Address{}
	route := quoteunified.Route{
		TokenIn:  weth,
		TokenOut: weth,
		Hops: []quoteunified.RouteHop{
			{Version: quoteunified.PoolVersionUnwrapWETH, TokenIn: weth, TokenOut: native},
			{Version: quoteunified.PoolVersionV4, PoolV4: testPoolID(1), TokenIn: native, TokenOut: testToken(2)},
			{Version: quoteunified.PoolVersionWrapWETH, TokenIn: native, TokenOut: weth},
		},
	}
	opportunity := NewOpportunity(
		"opp-wrap",
		NewTriangleStrategy("tri", weth, big.NewInt(1)),
		42,
		route,
		EvaluationResult{
			AmountIn:    big.NewInt(1_000),
			AmountOut:   big.NewInt(1_100),
			GrossProfit: big.NewInt(100),
			FlashLoan: FlashLoanQuote{
				Protocol: FlashLoanProtocolBalancer,
				Amount:   big.NewInt(1_000),
				Fee:      big.NewInt(0),
				FeePPM:   big.NewInt(0),
			},
			NetProfit:  big.NewInt(80),
			Profitable: true,
			Accepted:   true,
		},
		GasEstimate{CostWei: big.NewInt(20)},
		time.Unix(0, 0).UTC(),
	)
	if len(opportunity.Payload) == 0 {
		t.Fatal("expected encoded payload")
	}

	var payload opportunityPayload
	if err := json.Unmarshal(opportunity.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.Route.Hops) != 3 {
		t.Fatalf("expected 3 hops in payload, got %d", len(payload.Route.Hops))
	}
	if payload.Route.Hops[0].Version != quoteunified.PoolVersionUnwrapWETH.String() {
		t.Fatalf("expected unwrap hop, got %s", payload.Route.Hops[0].Version)
	}
	if payload.FlashLoan == nil || payload.FlashLoan.Protocol != string(FlashLoanProtocolBalancer) {
		t.Fatalf("expected balancer flash loan payload, got %+v", payload.FlashLoan)
	}
}

func TestOpportunityApplyPayloadRestoresFields(t *testing.T) {
	original := NewOpportunity(
		"opp-1",
		NewCycleStrategy("cycle-a", testToken(1), 2, big.NewInt(1)),
		100,
		quoteunified.NewDirectV3Route(testToken(9), testToken(1), testToken(2)),
		EvaluationResult{
			AmountIn:    big.NewInt(1_000),
			AmountOut:   big.NewInt(1_100),
			GrossProfit: big.NewInt(100),
			FlashLoan: FlashLoanQuote{
				Protocol: FlashLoanProtocolUniv3,
				PoolRef:  PoolRefFromV3(testToken(9)),
				Amount:   big.NewInt(1_000),
				Fee:      big.NewInt(1),
				FeePPM:   big.NewInt(500),
			},
			NetProfit:  big.NewInt(80),
			Profitable: true,
			Accepted:   true,
		},
		GasEstimate{CostWei: big.NewInt(20)},
		time.Unix(0, 0).UTC(),
	)

	loaded := &Opportunity{
		ID:          original.ID,
		PoolAddress: original.PoolAddress,
		BlockNumber: original.BlockNumber,
		Payload:     append([]byte(nil), original.Payload...),
		CreatedAt:   original.CreatedAt,
	}
	if err := loaded.ApplyPayload(); err != nil {
		t.Fatalf("apply payload: %v", err)
	}
	if loaded.NetProfit.Cmp(original.NetProfit) != 0 {
		t.Fatalf("expected net profit %s, got %s", original.NetProfit, loaded.NetProfit)
	}
	if loaded.Route.Len() != original.Route.Len() {
		t.Fatalf("expected %d hops, got %d", original.Route.Len(), loaded.Route.Len())
	}
	if loaded.FlashLoan.Protocol != FlashLoanProtocolUniv3 || loaded.FlashLoan.Fee.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected restored univ3 flash loan, got %+v", loaded.FlashLoan)
	}
	if loaded.FlashLoan.PoolRef.Key() != PoolRefFromV3(testToken(9)).Key() {
		t.Fatalf("expected restored v3 flash loan pool, got %s", loaded.FlashLoan.PoolRef.Key())
	}
}
