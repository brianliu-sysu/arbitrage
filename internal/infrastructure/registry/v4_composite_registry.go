package registry

import (
	"context"
	"fmt"
	"sort"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

// CompositeV4Registry merges poolmanager static pools with a V4 subgraph-backed registry.
type CompositeV4Registry struct {
	static          []v4PoolEntry
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
		subgraph:        NewV4SubgraphRegistry(cfg.Subgraph),
		subgraphEnabled: cfg.Subgraph.IsEnabled(),
	}, nil
}

func (r *CompositeV4Registry) List(ctx context.Context) ([]marketv4.PoolID, error) {
	seen := make(map[marketv4.PoolID]v4PoolEntry)
	entries := make([]v4PoolEntry, 0)

	for _, entry := range r.static {
		if _, ok := seen[entry.id]; ok {
			continue
		}
		seen[entry.id] = entry
		entries = append(entries, entry)
	}

	if r.subgraphEnabled {
		subgraphEntries, err := r.subgraph.List(ctx)
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
	for _, entry := range r.static {
		if entry.id == id {
			return entry.key, nil
		}
	}
	if r.subgraphEnabled {
		if key, ok, err := r.subgraph.GetKey(ctx, id); err != nil {
			return marketv4.PoolKey{}, err
		} else if ok {
			return key, nil
		}
	}
	return marketv4.PoolKey{}, fmt.Errorf("pool %s not found in registry", id)
}

func (r *CompositeV4Registry) Add(ctx context.Context, id marketv4.PoolID, key marketv4.PoolKey) error {
	if r.subgraph == nil {
		r.subgraph = NewV4SubgraphRegistry(config.V4SubgraphPoolConfig{})
	}
	return r.subgraph.Add(ctx, id, key)
}

func (r *CompositeV4Registry) Remove(ctx context.Context, id marketv4.PoolID) error {
	if r.subgraph == nil {
		r.subgraph = NewV4SubgraphRegistry(config.V4SubgraphPoolConfig{})
	}
	return r.subgraph.Remove(ctx, id)
}

var _ marketv4.PoolRegistry = (*CompositeV4Registry)(nil)
