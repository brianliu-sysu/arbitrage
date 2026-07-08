package arbitrage

import (
	"math/big"
	"testing"

	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

func TestFindUnifiedSpreadRoutesAcrossParallelPools(t *testing.T) {
	tokenA := testToken(1)
	tokenB := testToken(2)
	poolAB1 := testToken(10)
	poolAB2 := testToken(11)

	graph := quoteunified.NewStaticPoolGraph([]quoteunified.PoolEdge{
		{Version: quoteunified.PoolVersionV3, PoolV3: poolAB1, Token0: tokenA, Token1: tokenB},
		{Version: quoteunified.PoolVersionV3, PoolV3: poolAB2, Token0: tokenA, Token1: tokenB},
	})

	routes := FindUnifiedSpreadRoutes(graph, tokenA)
	if len(routes) != 2 {
		t.Fatalf("expected 2 spread routes, got %d", len(routes))
	}
	for _, route := range routes {
		if !IsUnifiedSpreadRoute(route) {
			t.Fatal("expected valid spread route")
		}
		if !MatchesStrategy(NewSpreadStrategy("spread", tokenA, big.NewInt(1)), route) {
			t.Fatal("expected route to match spread strategy")
		}
	}
}

func TestFindUnifiedSpreadRoutesMixedProtocols(t *testing.T) {
	tokenA := testToken(1)
	tokenB := testToken(2)
	poolV3 := testToken(10)
	poolV4 := testPoolID(11)

	graph := quoteunified.NewStaticPoolGraph([]quoteunified.PoolEdge{
		{Version: quoteunified.PoolVersionV3, PoolV3: poolV3, Token0: tokenA, Token1: tokenB},
		{Version: quoteunified.PoolVersionV4, PoolV4: poolV4, Token0: tokenA, Token1: tokenB},
	})

	routes := FindUnifiedSpreadRoutes(graph, tokenA)
	if len(routes) != 2 {
		t.Fatalf("expected 2 mixed spread routes, got %d", len(routes))
	}
}

func TestSpreadStrategyRejectsSamePoolRoundTrip(t *testing.T) {
	tokenA := testToken(1)
	tokenB := testToken(2)
	poolAB := testToken(10)
	route := quoteunified.Route{
		TokenIn:  tokenA,
		TokenOut: tokenA,
		Hops: []quoteunified.RouteHop{
			{Version: quoteunified.PoolVersionV3, PoolV3: poolAB, TokenIn: tokenA, TokenOut: tokenB},
			{Version: quoteunified.PoolVersionV3, PoolV3: poolAB, TokenIn: tokenB, TokenOut: tokenA},
		},
	}
	if MatchesStrategy(NewSpreadStrategy("spread", tokenA, big.NewInt(1)), route) {
		t.Fatal("same-pool round trip should not match spread strategy")
	}
}

func TestSpreadStrategyRejectsTriangleRoute(t *testing.T) {
	tokenA := testToken(1)
	tokenB := testToken(2)
	tokenC := testToken(3)
	route := quoteunified.Route{
		TokenIn:  tokenA,
		TokenOut: tokenA,
		Hops: []quoteunified.RouteHop{
			{Version: quoteunified.PoolVersionV3, PoolV3: testToken(10), TokenIn: tokenA, TokenOut: tokenB},
			{Version: quoteunified.PoolVersionV3, PoolV3: testToken(11), TokenIn: tokenB, TokenOut: tokenC},
			{Version: quoteunified.PoolVersionV3, PoolV3: testToken(12), TokenIn: tokenC, TokenOut: tokenA},
		},
	}
	if MatchesStrategy(NewSpreadStrategy("spread", tokenA, big.NewInt(1)), route) {
		t.Fatal("triangle route should not match spread strategy")
	}
}

func TestUnifiedSpreadRouteIDWithPoolsIsUnique(t *testing.T) {
	tokenA := testToken(1)
	tokenB := testToken(2)
	poolAB1 := testToken(10)
	poolAB2 := testToken(11)

	graph := quoteunified.NewStaticPoolGraph([]quoteunified.PoolEdge{
		{Version: quoteunified.PoolVersionV3, PoolV3: poolAB1, Token0: tokenA, Token1: tokenB},
		{Version: quoteunified.PoolVersionV3, PoolV3: poolAB2, Token0: tokenA, Token1: tokenB},
	})

	routes := FindUnifiedSpreadRoutes(graph, tokenA)
	seen := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		id := UnifiedSpreadRouteIDWithPools(route)
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate spread route id %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestFindUnifiedSpreadRoutesRequiresParallelPools(t *testing.T) {
	tokenA := testToken(1)
	tokenB := testToken(2)
	poolAB := testToken(10)

	graph := quoteunified.NewStaticPoolGraph([]quoteunified.PoolEdge{
		{Version: quoteunified.PoolVersionV3, PoolV3: poolAB, Token0: tokenA, Token1: tokenB},
	})

	if routes := FindUnifiedSpreadRoutes(graph, tokenA); len(routes) != 0 {
		t.Fatalf("expected no spread routes with a single pool, got %d", len(routes))
	}
}

func TestNewSpreadStrategyValidation(t *testing.T) {
	strategy := NewSpreadStrategy("spread-usdc", testToken(1), big.NewInt(1))
	if err := strategy.Validate(); err != nil {
		t.Fatalf("validate spread strategy: %v", err)
	}
	if strategy.Kind != StrategyKindSpread {
		t.Fatalf("expected spread kind, got %s", strategy.Kind)
	}
}

func TestSpreadRouteUsesDistinctPoolRefs(t *testing.T) {
	tokenA := common.HexToAddress("0x0000000000000000000000000000000000000001")
	tokenB := common.HexToAddress("0x0000000000000000000000000000000000000002")
	route := quoteunified.Route{
		TokenIn:  tokenA,
		TokenOut: tokenA,
		Hops: []quoteunified.RouteHop{
			{Version: quoteunified.PoolVersionV3, PoolV3: testToken(10), TokenIn: tokenA, TokenOut: tokenB},
			{Version: quoteunified.PoolVersionPancakeV3, PoolPancakeV3: testToken(11), TokenIn: tokenB, TokenOut: tokenA},
		},
	}
	if !IsUnifiedSpreadRoute(route) {
		t.Fatal("expected cross-protocol spread route")
	}
}
