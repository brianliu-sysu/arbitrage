package registry

import (
	"context"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	"github.com/ethereum/go-ethereum/common"
)

func TestNormalizeBalancerOrderBy(t *testing.T) {
	tests := []struct {
		in     string
		schema string
		want   string
	}{
		{in: "volume24h", schema: "v2", want: "totalSwapVolume"},
		{in: "volume24h", schema: "v3", want: "id"},
		{in: "totalLiquidity", schema: "v2", want: "totalLiquidity"},
		{in: "totalLiquidity", schema: "v3", want: "id"},
		{in: "address", schema: "v3", want: "address"},
	}
	for _, tc := range tests {
		if got := normalizeBalancerOrderBy(tc.in, tc.schema); got != tc.want {
			t.Fatalf("normalizeBalancerOrderBy(%q, %q) = %q, want %q", tc.in, tc.schema, got, tc.want)
		}
	}
}

func TestBalancerSubgraphRegistryBuildV3Query(t *testing.T) {
	registry := NewBalancerSubgraphRegistry(config.BalancerSubgraphPoolConfig{
		SubgraphPoolConfig: config.SubgraphPoolConfig{
			Endpoint:       "https://example.com/v3-pools-mainnet-smol/latest/gn",
			OrderBy:        "totalSwapVolume",
			OrderDirection: "desc",
			First:          25,
		},
		PoolTypes: []string{"Weighted", "Stable"},
	}, common.HexToAddress("0xBA12222222228d8Ba445958a75a0704d566BF2C8"), common.HexToAddress("0xbA1333333333a1BA1108E8412f11850A5C319bA9"))

	query, variables, err := registry.buildQuery()
	if err != nil {
		t.Fatalf("build query: %v", err)
	}
	if query == "" {
		t.Fatal("expected query")
	}
	if variables["orderBy"] != "id" {
		t.Fatalf("expected v3 orderBy=id, got %v", variables["orderBy"])
	}
	where, ok := variables["where"].(map[string]any)
	if !ok {
		t.Fatalf("expected where filter, got %T", variables["where"])
	}
	factory, ok := where["factory_"].(map[string]any)
	if !ok {
		t.Fatalf("expected factory_ filter, got %v", where)
	}
	types, ok := factory["type_in"].([]string)
	if !ok || len(types) != 2 {
		t.Fatalf("expected factory type_in filter, got %v", factory["type_in"])
	}
}

func TestBalancerSubgraphRegistryQueryV3Endpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live balancer v3 subgraph query in short mode")
	}

	registry := NewBalancerSubgraphRegistry(config.BalancerSubgraphPoolConfig{
		SubgraphPoolConfig: config.SubgraphPoolConfig{
			Enabled:        true,
			Endpoint:       "https://api.subgraph.ormilabs.com/api/public/717cf785-de57-4761-94dd-9ac51b019902/subgraphs/v3-pools-mainnet-smol/latest/gn",
			OrderBy:        "volume24h",
			OrderDirection: "desc",
			First:          3,
		},
		PoolTypes: []string{"Weighted", "Stable"},
	}, common.HexToAddress("0xBA12222222228d8Ba445958a75a0704d566BF2C8"), common.HexToAddress("0xbA1333333333a1BA1108E8412f11850A5C319bA9"))

	entries, err := registry.List(context.Background())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected pools from v3 subgraph")
	}
}
