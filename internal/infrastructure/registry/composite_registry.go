package registry

import (
	"context"
	"sort"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	"github.com/ethereum/go-ethereum/common"
)

// CompositeRegistry merges static config pools with a subgraph-backed registry.
type CompositeRegistry struct {
	static          []common.Address
	subgraph        *SubgraphRegistry
	subgraphEnabled bool
}

func NewCompositeRegistry(cfg config.Univ3SyncConfig) *CompositeRegistry {
	return &CompositeRegistry{
		static:          staticAddresses(cfg.Pools),
		subgraph:        NewSubgraphRegistry(cfg.Subgraph),
		subgraphEnabled: cfg.Subgraph.IsEnabled(),
	}
}

func (r *CompositeRegistry) List(ctx context.Context) ([]common.Address, error) {
	seen := make(map[common.Address]struct{})
	addresses := make([]common.Address, 0)

	for _, address := range r.static {
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address)
	}

	if r.subgraphEnabled {
		subgraphPools, err := r.subgraph.List(ctx)
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
	if r.subgraph == nil {
		r.subgraph = NewSubgraphRegistry(config.SubgraphPoolConfig{})
	}
	return r.subgraph.Add(ctx, address)
}

func (r *CompositeRegistry) Remove(ctx context.Context, address common.Address) error {
	if r.subgraph == nil {
		r.subgraph = NewSubgraphRegistry(config.SubgraphPoolConfig{})
	}
	return r.subgraph.Remove(ctx, address)
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
