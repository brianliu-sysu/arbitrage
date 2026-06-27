package subgraph

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("https://api.thegraph.com/subgraphs/name/uniswap/uniswap-v3")
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.url != "https://api.thegraph.com/subgraphs/name/uniswap/uniswap-v3" {
		t.Errorf("url = %q", c.url)
	}
	if c.http == nil {
		t.Fatal("http client should not be nil")
	}
}

func TestNewClientEmptyURL(t *testing.T) {
	c := NewClient("")
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.url != "" {
		t.Errorf("url = %q, want empty", c.url)
	}
}

func TestPoolInfoTypes(t *testing.T) {
	pi := PoolInfo{
		Address:  "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8",
		FeeTier:  "3000",
		VolumeUSD: "1000000",
		TVLUSD:   "50000000",
		Token0: TokenInfo{
			ID:       "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
			Symbol:   "USDC",
			Decimals: "6",
		},
		Token1: TokenInfo{
			ID:       "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2",
			Symbol:   "WETH",
			Decimals: "18",
		},
	}
	if pi.Address != "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8" {
		t.Errorf("Address = %q", pi.Address)
	}
	if pi.Token0.Symbol != "USDC" {
		t.Errorf("Token0.Symbol = %q", pi.Token0.Symbol)
	}
	if pi.Token1.Symbol != "WETH" {
		t.Errorf("Token1.Symbol = %q", pi.Token1.Symbol)
	}
}

func TestSubgraphURLs(t *testing.T) {
	if len(SubgraphURLs) == 0 {
		t.Fatal("SubgraphURLs should not be empty")
	}
	// Check key chains
	for _, chain := range []string{"ethereum", "arbitrum", "optimism", "polygon", "base"} {
		if url, ok := SubgraphURLs[chain]; !ok || url == "" {
			t.Errorf("missing SubgraphURL for %q", chain)
		}
	}
}
