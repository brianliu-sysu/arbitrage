package registry

import (
	"context"
	"sort"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	"github.com/ethereum/go-ethereum/common"
)

// QuickSwapCompositeRegistry merges static config pools with a subgraph-backed registry.
type QuickSwapCompositeRegistry struct {
	static          []common.Address
	subgraph        *QuickSwapSubgraphRegistry
	subgraphEnabled bool
}

func NewQuickSwapCompositeRegistry(cfg config.QuickSwapV3SyncConfig) *QuickSwapCompositeRegistry {
	return &QuickSwapCompositeRegistry{
		static:          staticAddresses(cfg.Pools),
		subgraph:        NewQuickSwapSubgraphRegistry(cfg.Subgraph),
		subgraphEnabled: cfg.Subgraph.IsEnabled(),
	}
}

func (r *QuickSwapCompositeRegistry) List(ctx context.Context) ([]common.Address, error) {
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

// AsPoolRegistry returns a nil-safe PoolRegistry interface value.
func (r *QuickSwapCompositeRegistry) AsPoolRegistry() marketquick.PoolRegistry {
	if r == nil {
		return nil
	}
	return r
}

func (r *QuickSwapCompositeRegistry) Add(ctx context.Context, address common.Address) error {
	if r.subgraph == nil {
		r.subgraph = NewQuickSwapSubgraphRegistry(config.SubgraphPoolConfig{})
	}
	return r.subgraph.Add(ctx, address)
}

func (r *QuickSwapCompositeRegistry) Remove(ctx context.Context, address common.Address) error {
	if r.subgraph == nil {
		r.subgraph = NewQuickSwapSubgraphRegistry(config.SubgraphPoolConfig{})
	}
	return r.subgraph.Remove(ctx, address)
}

var _ marketquick.PoolRegistry = (*QuickSwapCompositeRegistry)(nil)
