package arbitrage

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

func testToken(index byte) common.Address {
	return common.HexToAddress(fmt.Sprintf("0x000000000000000000000000000000000000000%x", index))
}

func testPoolID(index byte) marketv4.PoolID {
	return marketv4.PoolID(common.HexToHash(fmt.Sprintf("0x%064x", index)))
}

func TestEvaluatorComputesNetProfit(t *testing.T) {
	strategy := NewCycleStrategy("cycle-usdc", testToken(1), 3, big.NewInt(10))
	evaluator := NewEvaluator()

	result := evaluator.Evaluate(EvaluationInput{
		Strategy:    strategy,
		BlockNumber: 100,
		Route:       quoteunified.NewDirectV3Route(testToken(9), testToken(1), testToken(2)),
		AmountIn:    big.NewInt(1_000_000),
		AmountOut:   big.NewInt(1_000_050),
		GasCost:     big.NewInt(20),
	})

	if result.GrossProfit.Cmp(big.NewInt(50)) != 0 {
		t.Fatalf("expected gross profit 50, got %s", result.GrossProfit)
	}
	if result.NetProfit.Cmp(big.NewInt(30)) != 0 {
		t.Fatalf("expected net profit 30, got %s", result.NetProfit)
	}
	if !result.Profitable || !result.Accepted {
		t.Fatal("expected profitable accepted result")
	}
}

func TestEvaluatorRejectsBelowMinimumProfit(t *testing.T) {
	strategy := NewCycleStrategy("cycle-usdc", testToken(1), 3, big.NewInt(100))
	evaluator := NewEvaluator()

	result := evaluator.Evaluate(EvaluationInput{
		Strategy:  strategy,
		AmountIn:  big.NewInt(1_000_000),
		AmountOut: big.NewInt(1_000_050),
		GasCost:   big.NewInt(20),
	})

	if !result.Profitable {
		t.Fatal("expected profitable result before minimum threshold")
	}
	if result.Accepted {
		t.Fatal("expected rejected result below minimum net profit")
	}
}

func TestEvaluatorSubtractsFlashLoanFee(t *testing.T) {
	strategy := NewCycleStrategy("cycle-usdc", testToken(1), 3, big.NewInt(1))
	evaluator := NewEvaluator()

	result := evaluator.Evaluate(EvaluationInput{
		Strategy:  strategy,
		AmountIn:  big.NewInt(1_000_000),
		AmountOut: big.NewInt(1_000_100),
		GasCost:   big.NewInt(20),
		FlashLoan: FlashLoanQuote{
			Protocol: FlashLoanProtocolUniv3,
			Fee:      big.NewInt(30),
			FeePPM:   big.NewInt(30),
		},
	})

	if result.NetProfit.Cmp(big.NewInt(50)) != 0 {
		t.Fatalf("expected net profit after flash fee 50, got %s", result.NetProfit)
	}
	if result.FlashLoan.Protocol != FlashLoanProtocolUniv3 {
		t.Fatalf("expected univ3 flash loan, got %s", result.FlashLoan.Protocol)
	}
}

func TestSelectBestFlashLoanChoosesLowestFee(t *testing.T) {
	quote, err := SelectBestFlashLoan(big.NewInt(1_000_001), []FlashLoanOption{
		{Protocol: FlashLoanProtocolUniv3, FeePPM: big.NewInt(500)},
		{Protocol: FlashLoanProtocolBalancer, FeePPM: big.NewInt(10)},
	})
	if err != nil {
		t.Fatalf("select flash loan: %v", err)
	}
	if quote.Protocol != FlashLoanProtocolBalancer {
		t.Fatalf("expected balancer flash loan, got %s", quote.Protocol)
	}
	if quote.Fee.Cmp(big.NewInt(11)) != 0 {
		t.Fatalf("expected rounded-up fee 11, got %s", quote.Fee)
	}
}

func TestFlashLoanOptionsForRouteUsesV3PoolFee(t *testing.T) {
	tokenA := testToken(1)
	tokenB := testToken(2)
	poolAddress := testToken(9)
	pool := marketv3.NewPool(poolAddress, tokenA, tokenB, 3000, 60)
	route := quoteunified.NewDirectV3Route(poolAddress, tokenA, tokenB)

	options := FlashLoanOptionsForRoute(route, quoteunified.RoutePools{
		V3: map[common.Address]*marketv3.Pool{poolAddress: pool},
	}, []FlashLoanOption{
		{Protocol: FlashLoanProtocolBalancer, FeePPM: big.NewInt(10)},
	})

	var v3Option FlashLoanOption
	for _, option := range options {
		if option.Protocol == FlashLoanProtocolUniv3 {
			v3Option = option
			break
		}
	}
	if v3Option.Protocol == "" {
		t.Fatal("expected univ3 flash loan option")
	}
	if v3Option.FeePPM.Cmp(big.NewInt(3000)) != 0 {
		t.Fatalf("expected v3 pool fee 3000, got %s", v3Option.FeePPM)
	}
	if v3Option.PoolRef.Key() != PoolRefFromV3(poolAddress).Key() {
		t.Fatalf("expected v3 pool ref, got %s", v3Option.PoolRef.Key())
	}
	if !v3Option.BorrowToken0 {
		t.Fatal("expected borrowToken0=true when route.TokenIn is pool token0")
	}

	routeToken1 := quoteunified.NewDirectV3Route(poolAddress, tokenB, tokenA)
	optionsToken1 := FlashLoanOptionsForRoute(routeToken1, quoteunified.RoutePools{
		V3: map[common.Address]*marketv3.Pool{poolAddress: pool},
	}, nil)
	var v3OptionToken1 FlashLoanOption
	for _, option := range optionsToken1 {
		if option.Protocol == FlashLoanProtocolUniv3 {
			v3OptionToken1 = option
			break
		}
	}
	if v3OptionToken1.BorrowToken0 {
		t.Fatal("expected borrowToken0=false when route.TokenIn is pool token1")
	}
}

type linearQuoter struct {
	gain *big.Int
}

func (q linearQuoter) QuoteAmountOut(amountIn *big.Int) (*big.Int, error) {
	return new(big.Int).Add(amountIn, q.gain), nil
}

type thresholdQuoter struct {
	minAmount *big.Int
	gain      *big.Int
}

func (q thresholdQuoter) QuoteAmountOut(amountIn *big.Int) (*big.Int, error) {
	if amountIn.Cmp(q.minAmount) < 0 {
		return nil, fmt.Errorf("amount too small")
	}
	return new(big.Int).Add(amountIn, q.gain), nil
}

func TestOptimizerFindsBestAmount(t *testing.T) {
	optimizer := NewOptimizer(big.NewInt(1), big.NewInt(100), 10)
	result, err := optimizer.Optimize(linearQuoter{gain: big.NewInt(5)})
	if err != nil {
		t.Fatalf("optimize: %v", err)
	}
	if result.GrossProfit.Cmp(big.NewInt(5)) != 0 {
		t.Fatalf("expected gross profit 5, got %s", result.GrossProfit)
	}
	if result.AmountIn.Cmp(big.NewInt(1)) != 0 && result.AmountIn.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected boundary amount, got %s", result.AmountIn)
	}
}

func TestOptimizerSkipsFailedSampleAmounts(t *testing.T) {
	optimizer := NewOptimizer(big.NewInt(1), big.NewInt(100), 10)
	result, err := optimizer.Optimize(thresholdQuoter{
		minAmount: big.NewInt(50),
		gain:      big.NewInt(5),
	})
	if err != nil {
		t.Fatalf("optimize: %v", err)
	}
	if result.AmountIn.Cmp(big.NewInt(50)) < 0 {
		t.Fatalf("expected optimizer to skip failing samples, got amount %s", result.AmountIn)
	}
	if result.GrossProfit.Cmp(big.NewInt(5)) != 0 {
		t.Fatalf("expected gross profit 5, got %s", result.GrossProfit)
	}
}

func TestDependencyGraphAffectedRoutes(t *testing.T) {
	graph := NewDependencyGraph()
	routeA := RouteRef{
		ID: "route-a",
		Route: quoteunified.Route{
			TokenIn:  testToken(1),
			TokenOut: testToken(2),
			Hops: []quoteunified.RouteHop{
				{Version: quoteunified.PoolVersionV3, PoolV3: testToken(10), TokenIn: testToken(1), TokenOut: testToken(2)},
			},
		},
	}
	routeB := RouteRef{
		ID: "route-b",
		Route: quoteunified.Route{
			TokenIn:  testToken(2),
			TokenOut: testToken(3),
			Hops: []quoteunified.RouteHop{
				{Version: quoteunified.PoolVersionV3, PoolV3: testToken(11), TokenIn: testToken(2), TokenOut: testToken(3)},
			},
		},
	}
	graph.Register(routeA)
	graph.Register(routeB)

	affected := graph.AffectedRoutes([]common.Address{testToken(10)}, nil, nil, nil)
	if len(affected) != 1 || affected[0].ID != "route-a" {
		t.Fatalf("expected route-a only, got %+v", affected)
	}
}

func TestStaticGasEstimator(t *testing.T) {
	estimator := NewStaticGasEstimator(100_000, 80_000, big.NewInt(10))
	estimate, err := estimator.Estimate(context.Background(), 2)
	if err != nil {
		t.Fatalf("estimate gas: %v", err)
	}
	if estimate.GasLimit != 260_000 {
		t.Fatalf("expected gas limit 260000, got %d", estimate.GasLimit)
	}
	if estimate.CostWei.Cmp(big.NewInt(2_600_000)) != 0 {
		t.Fatalf("expected gas cost 2600000, got %s", estimate.CostWei)
	}
}

func TestNewOpportunity(t *testing.T) {
	strategy := NewCycleStrategy("cycle-usdc", testToken(1), 3, big.NewInt(1))
	route := quoteunified.NewDirectV3Route(testToken(9), testToken(1), testToken(2))
	evaluation := EvaluationResult{
		AmountIn:    big.NewInt(1_000),
		AmountOut:   big.NewInt(1_100),
		GrossProfit: big.NewInt(100),
		NetProfit:   big.NewInt(80),
		Profitable:  true,
		Accepted:    true,
	}
	gas := GasEstimate{CostWei: big.NewInt(20)}

	opportunity := NewOpportunity("opp-1", strategy, 42, route, evaluation, gas, time.Unix(0, 0).UTC())
	if opportunity.PoolAddress != testToken(9) {
		t.Fatalf("expected first hop pool, got %s", opportunity.PoolAddress.Hex())
	}
	if !opportunity.IsProfitable() {
		t.Fatal("expected profitable opportunity")
	}
}

func TestFindTriangleRoutes(t *testing.T) {
	tokenA := testToken(1)
	tokenB := testToken(2)
	tokenC := testToken(3)
	poolAB := testToken(10)
	poolBC := testToken(11)
	poolCA := testToken(12)

	graph := quoteunified.NewStaticPoolGraph([]quoteunified.PoolEdge{
		{Version: quoteunified.PoolVersionV3, PoolV3: poolAB, Token0: tokenA, Token1: tokenB},
		{Version: quoteunified.PoolVersionV3, PoolV3: poolBC, Token0: tokenB, Token1: tokenC},
		{Version: quoteunified.PoolVersionV3, PoolV3: poolCA, Token0: tokenC, Token1: tokenA},
	})

	routes := FindUnifiedTriangleRoutes(graph, tokenA)
	if len(routes) != 2 {
		t.Fatalf("expected 2 triangle routes, got %d", len(routes))
	}
	if !IsUnifiedTriangleRoute(routes[0]) {
		t.Fatal("expected valid triangle route")
	}
	if !MatchesStrategy(NewTriangleStrategy("tri", tokenA, big.NewInt(1)), routes[0]) {
		t.Fatal("expected route to match triangle strategy")
	}
}

func TestTriangleStrategyRejectsTwoHopCycle(t *testing.T) {
	tokenA := testToken(1)
	tokenB := testToken(2)
	route := quoteunified.Route{
		TokenIn:  tokenA,
		TokenOut: tokenA,
		Hops: []quoteunified.RouteHop{
			{Version: quoteunified.PoolVersionV3, PoolV3: testToken(10), TokenIn: tokenA, TokenOut: tokenB},
			{Version: quoteunified.PoolVersionV3, PoolV3: testToken(10), TokenIn: tokenB, TokenOut: tokenA},
		},
	}
	if MatchesStrategy(NewTriangleStrategy("tri", tokenA, big.NewInt(1)), route) {
		t.Fatal("two-hop route should not match triangle strategy")
	}
}

func TestFindUnifiedTriangleRoutesMixedPools(t *testing.T) {
	tokenA := testToken(1)
	tokenB := testToken(2)
	tokenC := testToken(3)
	poolAB := testToken(10)
	poolCA := testToken(12)
	poolBCID := testPoolID(11)

	graph := quoteunified.NewStaticPoolGraph([]quoteunified.PoolEdge{
		{Version: quoteunified.PoolVersionV3, PoolV3: poolAB, Token0: tokenA, Token1: tokenB},
		{Version: quoteunified.PoolVersionV4, PoolV4: poolBCID, Token0: tokenB, Token1: tokenC},
		{Version: quoteunified.PoolVersionV3, PoolV3: poolCA, Token0: tokenC, Token1: tokenA},
	})

	routes := FindUnifiedTriangleRoutes(graph, tokenA)
	if len(routes) != 2 {
		t.Fatalf("expected 2 mixed triangle routes, got %d", len(routes))
	}
	hasV4Hop := false
	for _, route := range routes {
		for _, hop := range route.Hops {
			if hop.Version == quoteunified.PoolVersionV4 {
				hasV4Hop = true
			}
		}
	}
	if !hasV4Hop {
		t.Fatal("expected at least one route with a v4 hop")
	}
}

func TestFindUnifiedTriangleRoutesKeepsParallelPoolVariants(t *testing.T) {
	tokenA := testToken(1)
	tokenB := testToken(2)
	tokenC := testToken(3)
	poolAB1 := testToken(10)
	poolAB2 := testToken(11)
	poolBC := testToken(12)
	poolCA := testToken(13)

	graph := quoteunified.NewStaticPoolGraph([]quoteunified.PoolEdge{
		{Version: quoteunified.PoolVersionV3, PoolV3: poolAB1, Token0: tokenA, Token1: tokenB},
		{Version: quoteunified.PoolVersionV3, PoolV3: poolAB2, Token0: tokenA, Token1: tokenB},
		{Version: quoteunified.PoolVersionV3, PoolV3: poolBC, Token0: tokenB, Token1: tokenC},
		{Version: quoteunified.PoolVersionV3, PoolV3: poolCA, Token0: tokenC, Token1: tokenA},
	})

	routes := FindUnifiedTriangleRoutes(graph, tokenA)
	if len(routes) != 4 {
		t.Fatalf("expected 4 triangle routes with parallel pools, got %d", len(routes))
	}

	seen := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		id := UnifiedTriangleRouteIDWithPools(route)
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate route id %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestDependencyGraphAffectedRoutesPancakeV3(t *testing.T) {
	poolBC := testToken(11)
	graph := NewDependencyGraph()
	route := RouteRef{
		ID: "mixed-pancake",
		Route: quoteunified.Route{
			TokenIn:  testToken(1),
			TokenOut: testToken(1),
			Hops: []quoteunified.RouteHop{
				{Version: quoteunified.PoolVersionV3, PoolV3: testToken(10), TokenIn: testToken(1), TokenOut: testToken(2)},
				{Version: quoteunified.PoolVersionPancakeV3, PoolPancakeV3: poolBC, TokenIn: testToken(2), TokenOut: testToken(3)},
				{Version: quoteunified.PoolVersionV3, PoolV3: testToken(12), TokenIn: testToken(3), TokenOut: testToken(1)},
			},
		},
	}
	graph.Register(route)

	affected := graph.AffectedRoutes(nil, []common.Address{poolBC}, nil, nil)
	if len(affected) != 1 || affected[0].ID != "mixed-pancake" {
		t.Fatalf("expected mixed-pancake route, got %+v", affected)
	}

	// Same address on univ3 must not match pancake route.
	affectedV3 := graph.AffectedRoutes([]common.Address{poolBC}, nil, nil, nil)
	if len(affectedV3) != 0 {
		t.Fatalf("expected no routes for univ3 key collision, got %+v", affectedV3)
	}
}

func TestDependencyGraphAffectedRoutesQuickSwapV3(t *testing.T) {
	poolBC := testToken(11)
	graph := NewDependencyGraph()
	route := RouteRef{
		ID: "mixed-quickswap",
		Route: quoteunified.Route{
			TokenIn:  testToken(1),
			TokenOut: testToken(1),
			Hops: []quoteunified.RouteHop{
				{Version: quoteunified.PoolVersionV3, PoolV3: testToken(10), TokenIn: testToken(1), TokenOut: testToken(2)},
				{Version: quoteunified.PoolVersionQuickSwapV3, PoolQuickSwapV3: poolBC, TokenIn: testToken(2), TokenOut: testToken(3)},
				{Version: quoteunified.PoolVersionV3, PoolV3: testToken(12), TokenIn: testToken(3), TokenOut: testToken(1)},
			},
		},
	}
	graph.Register(route)

	affected := graph.AffectedRoutes(nil, nil, []common.Address{poolBC}, nil)
	if len(affected) != 1 || affected[0].ID != "mixed-quickswap" {
		t.Fatalf("expected mixed-quickswap route, got %+v", affected)
	}

	affectedV3 := graph.AffectedRoutes([]common.Address{poolBC}, nil, nil, nil)
	if len(affectedV3) != 0 {
		t.Fatalf("expected no routes for univ3 key collision, got %+v", affectedV3)
	}
}

func TestFindUnifiedTriangleRoutesWithPancakeV3(t *testing.T) {
	tokenA := testToken(1)
	tokenB := testToken(2)
	tokenC := testToken(3)
	poolAB := testToken(10)
	poolBC := testToken(11)
	poolCA := testToken(12)

	graph := quoteunified.NewStaticPoolGraph([]quoteunified.PoolEdge{
		{Version: quoteunified.PoolVersionV3, PoolV3: poolAB, Token0: tokenA, Token1: tokenB},
		{Version: quoteunified.PoolVersionPancakeV3, PoolPancakeV3: poolBC, Token0: tokenB, Token1: tokenC},
		{Version: quoteunified.PoolVersionV3, PoolV3: poolCA, Token0: tokenC, Token1: tokenA},
	})

	routes := FindUnifiedTriangleRoutes(graph, tokenA)
	if len(routes) != 2 {
		t.Fatalf("expected 2 mixed triangle routes, got %d", len(routes))
	}
	hasPancakeHop := false
	for _, route := range routes {
		for _, hop := range route.Hops {
			if hop.Version == quoteunified.PoolVersionPancakeV3 {
				hasPancakeHop = true
			}
		}
	}
	if !hasPancakeHop {
		t.Fatal("expected at least one route with a pancakev3 hop")
	}
}

func TestDependencyGraphAffectedRoutesV4(t *testing.T) {
	poolBCID := testPoolID(11)
	graph := NewDependencyGraph()
	route := RouteRef{
		ID: "mixed-tri",
		Route: quoteunified.Route{
			TokenIn:  testToken(1),
			TokenOut: testToken(1),
			Hops: []quoteunified.RouteHop{
				{Version: quoteunified.PoolVersionV3, PoolV3: testToken(10), TokenIn: testToken(1), TokenOut: testToken(2)},
				{Version: quoteunified.PoolVersionV4, PoolV4: poolBCID, TokenIn: testToken(2), TokenOut: testToken(3)},
				{Version: quoteunified.PoolVersionV3, PoolV3: testToken(12), TokenIn: testToken(3), TokenOut: testToken(1)},
			},
		},
	}
	graph.Register(route)

	affected := graph.AffectedRoutes(nil, nil, nil, []marketv4.PoolID{poolBCID})
	if len(affected) != 1 || affected[0].ID != "mixed-tri" {
		t.Fatalf("expected mixed-tri route, got %+v", affected)
	}
}

func TestFindUnifiedTriangleRoutesWithWETHBridge(t *testing.T) {
	weth := asset.MainnetWETH
	native := common.Address{}
	usdc := testToken(2)
	poolWETHUSDC := testToken(10)
	poolETHUSDC := testPoolID(11)
	poolUSDCWETH := testToken(12)

	graph := quoteunified.NewStaticPoolGraph([]quoteunified.PoolEdge{
		{Version: quoteunified.PoolVersionV3, PoolV3: poolWETHUSDC, Token0: weth, Token1: usdc},
		{Version: quoteunified.PoolVersionV4, PoolV4: poolETHUSDC, Token0: native, Token1: usdc},
		{Version: quoteunified.PoolVersionV3, PoolV3: poolUSDCWETH, Token0: usdc, Token1: weth},
	})

	routes := FindUnifiedTriangleRoutes(graph, weth)
	if len(routes) == 0 {
		t.Fatal("expected triangle routes through WETH bridge")
	}

	hasBridge := false
	for _, route := range routes {
		for _, hop := range route.Hops {
			if quoteunified.IsWETHBridgeVersion(hop.Version) {
				hasBridge = true
			}
		}
	}
	if !hasBridge {
		t.Fatal("expected at least one route with a WETH wrap or unwrap hop")
	}
}
