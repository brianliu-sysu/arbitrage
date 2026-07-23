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
			QuoteSteps: []OpportunityQuoteStep{
				{
					Index:     0,
					Version:   quoteunified.PoolVersionUnwrapWETH.String(),
					TokenIn:   weth,
					TokenOut:  native,
					AmountIn:  big.NewInt(1_000),
					AmountOut: big.NewInt(1_000),
					FeeAmount: big.NewInt(0),
				},
				{
					Index:     1,
					Version:   quoteunified.PoolVersionV4.String(),
					TokenIn:   native,
					TokenOut:  testToken(2),
					AmountIn:  big.NewInt(1_000),
					AmountOut: big.NewInt(1_100),
					FeeAmount: big.NewInt(5),
				},
			},
			NetProfit:         big.NewInt(80),
			BuilderPaymentWei: big.NewInt(15),
			Profitable:        true,
			Accepted:          true,
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
	if len(payload.QuoteSteps) != 2 {
		t.Fatalf("expected 2 quote steps, got %d", len(payload.QuoteSteps))
	}
	if payload.QuoteSteps[1].AmountIn != "1000" || payload.QuoteSteps[1].AmountOut != "1100" {
		t.Fatalf("unexpected quote step payload: %+v", payload.QuoteSteps[1])
	}
}

func TestOpportunitySetStatusSyncsPayload(t *testing.T) {
	opportunity := NewOpportunity(
		"opp-status",
		NewTriangleStrategy("tri", testToken(1), big.NewInt(1)),
		42,
		quoteunified.NewDirectV3Route(testToken(9), testToken(1), testToken(2)),
		EvaluationResult{
			AmountIn:    big.NewInt(1_000),
			AmountOut:   big.NewInt(1_100),
			GrossProfit: big.NewInt(100),
			NetProfit:   big.NewInt(80),
			Profitable:  true,
			Accepted:    true,
		},
		GasEstimate{CostWei: big.NewInt(20)},
		time.Unix(0, 0).UTC(),
	)

	// Simulate an embedded execution blob that must survive status updates.
	var payload map[string]any
	if err := json.Unmarshal(opportunity.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	payload["execution"] = map[string]any{"profitToken": testToken(1).Hex()}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	opportunity.Payload = raw

	if err := opportunity.SetStatus(OpportunityStatusAccepted); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if opportunity.Status != OpportunityStatusAccepted {
		t.Fatalf("expected accepted status, got %q", opportunity.Status)
	}

	var updated map[string]any
	if err := json.Unmarshal(opportunity.Payload, &updated); err != nil {
		t.Fatalf("unmarshal updated payload: %v", err)
	}
	if updated["status"] != string(OpportunityStatusAccepted) {
		t.Fatalf("expected payload status accepted, got %#v", updated["status"])
	}
	if _, ok := updated["execution"]; !ok {
		t.Fatal("expected execution key to be preserved")
	}

	loaded := &Opportunity{
		ID:          opportunity.ID,
		PoolAddress: opportunity.PoolAddress,
		BlockNumber: opportunity.BlockNumber,
		Payload:     append([]byte(nil), opportunity.Payload...),
		CreatedAt:   opportunity.CreatedAt,
	}
	if err := loaded.ApplyPayload(); err != nil {
		t.Fatalf("apply payload: %v", err)
	}
	if loaded.Status != OpportunityStatusAccepted {
		t.Fatalf("expected loaded status accepted, got %q", loaded.Status)
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
				Protocol:     FlashLoanProtocolUniv3,
				PoolRef:      PoolRefFromV3(testToken(9)),
				Amount:       big.NewInt(1_000),
				Fee:          big.NewInt(1),
				FeePPM:       big.NewInt(500),
				BorrowToken0: true,
			},
			QuoteSteps: []OpportunityQuoteStep{
				{
					Index:     0,
					Version:   quoteunified.PoolVersionV3.String(),
					TokenIn:   testToken(1),
					TokenOut:  testToken(2),
					AmountIn:  big.NewInt(1_000),
					AmountOut: big.NewInt(1_100),
					FeeAmount: big.NewInt(3),
				},
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
	if loaded.BuilderPaymentWei.Cmp(original.BuilderPaymentWei) != 0 {
		t.Fatalf("expected builder payment %s, got %s", original.BuilderPaymentWei, loaded.BuilderPaymentWei)
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
	if !loaded.FlashLoan.BorrowToken0 {
		t.Fatal("expected restored borrowToken0")
	}
	if len(loaded.QuoteSteps) != 1 || loaded.QuoteSteps[0].AmountOut.Cmp(big.NewInt(1_100)) != 0 {
		t.Fatalf("expected restored quote step, got %+v", loaded.QuoteSteps)
	}
}
