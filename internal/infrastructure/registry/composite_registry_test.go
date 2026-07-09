package registry_test

import (
	"context"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/registry"
	"github.com/ethereum/go-ethereum/common"
)

func TestCompositeRegistryDisabledReturnsNoPools(t *testing.T) {
	reg := registry.NewCompositeRegistry(config.Univ3SyncConfig{
		Enabled: false,
		Pools: []config.StaticPoolConfig{
			{Address: "0x4585FE77225b41b697C938B018E2Ac67Ac5a20c0", Fee: 500},
		},
	})

	pools, err := reg.List(context.Background())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 0 {
		t.Fatalf("expected no pools when univ3 sync disabled, got %d", len(pools))
	}
}

func TestCompositeRegistryEnabledReturnsStaticPools(t *testing.T) {
	address := "0x4585FE77225b41b697C938B018E2Ac67Ac5a20c0"
	reg := registry.NewCompositeRegistry(config.Univ3SyncConfig{
		Enabled: true,
		Pools: []config.StaticPoolConfig{
			{Address: address, Fee: 500},
		},
	})

	pools, err := reg.List(context.Background())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(pools))
	}
	if pools[0] != common.HexToAddress(address) {
		t.Fatalf("unexpected pool address %s", pools[0].Hex())
	}
}

func TestCompositeRegistryAddVisibleWhenSubgraphDisabled(t *testing.T) {
	address := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	reg := registry.NewCompositeRegistry(config.Univ3SyncConfig{Enabled: true})

	if err := reg.Add(context.Background(), address); err != nil {
		t.Fatalf("add pool: %v", err)
	}
	pools, err := reg.List(context.Background())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 1 || pools[0] != address {
		t.Fatalf("expected dynamic pool %s, got %v", address.Hex(), pools)
	}
}
