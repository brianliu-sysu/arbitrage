package registry

import (
	"strings"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
)

func TestQuickSwapSubgraphRegistryBuildDailySnapshotQuery(t *testing.T) {
	registry := NewQuickSwapSubgraphRegistry(config.SubgraphPoolConfig{
		Enabled:                true,
		Endpoint:               "http://example.com",
		OrderBy:                "volume24h",
		OrderDirection:         "desc",
		MinTotalValueLockedUSD: "1000000",
		MinVolume24hUSD:        "200000",
		FeeTiers:               []uint32{500, 2500},
	})

	query, variables, mode, err := registry.buildQuery()
	if err != nil {
		t.Fatalf("build query: %v", err)
	}
	if mode != quickQueryModeDailySnapshot {
		t.Fatalf("expected daily snapshot query mode, got %d", mode)
	}
	if !strings.Contains(query, "liquidityPoolDailySnapshots") || !strings.Contains(query, "LiquidityPoolDailySnapshot_orderBy") {
		t.Fatalf("expected daily snapshot query, got %q", query)
	}
	if variables["orderBy"] != "dailyVolumeUSD" {
		t.Fatalf("expected dailyVolumeUSD order, got %#v", variables["orderBy"])
	}

	where, ok := variables["where"].(map[string]any)
	if !ok {
		t.Fatalf("expected where variables, got %#v", variables["where"])
	}
	if where["dailyVolumeUSD_gt"] != "200000" {
		t.Fatalf("expected volume filter, got %#v", where["dailyVolumeUSD_gt"])
	}
	poolWhere, ok := where["pool_"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested pool filter, got %#v", where["pool_"])
	}
	if poolWhere["totalValueLockedUSD_gt"] != "1000000" {
		t.Fatalf("expected tvl filter, got %#v", poolWhere["totalValueLockedUSD_gt"])
	}
	if _, ok := poolWhere["fees_"]; ok {
		t.Fatal("expected fee tiers to be filtered client-side, not in graphql where")
	}
}

func TestQuickSwapPoolMatchesFeeTiers(t *testing.T) {
	pool := quickSubgraphPool{
		ID: "0x1",
		Fees: []quickSubgraphFee{
			{FeeType: "FIXED_PROTOCOL_FEE", FeePercentage: "0.0033"},
			{FeeType: "FIXED_TRADING_FEE", FeePercentage: "0.05"},
		},
	}
	if !quickPoolMatchesFeeTiers(pool, []uint32{500}) {
		t.Fatal("expected 0.05% trading fee to match tier 500")
	}
	if quickPoolMatchesFeeTiers(pool, []uint32{2500}) {
		t.Fatal("expected tier 2500 to not match 0.05% trading fee")
	}
}

func TestQuickSwapSubgraphRegistryBuildLiquidityPoolsQuery(t *testing.T) {
	registry := NewQuickSwapSubgraphRegistry(config.SubgraphPoolConfig{
		Enabled:                true,
		Endpoint:               "http://example.com",
		OrderBy:                "totalValueLockedUSD",
		OrderDirection:         "desc",
		MinTotalValueLockedUSD: "1000000",
	})

	query, variables, mode, err := registry.buildQuery()
	if err != nil {
		t.Fatalf("build query: %v", err)
	}
	if mode != quickQueryModeLiquidityPools {
		t.Fatalf("expected liquidity pools query mode, got %d", mode)
	}
	if !strings.Contains(query, "liquidityPools") || !strings.Contains(query, "LiquidityPool_orderBy") {
		t.Fatalf("expected liquidity pools query, got %q", query)
	}
	if variables["orderBy"] != "totalValueLockedUSD" {
		t.Fatalf("unexpected orderBy: %#v", variables["orderBy"])
	}
}
