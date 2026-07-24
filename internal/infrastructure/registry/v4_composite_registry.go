package registry

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

// CompositeV4Registry merges poolmanager static pools with a V4 subgraph-backed registry.
type CompositeV4Registry struct {
	mu              sync.RWMutex
	static          []v4PoolEntry
	dynamic         map[marketv4.PoolID]v4PoolEntry
	subgraph        *V4SubgraphRegistry
	subgraphEnabled bool
}

func NewCompositeV4Registry(cfg config.Univ4SyncConfig) (*CompositeV4Registry, error) {
	static, err := parseStaticV4Pools(cfg.PoolManager.Pools)
	if err != nil {
		return nil, err
	}
	return &CompositeV4Registry{
		static:          static,
		dynamic:         make(map[marketv4.PoolID]v4PoolEntry),
		subgraph:        NewV4SubgraphRegistry(cfg.Subgraph),
		subgraphEnabled: cfg.Subgraph.IsEnabled(),
	}, nil
}

func (r *CompositeV4Registry) List(ctx context.Context) ([]marketv4.PoolID, error) {
	if r == nil {
		return nil, nil
	}
	r.mu.RLock()
	static := append([]v4PoolEntry(nil), r.static...)
	dynamic := make([]v4PoolEntry, 0, len(r.dynamic))
	for _, entry := range r.dynamic {
		dynamic = append(dynamic, entry)
	}
	subgraph := r.subgraph
	subgraphEnabled := r.subgraphEnabled
	r.mu.RUnlock()
	seen := make(map[marketv4.PoolID]v4PoolEntry)
	entries := make([]v4PoolEntry, 0)

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
		subgraphEntries, err := subgraph.list(ctx)
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

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].id.String() < entries[j].id.String()
	})

	ids := make([]marketv4.PoolID, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.id)
	}
	return ids, nil
}

func (r *CompositeV4Registry) GetKey(ctx context.Context, id marketv4.PoolID) (marketv4.PoolKey, error) {
	r.mu.RLock()
	static := append([]v4PoolEntry(nil), r.static...)
	if entry, ok := r.dynamic[id]; ok {
		r.mu.RUnlock()
		return entry.key, nil
	}
	subgraph := r.subgraph
	subgraphEnabled := r.subgraphEnabled
	r.mu.RUnlock()
	for _, entry := range static {
		if entry.id == id {
			return entry.key, nil
		}
	}
	if subgraphEnabled && subgraph != nil {
		if key, ok, err := subgraph.GetKey(ctx, id); err != nil {
			return marketv4.PoolKey{}, err
		} else if ok {
			return key, nil
		}
	}
	return marketv4.PoolKey{}, fmt.Errorf("pool %s not found in registry", id)
}

// AsPoolRegistry returns a nil-safe PoolRegistry interface value.
func (r *CompositeV4Registry) AsPoolRegistry() marketv4.PoolRegistry {
	if r == nil {
		return nil
	}
	return r
}

func (r *CompositeV4Registry) Add(ctx context.Context, id marketv4.PoolID, key marketv4.PoolKey) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dynamic == nil {
		r.dynamic = make(map[marketv4.PoolID]v4PoolEntry)
	}
	r.dynamic[id] = v4PoolEntry{id: id, key: key}
	return nil
}

func (r *CompositeV4Registry) Remove(ctx context.Context, id marketv4.PoolID) error {
	r.mu.Lock()
	delete(r.dynamic, id)
	subgraph := r.subgraph
	r.mu.Unlock()
	if subgraph != nil {
		return subgraph.Remove(ctx, id)
	}
	return nil
}

var _ marketv4.PoolRegistry = (*CompositeV4Registry)(nil)
