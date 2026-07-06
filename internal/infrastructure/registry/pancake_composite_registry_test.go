package registry

import (
	"testing"

	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
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
