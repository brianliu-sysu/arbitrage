package registry

import (
	"context"
	"fmt"
	"sync"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

// CompositeBalancerRegistry merges static pools with a Balancer subgraph-backed registry.
type CompositeBalancerRegistry struct {
	mu              sync.RWMutex
	static          []balancerPoolEntry
	dynamic         map[marketbalancer.PoolID]balancerPoolEntry
	subgraph        *BalancerSubgraphRegistry
	subgraphEnabled bool
}

func NewCompositeBalancerRegistry(cfg config.BalancerSyncConfig, defaultVault, defaultVaultV3 common.Address) (*CompositeBalancerRegistry, error) {
	static, err := parseStaticBalancerPools(cfg.Pools, defaultVault, defaultVaultV3)
	if err != nil {
		return nil, err
	}
	return &CompositeBalancerRegistry{
		static:          static,
		dynamic:         make(map[marketbalancer.PoolID]balancerPoolEntry),
		subgraph:        NewBalancerSubgraphRegistry(cfg.Subgraph, defaultVault, defaultVaultV3),
		subgraphEnabled: cfg.Subgraph.IsEnabled(),
	}, nil
}

func (r *CompositeBalancerRegistry) List(ctx context.Context) ([]marketbalancer.PoolID, error) {
	if r == nil {
		return nil, nil
	}
	r.mu.RLock()
	static := append([]balancerPoolEntry(nil), r.static...)
	dynamic := make([]balancerPoolEntry, 0, len(r.dynamic))
	for _, entry := range r.dynamic {
		dynamic = append(dynamic, entry)
	}
	subgraph := r.subgraph
	subgraphEnabled := r.subgraphEnabled
	r.mu.RUnlock()
	seen := make(map[marketbalancer.PoolID]balancerPoolEntry)
	entries := make([]balancerPoolEntry, 0)

	for _, entry := range static {
		if _, ok := seen[entry.id]; ok {
			continue
		}
		seen[entry.id] = entry
		entries = append(entries, entry)
	}

	for _, entry := range dynamic {
		if _, ok := seen[entry.id]; ok {
			continue
		}
		seen[entry.id] = entry
		entries = append(entries, entry)
	}

	if subgraphEnabled && subgraph != nil {
		subgraphEntries, err := subgraph.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, entry := range subgraphEntries {
			if _, ok := seen[entry.id]; ok {
				continue
			}
			seen[entry.id] = entry
			entries = append(entries, entry)
		}
	}

	sortBalancerPoolEntries(entries)
	ids := make([]marketbalancer.PoolID, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.id)
	}
	return ids, nil
}

func (r *CompositeBalancerRegistry) GetSpec(ctx context.Context, id marketbalancer.PoolID) (marketbalancer.PoolSpec, error) {
	r.mu.RLock()
	static := append([]balancerPoolEntry(nil), r.static...)
	if entry, ok := r.dynamic[id]; ok {
		r.mu.RUnlock()
		return entry.spec, nil
	}
	subgraph := r.subgraph
	subgraphEnabled := r.subgraphEnabled
	r.mu.RUnlock()
	for _, entry := range static {
		if entry.id == id {
			return entry.spec, nil
		}
	}
	if subgraphEnabled && subgraph != nil {
		if spec, ok, err := subgraph.GetSpec(ctx, id); err != nil {
			return marketbalancer.PoolSpec{}, err
		} else if ok {
			return spec, nil
		}
	}
	return marketbalancer.PoolSpec{}, fmt.Errorf("balancer pool %s not found in registry", id)
}

func (r *CompositeBalancerRegistry) Add(ctx context.Context, id marketbalancer.PoolID, spec marketbalancer.PoolSpec) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dynamic == nil {
		r.dynamic = make(map[marketbalancer.PoolID]balancerPoolEntry)
	}
	r.dynamic[id] = balancerPoolEntry{id: id, spec: spec}
	return nil
}

func (r *CompositeBalancerRegistry) Remove(ctx context.Context, id marketbalancer.PoolID) error {
	r.mu.Lock()
	delete(r.dynamic, id)
	subgraph := r.subgraph
	r.mu.Unlock()
	if subgraph != nil {
		return subgraph.Remove(ctx, id)
	}
	return nil
}

// AsPoolRegistry returns a nil-safe PoolRegistry interface value.
func (r *CompositeBalancerRegistry) AsPoolRegistry() marketbalancer.PoolRegistry {
	if r == nil {
		return nil
	}
	return r
}

var _ marketbalancer.PoolRegistry = (*CompositeBalancerRegistry)(nil)
