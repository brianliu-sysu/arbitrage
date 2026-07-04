package quote

import (
	"fmt"
	"math/big"
	"testing"

	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v3"
	"github.com/ethereum/go-ethereum/common"
)

func sqrtPriceAtTick0() *big.Int {
	v, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	return v
}

func testToken(index byte) common.Address {
	return common.HexToAddress(fmt.Sprintf("0x000000000000000000000000000000000000000%x", index))
}

func setupQuotedPool(t *testing.T) (*marketv3.Pool, common.Address, common.Address) {
	t.Helper()

	token0 := testToken(2)
	token1 := testToken(3)
	pool := marketv3.NewPool(testToken(1), token0, token1, 3000, 60)
	sqrtPrice := sqrtPriceAtTick0()

	meta := marketv3.EventMeta{
		PoolAddress: pool.Address,
		BlockNumber: 1,
	}
	if err := pool.Apply(marketv3.NewInitializeEvent(meta, sqrtPrice, 0)); err != nil {
		t.Fatalf("initialize pool: %v", err)
	}

	liquidity := big.NewInt(10_000_000_000_000)
	if err := pool.Apply(marketv3.NewMintEvent(meta, common.Address{}, common.Address{}, -120, 120, liquidity, big.NewInt(1), big.NewInt(1))); err != nil {
		t.Fatalf("mint liquidity: %v", err)
	}
	return pool, token0, token1
}

func TestGetSqrtRatioAtTickRoundTrip(t *testing.T) {
	for _, tick := range []int32{-887272, -1000, 0, 1000, 887271} {
		sqrtPrice, err := GetSqrtRatioAtTick(tick)
		if err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		gotTick, err := GetTickAtSqrtRatio(sqrtPrice)
		if err != nil {
			t.Fatalf("sqrt at tick %d: %v", tick, err)
		}
		if gotTick != tick {
			t.Fatalf("tick %d round-trip got %d", tick, gotTick)
		}
	}
}

func TestQuoteExactInputWithinSingleTick(t *testing.T) {
	pool, token0, token1 := setupQuotedPool(t)
	service := NewQuoteService()

	amountIn := big.NewInt(1_000_000)
	result, err := service.QuoteExactInput(pool, token0, token1, amountIn)
	if err != nil {
		t.Fatalf("quote exact input: %v", err)
	}
	if result.AmountIn.Cmp(amountIn) != 0 {
		t.Fatalf("expected amountIn %s, got %s", amountIn, result.AmountIn)
	}
	if result.AmountOut.Sign() <= 0 {
		t.Fatalf("expected positive amountOut, got %s", result.AmountOut)
	}
	if result.FeeAmount.Sign() <= 0 {
		t.Fatalf("expected positive fee, got %s", result.FeeAmount)
	}
	if result.SqrtPriceX96.Cmp(pool.State.SqrtPriceX96) == 0 {
		t.Fatal("expected price to move after swap")
	}
}

func TestQuoteExactOutputWithinSingleTick(t *testing.T) {
	pool, token0, token1 := setupQuotedPool(t)
	service := NewQuoteService()

	amountOut := big.NewInt(900_000)
	result, err := service.QuoteExactOutput(pool, token0, token1, amountOut)
	if err != nil {
		t.Fatalf("quote exact output: %v", err)
	}
	if result.AmountOut.Cmp(amountOut) != 0 {
		t.Fatalf("expected amountOut %s, got %s", amountOut, result.AmountOut)
	}
	if result.AmountIn.Sign() <= 0 {
		t.Fatalf("expected positive amountIn, got %s", result.AmountIn)
	}
}

func TestRouteServiceFindDirectAndTwoHopRoutes(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	tokenC := testToken(4)
	poolAB := testToken(10)
	poolBC := testToken(11)

	graph := NewStaticPoolGraph([]PoolEdge{
		{PoolAddress: poolAB, Token0: tokenA, Token1: tokenB},
		{PoolAddress: poolBC, Token0: tokenB, Token1: tokenC},
	})
	service := NewRouteService(graph, 3)

	routes, err := service.FindRoutes(tokenA, tokenC)
	if err != nil {
		t.Fatalf("find routes: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if routes[0].Len() != 2 {
		t.Fatalf("expected 2 hops, got %d", routes[0].Len())
	}

	directRoutes, err := service.FindRoutes(tokenA, tokenB)
	if err != nil {
		t.Fatalf("find direct routes: %v", err)
	}
	if len(directRoutes) != 1 || directRoutes[0].Len() != 1 {
		t.Fatalf("expected single direct route, got %+v", directRoutes)
	}
}

func TestQuoteRouteTwoHop(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	tokenC := testToken(4)
	poolABAddr := testToken(10)
	poolBCAddr := testToken(11)

	poolAB, ab0, ab1 := setupQuotedPool(t)
	poolAB.Address = poolABAddr
	poolAB.Token0 = tokenA
	poolAB.Token1 = tokenB

	poolBC, bc0, bc1 := setupQuotedPool(t)
	poolBC.Address = poolBCAddr
	poolBC.Token0 = tokenB
	poolBC.Token1 = tokenC

	_ = ab0
	_ = ab1
	_ = bc0
	_ = bc1

	route := Route{
		TokenIn:  tokenA,
		TokenOut: tokenC,
		Hops: []RouteHop{
			{PoolAddress: poolABAddr, TokenIn: tokenA, TokenOut: tokenB},
			{PoolAddress: poolBCAddr, TokenIn: tokenB, TokenOut: tokenC},
		},
	}

	service := NewQuoteService()
	result, err := service.QuoteRoute(map[common.Address]*marketv3.Pool{
		poolABAddr: poolAB,
		poolBCAddr: poolBC,
	}, route, big.NewInt(1_000_000))
	if err != nil {
		t.Fatalf("quote route: %v", err)
	}
	if result.AmountOut.Sign() <= 0 {
		t.Fatalf("expected positive route output, got %s", result.AmountOut)
	}
}
