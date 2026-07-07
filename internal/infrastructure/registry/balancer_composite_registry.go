package registry

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

// CompositeBalancerRegistry merges static pools with a Balancer subgraph-backed registry.
type CompositeBalancerRegistry struct {
	static          []balancerPoolEntry
	subgraph        *BalancerSubgraphRegistry
	subgraphEnabled bool
}

func NewCompositeBalancerRegistry(cfg config.BalancerSyncConfig, defaultVault common.Address) (*CompositeBalancerRegistry, error) {
	static, err := parseStaticBalancerPools(cfg.Pools, defaultVault)
	if err != nil {
		return nil, err
	}
	return &CompositeBalancerRegistry{
		static:          static,
		subgraph:        NewBalancerSubgraphRegistry(cfg.Subgraph, defaultVault),
		subgraphEnabled: cfg.Subgraph.IsEnabled(),
	}, nil
}

func (r *CompositeBalancerRegistry) List(ctx context.Context) ([]marketbalancer.PoolID, error) {
	seen := make(map[marketbalancer.PoolID]balancerPoolEntry)
	entries := make([]balancerPoolEntry, 0)

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

	sortBalancerPoolEntries(entries)
	ids := make([]marketbalancer.PoolID, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.id)
	}
	return ids, nil
}

func (r *CompositeBalancerRegistry) GetSpec(ctx context.Context, id marketbalancer.PoolID) (marketbalancer.PoolSpec, error) {
	for _, entry := range r.static {
		if entry.id == id {
			return entry.spec, nil
		}
	}
	if r.subgraphEnabled {
		if spec, ok, err := r.subgraph.GetSpec(ctx, id); err != nil {
			return marketbalancer.PoolSpec{}, err
		} else if ok {
			return spec, nil
		}
	}
	return marketbalancer.PoolSpec{}, fmt.Errorf("balancer pool %s not found in registry", id)
}

func (r *CompositeBalancerRegistry) Add(ctx context.Context, id marketbalancer.PoolID, spec marketbalancer.PoolSpec) error {
	if r.subgraph == nil {
		r.subgraph = NewBalancerSubgraphRegistry(config.BalancerSubgraphPoolConfig{}, common.Address{})
	}
	return r.subgraph.Add(ctx, id, spec)
}

func (r *CompositeBalancerRegistry) Remove(ctx context.Context, id marketbalancer.PoolID) error {
	if r.subgraph == nil {
		r.subgraph = NewBalancerSubgraphRegistry(config.BalancerSubgraphPoolConfig{}, common.Address{})
	}
	return r.subgraph.Remove(ctx, id)
}

// AsPoolRegistry returns a nil-safe PoolRegistry interface value.
func (r *CompositeBalancerRegistry) AsPoolRegistry() marketbalancer.PoolRegistry {
	if r == nil {
		return nil
	}
	return r
}

var _ marketbalancer.PoolRegistry = (*CompositeBalancerRegistry)(nil)
