package chainruntime

import (
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
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
