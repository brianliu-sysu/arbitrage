package chainruntime

import (
	"math/big"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	domainasset "github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	"github.com/ethereum/go-ethereum/common"
)

func TestNewProtocolServicesSkipsDisabledProtocols(t *testing.T) {
	services, err := newProtocolServices(config.ChainConfig{}, nil, nil, protocolResources{})
	if err != nil {
		t.Fatalf("build disabled protocols: %v", err)
	}
	if len(services.modules) != 0 {
		t.Fatalf("expected no disabled protocol modules, got %d", len(services.modules))
	}
}

func TestNewProtocolResourcesSkipsDisabledProtocols(t *testing.T) {
	services, headLogFetcher, err := newProtocolResources(config.ChainConfig{}, nil)
	if err != nil {
		t.Fatalf("build disabled protocol infrastructure: %v", err)
	}
	if headLogFetcher != nil ||
		services.univ3() != nil ||
		services.pancakeV3() != nil ||
		services.quickSwapV3() != nil ||
		services.univ4() != nil ||
		services.balancer() != nil {
		t.Fatal("expected disabled protocol infrastructure to remain nil")
	}
}

func TestLivePlanConfigEnablesCoinbasePaymentAndWETHSettlement(t *testing.T) {
	cfg := config.ChainConfig{
		Arbitrage: config.ArbitrageConfig{
			Execution: config.ExecutionConfig{
				FlashbotsRPCURL:       "https://relay.flashbots.net",
				FlashbotsPaymentBPS:   8_000,
				SettlementSlippageBPS: 50,
			},
		},
	}

	got := livePlanConfigFromRuntime(cfg)

	if !got.RequireWETHProfit {
		t.Fatal("expected WETH settlement when coinbase payment is enabled")
	}
	if got.CoinbasePaymentBPS != 8_000 {
		t.Fatalf("expected coinbase payment 8000 bps, got %d", got.CoinbasePaymentBPS)
	}
	if got.SettlementSlippageBPS != 50 {
		t.Fatalf("expected settlement slippage 50 bps, got %d", got.SettlementSlippageBPS)
	}
}

func TestFlashbotsCoinbasePaymentDisabledWithoutRelay(t *testing.T) {
	got := flashbotsCoinbasePaymentBPS(config.ExecutionConfig{FlashbotsPaymentBPS: 8_000})
	if got != 0 {
		t.Fatalf("expected zero payment without relay, got %d", got)
	}
}

func TestResolveTokenStrategiesUsesEachTokenDecimals(t *testing.T) {
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	weth := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	configs := []config.ArbitrageTokenConfig{
		{Address: usdc.Hex(), MinAmount: "1", MaxAmount: "100", MinNetProfit: "0.5"},
		{Address: weth.Hex(), MinAmount: "0.1", MaxAmount: "2", MinNetProfit: "0.01"},
	}
	metadata := map[common.Address]*domainasset.Token{
		usdc: {Address: usdc, Decimal: 6},
		weth: {Address: weth, Decimal: 18},
	}

	got, err := resolveTokenStrategies(configs, metadata, true)
	if err != nil {
		t.Fatalf("resolve strategies: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 strategies, got %d", len(got))
	}
	if got[0].MinAmount.Cmp(big.NewInt(1_000_000)) != 0 ||
		got[0].MinNetProfit.Cmp(big.NewInt(500_000)) != 0 {
		t.Fatalf("unexpected USDC limits: %+v", got[0])
	}
	wantWETHMin := new(big.Int)
	wantWETHMin.SetString("100000000000000000", 10)
	if got[1].MinAmount.Cmp(wantWETHMin) != 0 {
		t.Fatalf("unexpected WETH minimum: %s", got[1].MinAmount)
	}
}
