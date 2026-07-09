package registry

import (
	"context"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	"github.com/ethereum/go-ethereum/common"
)

func TestQuickSwapCompositeRegistryAsPoolRegistryNilSafe(t *testing.T) {
	var registry *QuickSwapCompositeRegistry

	var typedNilIface marketquick.PoolRegistry = registry
	if typedNilIface == nil {
		t.Fatal("typed nil pointer assignment should produce non-nil interface in Go")
	}
	if registry.AsPoolRegistry() != nil {
		t.Fatal("expected nil interface from AsPoolRegistry for nil registry pointer")
	}
}

func TestQuickSwapCompositeRegistryAddVisibleWhenSubgraphDisabled(t *testing.T) {
	address := common.HexToAddress("0x00000000000000000000000000000000000000c1")
	registry := NewQuickSwapCompositeRegistry(config.QuickSwapV3SyncConfig{})

	if err := registry.Add(context.Background(), address); err != nil {
		t.Fatalf("add pool: %v", err)
	}
	pools, err := registry.List(context.Background())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 1 || pools[0] != address {
		t.Fatalf("expected dynamic quickswap pool %s, got %v", address.Hex(), pools)
	}
}
