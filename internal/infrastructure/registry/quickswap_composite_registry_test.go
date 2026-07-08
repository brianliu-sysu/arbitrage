package registry

import (
	"testing"

	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
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
