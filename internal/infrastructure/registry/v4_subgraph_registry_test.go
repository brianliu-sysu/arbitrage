package registry

import (
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
)

func TestV4SubgraphRegistryBuildPoolWhereFilterDefaultHooks(t *testing.T) {
	registry := NewV4SubgraphRegistry(config.V4SubgraphPoolConfig{
		SubgraphPoolConfig: config.SubgraphPoolConfig{
			MinTotalValueLockedUSD: "1000000",
		},
	})

	where := registry.buildPoolWhereFilter()
	hooks, ok := where["hooks_in"].([]string)
	if !ok {
		t.Fatalf("expected hooks_in filter, got %#v", where["hooks_in"])
	}
	if len(hooks) != 1 || hooks[0] != config.DefaultV4HooksAddress {
		t.Fatalf("expected default zero hook filter, got %#v", hooks)
	}
}

func TestV4SubgraphRegistryBuildPoolWhereFilterCustomHooks(t *testing.T) {
	customHook := "0x0000000000000000000000000000000000000001"
	registry := NewV4SubgraphRegistry(config.V4SubgraphPoolConfig{
		Hooks: []string{customHook, "  " + customHook + "  "},
	})

	where := registry.buildPoolWhereFilter()
	hooks, ok := where["hooks_in"].([]string)
	if !ok {
		t.Fatalf("expected hooks_in filter, got %#v", where["hooks_in"])
	}
	if len(hooks) != 2 || hooks[0] != customHook || hooks[1] != customHook {
		t.Fatalf("expected normalized custom hooks, got %#v", hooks)
	}
}
