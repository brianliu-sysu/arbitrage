package unified_test

import (
	"math/big"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

func TestQuoteWETHBridgeUnwrap(t *testing.T) {
	weth := asset.MainnetWETH
	native := common.Address{}
	hop := quoteunified.RouteHop{
		Version:  quoteunified.PoolVersionUnwrapWETH,
		TokenIn:  weth,
		TokenOut: native,
	}
	amountIn := big.NewInt(1_000_000_000_000_000_000)

	result, err := quoteunified.QuoteWETHBridge(hop, amountIn)
	if err != nil {
		t.Fatalf("quote unwrap: %v", err)
	}
	if result.AmountOut.Cmp(amountIn) != 0 {
		t.Fatalf("expected 1:1 amount out, got %s", result.AmountOut)
	}
	if result.FeeAmount.Sign() != 0 {
		t.Fatalf("expected zero fee, got %s", result.FeeAmount)
	}
}

func TestQuoteRouteWithWETHBridge(t *testing.T) {
	weth := asset.MainnetWETH
	native := common.Address{}
	route := quoteunified.Route{
		TokenIn:  weth,
		TokenOut: native,
		Hops: []quoteunified.RouteHop{{
			Version:  quoteunified.PoolVersionUnwrapWETH,
			TokenIn:  weth,
			TokenOut: native,
		}},
	}
	quotes := quoteunified.NewQuoteService(nil, nil, nil)
	amountIn := big.NewInt(5_000_000_000_000_000_000)

	result, err := quotes.QuoteRoute(quoteunified.RoutePools{}, route, amountIn)
	if err != nil {
		t.Fatalf("quote route: %v", err)
	}
	if result.AmountOut.Cmp(amountIn) != 0 {
		t.Fatalf("expected 1:1 amount out, got %s", result.AmountOut)
	}
}

func TestFindRoutesWETHToNativeETH(t *testing.T) {
	weth := asset.MainnetWETH
	native := common.Address{}
	graph := quoteunified.NewStaticPoolGraph(nil)

	rs := quoteunified.NewRouteService(graph, 1)
	routes, err := rs.FindRoutes(weth, native)
	if err != nil {
		t.Fatalf("find routes: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 unwrap route, got %d", len(routes))
	}
	if routes[0].Hops[0].Version != quoteunified.PoolVersionUnwrapWETH {
		t.Fatalf("expected unwrap hop, got %s", routes[0].Hops[0].Version)
	}
}
