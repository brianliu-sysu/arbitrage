package registry

import (
	"context"
	"sort"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	"github.com/ethereum/go-ethereum/common"
)

// PancakeCompositeRegistry merges static config pools with a subgraph-backed registry.
type PancakeCompositeRegistry struct {
	static          []common.Address
	subgraph        *SubgraphRegistry
	subgraphEnabled bool
}

func NewPancakeCompositeRegistry(cfg config.PancakeV3SyncConfig) *PancakeCompositeRegistry {
	return &PancakeCompositeRegistry{
		static:          staticAddresses(cfg.Pools),
		subgraph:        NewSubgraphRegistry(cfg.Subgraph),
		subgraphEnabled: cfg.Subgraph.IsEnabled(),
	}
}

func (r *PancakeCompositeRegistry) List(ctx context.Context) ([]common.Address, error) {
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

func (r *PancakeCompositeRegistry) Add(ctx context.Context, address common.Address) error {
	if r.subgraph == nil {
		r.subgraph = NewSubgraphRegistry(config.SubgraphPoolConfig{})
	}
	return r.subgraph.Add(ctx, address)
}

func (r *PancakeCompositeRegistry) Remove(ctx context.Context, address common.Address) error {
	if r.subgraph == nil {
		r.subgraph = NewSubgraphRegistry(config.SubgraphPoolConfig{})
	}
	return r.subgraph.Remove(ctx, address)
}

var _ marketpancake.PoolRegistry = (*PancakeCompositeRegistry)(nil)
