package registry

import (
	"context"
	"sort"
	"sync"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	"github.com/ethereum/go-ethereum/common"
)

// PancakeCompositeRegistry merges static config pools with a subgraph-backed registry.
type PancakeCompositeRegistry struct {
	mu              sync.RWMutex
	static          []common.Address
	dynamic         map[common.Address]struct{}
	subgraph        *PancakeSubgraphRegistry
	subgraphEnabled bool
}

func NewPancakeCompositeRegistry(cfg config.PancakeV3SyncConfig) *PancakeCompositeRegistry {
	return &PancakeCompositeRegistry{
		static:          staticAddresses(cfg.Pools),
		dynamic:         make(map[common.Address]struct{}),
		subgraph:        NewPancakeSubgraphRegistry(cfg.Subgraph),
		subgraphEnabled: cfg.Subgraph.IsEnabled(),
	}
}

func (r *PancakeCompositeRegistry) List(ctx context.Context) ([]common.Address, error) {
	if r == nil {
		return nil, nil
	}
	r.mu.RLock()
	static := append([]common.Address(nil), r.static...)
	dynamic := make([]common.Address, 0, len(r.dynamic))
	for address := range r.dynamic {
		dynamic = append(dynamic, address)
	}
	subgraph := r.subgraph
	subgraphEnabled := r.subgraphEnabled
	r.mu.RUnlock()
	seen := make(map[common.Address]struct{})
	addresses := make([]common.Address, 0)

	for _, address := range static {
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address)
	}
	for _, address := range dynamic {
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address)
	}

	if subgraphEnabled && subgraph != nil {
		subgraphPools, err := subgraph.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, address := range subgraphPools {
			if _, ok := seen[address]; ok {
				continue
			}
			seen[address] = struct{}{}
			addresses = append(addresses, address)
		}
	}

	sort.Slice(addresses, func(i, j int) bool {
		return addresses[i].Hex() < addresses[j].Hex()
	})
	return addresses, nil
}

// AsPoolRegistry returns a nil-safe PoolRegistry interface value.
func (r *PancakeCompositeRegistry) AsPoolRegistry() marketpancake.PoolRegistry {
	if r == nil {
		return nil
	}
	return r
}

func (r *PancakeCompositeRegistry) Add(ctx context.Context, address common.Address) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dynamic == nil {
		r.dynamic = make(map[common.Address]struct{})
	}
	r.dynamic[address] = struct{}{}
	return nil
}

func (r *PancakeCompositeRegistry) Remove(ctx context.Context, address common.Address) error {
	r.mu.Lock()
	delete(r.dynamic, address)
	subgraph := r.subgraph
	r.mu.Unlock()
	if subgraph != nil {
		return subgraph.Remove(ctx, address)
	}
	return nil
}

var _ marketpancake.PoolRegistry = (*PancakeCompositeRegistry)(nil)
