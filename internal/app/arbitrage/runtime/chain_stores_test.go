package runtime

import (
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence"
)

func TestNewChainStoresSharesMemoryBackend(t *testing.T) {
	stores, err := newChainStores(persistence.Config{UseMemory: true})
	if err != nil {
		t.Fatalf("create chain stores: %v", err)
	}
	defer stores.Close()

	if stores.durable == nil || stores.runtime == nil {
		t.Fatal("expected durable and runtime stores")
	}
	if stores.durable != stores.runtime {
		t.Fatal("expected memory mode to share one store")
	}
	if stores.hasSeparateRuntime() {
		t.Fatal("memory mode must not require asynchronous persistence")
	}
	if !stores.usesMemory() {
		t.Fatal("expected memory mode")
	}
}

func TestChainStoresDetectsSeparateRuntimeStore(t *testing.T) {
	durable := persistence.MemoryServices()
	runtime := persistence.MemoryServices()
	stores := &chainStores{durable: durable, runtime: runtime, separateRuntime: true}
	defer stores.Close()

	if !stores.hasSeparateRuntime() {
		t.Fatal("expected separate runtime store")
	}
}
