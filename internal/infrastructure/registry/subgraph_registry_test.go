package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	"github.com/ethereum/go-ethereum/common"
)

func TestSubgraphRegistryList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"pools": []map[string]string{
					{"id": "0x88e6a0c2ddd26feeb64f039a2c41296fb728693b"},
					{"id": "0x8ad599c3a0ff1de082011efddc58f0328fb0668d"},
				},
			},
		})
	}))
	defer server.Close()

	registry := NewSubgraphRegistry(config.SubgraphPoolConfig{
		Enabled:         true,
		Endpoint:        server.URL,
		RefreshInterval: time.Minute,
		First:           2,
		OrderBy:         "totalValueLockedUSD",
		OrderDirection:  "desc",
	})
	registry.client = server.Client()
	registry.clock = time.Now

	addresses, err := registry.List(context.Background())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(addresses) != 2 {
		t.Fatalf("expected 2 pools, got %d", len(addresses))
	}
}

func TestSubgraphRegistryAddRemove(t *testing.T) {
	registry := NewSubgraphRegistry(config.SubgraphPoolConfig{Enabled: false})
	pool := common.HexToAddress("0x0000000000000000000000000000000000000001")

	if err := registry.Add(context.Background(), pool); err != nil {
		t.Fatalf("add pool: %v", err)
	}
	addresses, err := registry.List(context.Background())
	if err != nil || len(addresses) != 1 {
		t.Fatalf("list after add: %#v err=%v", addresses, err)
	}

	if err := registry.Remove(context.Background(), pool); err != nil {
		t.Fatalf("remove pool: %v", err)
	}
	addresses, err = registry.List(context.Background())
	if err != nil || len(addresses) != 0 {
		t.Fatalf("list after remove: %#v err=%v", addresses, err)
	}
}

func TestSubgraphRegistryBuildQueryValidation(t *testing.T) {
	registry := NewSubgraphRegistry(config.SubgraphPoolConfig{
		Enabled:        true,
		Endpoint:       "http://example.com",
		OrderBy:        "invalid",
		OrderDirection: "desc",
	})
	if _, _, _, err := registry.buildQuery(); err == nil {
		t.Fatal("expected order_by validation error")
	}
}

func TestSubgraphRegistryBuildPoolDayDataQuery(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	registry := NewSubgraphRegistry(config.SubgraphPoolConfig{
		Enabled:                true,
		Endpoint:               "http://example.com",
		OrderBy:                "volume24h",
		OrderDirection:         "desc",
		MinTotalValueLockedUSD: "1000000",
		MinVolume24hUSD:        "200000",
	})
	registry.clock = func() time.Time { return now }

	query, variables, mode, err := registry.buildQuery()
	if err != nil {
		t.Fatalf("build query: %v", err)
	}
	if mode != queryModePoolDayData {
		t.Fatalf("expected poolDayData query mode, got %d", mode)
	}
	if !strings.Contains(query, "poolDayDatas") {
		t.Fatalf("expected poolDayDatas query, got %q", query)
	}
	where, ok := variables["where"].(map[string]any)
	if !ok {
		t.Fatalf("expected where variables, got %#v", variables["where"])
	}
	if where["date"] != poolDayDate(now) {
		t.Fatalf("expected date %d, got %#v", poolDayDate(now), where["date"])
	}
	if where["volumeUSD_gt"] != "200000" {
		t.Fatalf("expected volume filter, got %#v", where["volumeUSD_gt"])
	}
	poolWhere, ok := where["pool_"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested pool filter, got %#v", where["pool_"])
	}
	if poolWhere["totalValueLockedUSD_gt"] != "1000000" {
		t.Fatalf("expected tvl filter, got %#v", poolWhere["totalValueLockedUSD_gt"])
	}
	if variables["orderBy"] != "volumeUSD" || variables["orderDirection"] != "desc" {
		t.Fatalf("unexpected sort variables: %#v", variables)
	}
}

func TestSubgraphRegistryUsesCacheUntilRefreshInterval(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"pools": []map[string]string{
					{"id": "0x88e6a0c2ddd26feeb64f039a2c41296fb728693b"},
				},
			},
		})
	}))
	defer server.Close()

	now := time.Unix(0, 0)
	registry := NewSubgraphRegistry(config.SubgraphPoolConfig{
		Enabled:         true,
		Endpoint:        server.URL,
		RefreshInterval: time.Minute,
		OrderBy:         "totalValueLockedUSD",
		OrderDirection:  "desc",
	})
	registry.client = server.Client()
	registry.clock = func() time.Time { return now }

	ctx := context.Background()
	if _, err := registry.List(ctx); err != nil {
		t.Fatalf("first list: %v", err)
	}
	if _, err := registry.List(ctx); err != nil {
		t.Fatalf("second list: %v", err)
	}
	if requests != 1 {
		t.Fatalf("expected 1 subgraph request, got %d", requests)
	}

	now = now.Add(time.Minute)
	if _, err := registry.List(ctx); err != nil {
		t.Fatalf("third list: %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected refresh after interval, got %d requests", requests)
	}
}
