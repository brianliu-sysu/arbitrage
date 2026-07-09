package registry

import (
	"context"
	"sort"
	"sync"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	"github.com/ethereum/go-ethereum/common"
)

// CompositeRegistry merges static config pools with a subgraph-backed registry.
type CompositeRegistry struct {
	mu              sync.RWMutex
	enabled         bool
	static          []common.Address
	dynamic         map[common.Address]struct{}
	subgraph        *SubgraphRegistry
	subgraphEnabled bool
}

func NewCompositeRegistry(cfg config.Univ3SyncConfig) *CompositeRegistry {
	return &CompositeRegistry{
		enabled:         cfg.Enabled,
		static:          staticAddresses(cfg.Pools),
		dynamic:         make(map[common.Address]struct{}),
		subgraph:        NewSubgraphRegistry(cfg.Subgraph),
		subgraphEnabled: cfg.Subgraph.IsEnabled(),
	}
}

func (r *CompositeRegistry) List(ctx context.Context) ([]common.Address, error) {
	if r == nil {
		return nil, nil
	}
	r.mu.RLock()
	enabled := r.enabled
	static := append([]common.Address(nil), r.static...)
	dynamic := make([]common.Address, 0, len(r.dynamic))
	for address := range r.dynamic {
		dynamic = append(dynamic, address)
	}
	subgraph := r.subgraph
	subgraphEnabled := r.subgraphEnabled
	r.mu.RUnlock()
	if !enabled && len(dynamic) == 0 {
		return nil, nil
	}
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

func (r *CompositeRegistry) Add(ctx context.Context, address common.Address) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dynamic == nil {
		r.dynamic = make(map[common.Address]struct{})
	}
	r.dynamic[address] = struct{}{}
	return nil
}

func (r *CompositeRegistry) Remove(ctx context.Context, address common.Address) error {
	r.mu.Lock()
	delete(r.dynamic, address)
	subgraph := r.subgraph
	r.mu.Unlock()
	if subgraph != nil {
		return subgraph.Remove(ctx, address)
	}
	return nil
}

func staticAddresses(pools []config.StaticPoolConfig) []common.Address {
	addresses := make([]common.Address, 0, len(pools))
	for _, pool := range pools {
		if pool.Address == "" {
			continue
		}
		addresses = append(addresses, common.HexToAddress(pool.Address))
	}
	return addresses
}

var _ marketv3.PoolRegistry = (*CompositeRegistry)(nil)
