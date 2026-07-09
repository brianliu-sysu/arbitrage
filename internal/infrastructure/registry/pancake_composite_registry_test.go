package registry

import (
	"context"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

func TestPancakeCompositeRegistryAsPoolRegistryNilSafe(t *testing.T) {
	var registry *PancakeCompositeRegistry

	var typedNilIface marketpancake.PoolRegistry = registry
	if typedNilIface == nil {
		t.Fatal("typed nil pointer assignment should produce non-nil interface in Go")
	}
	if registry.AsPoolRegistry() != nil {
		t.Fatal("expected nil interface from AsPoolRegistry for nil registry pointer")
	}
}

func TestCompositeV4RegistryAsPoolRegistryNilSafe(t *testing.T) {
	var registry *CompositeV4Registry
	if registry.AsPoolRegistry() != nil {
		t.Fatal("expected nil interface for nil registry pointer")
	}
}

func TestPancakeCompositeRegistryAddVisibleWhenSubgraphDisabled(t *testing.T) {
	address := common.HexToAddress("0x00000000000000000000000000000000000000b1")
	registry := NewPancakeCompositeRegistry(config.PancakeV3SyncConfig{})

	if err := registry.Add(context.Background(), address); err != nil {
		t.Fatalf("add pool: %v", err)
	}
	pools, err := registry.List(context.Background())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 1 || pools[0] != address {
		t.Fatalf("expected dynamic pancake pool %s, got %v", address.Hex(), pools)
	}
}

func TestCompositeV4RegistryAddVisibleWhenSubgraphDisabled(t *testing.T) {
	key := marketv4.PoolKey{
		Currency0:   common.HexToAddress("0x0000000000000000000000000000000000000001"),
		Currency1:   common.HexToAddress("0x0000000000000000000000000000000000000002"),
		Fee:         3000,
		TickSpacing: 60,
	}
	id, err := marketv4.ComputePoolID(key)
	if err != nil {
		t.Fatalf("compute pool id: %v", err)
	}
	registry, err := NewCompositeV4Registry(config.Univ4SyncConfig{})
	if err != nil {
		t.Fatalf("new v4 registry: %v", err)
	}

	if err := registry.Add(context.Background(), id, key); err != nil {
		t.Fatalf("add pool: %v", err)
	}
	pools, err := registry.List(context.Background())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 1 || pools[0] != id {
		t.Fatalf("expected dynamic v4 pool %s, got %v", id.String(), pools)
	}
	gotKey, err := registry.GetKey(context.Background(), id)
	if err != nil {
		t.Fatalf("get key: %v", err)
	}
	if gotKey != key {
		t.Fatalf("expected key %+v, got %+v", key, gotKey)
	}
}
