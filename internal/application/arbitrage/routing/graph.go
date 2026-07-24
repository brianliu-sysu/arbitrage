package routing

import (
	"context"
	"errors"
	"fmt"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

// ErrNoPoolsAvailable indicates that protocol bootstrap has not made any pools
// available for routing yet.
var ErrNoPoolsAvailable = errors.New("no pools available for routing")

// PoolEdgeSource converts one protocol's tracked pools into unified graph
// edges. Adding a protocol does not require changing the graph aggregator.
type PoolEdgeSource interface {
	Name() string
	LoadEdges(context.Context) ([]quoteunified.PoolEdge, error)
}

// BuildUnifiedPoolGraph builds a routing graph from the configured protocol
// sources.
func BuildUnifiedPoolGraph(
	ctx context.Context,
	sources ...PoolEdgeSource,
) (quoteunified.PoolGraph, error) {
	edges := make([]quoteunified.PoolEdge, 0)
	for _, source := range sources {
		if source == nil {
			continue
		}
		sourceEdges, err := source.LoadEdges(ctx)
		if err != nil {
			return nil, fmt.Errorf("load %s pool graph edges: %w", source.Name(), err)
		}
		edges = append(edges, sourceEdges...)
	}
	if len(edges) == 0 {
		return nil, ErrNoPoolsAvailable
	}
	return quoteunified.NewStaticPoolGraph(edges), nil
}

type addressPoolRegistry interface {
	List(context.Context) ([]common.Address, error)
}

type addressPoolRepository[Pool any] interface {
	Get(context.Context, common.Address) (Pool, error)
}

type addressPoolEdgeMapper[Pool any] func(Pool) (quoteunified.PoolEdge, bool)

type addressPoolEdgeSource[Pool any] struct {
	name     string
	registry addressPoolRegistry
	pools    addressPoolRepository[Pool]
	toEdge   addressPoolEdgeMapper[Pool]
}

func (s *addressPoolEdgeSource[Pool]) Name() string { return s.name }

func (s *addressPoolEdgeSource[Pool]) LoadEdges(ctx context.Context) ([]quoteunified.PoolEdge, error) {
	addresses, err := s.registry.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pools: %w", err)
	}
	edges := make([]quoteunified.PoolEdge, 0, len(addresses))
	for _, address := range addresses {
		pool, err := s.pools.Get(ctx, address)
		if err != nil {
			return nil, fmt.Errorf("load pool %s: %w", address.Hex(), err)
		}
		edge, ok := s.toEdge(pool)
		if ok {
			edges = append(edges, edge)
		}
	}
	return edges, nil
}

func NewUniv3PoolEdgeSource(
	registry marketuniv3.PoolRegistry,
	pools marketuniv3.PoolRepository,
) PoolEdgeSource {
	if registry == nil || pools == nil {
		return nil
	}
	return &addressPoolEdgeSource[*marketuniv3.Pool]{
		name:     "univ3",
		registry: registry,
		pools:    pools,
		toEdge:   univ3PoolEdge,
	}
}

func univ3PoolEdge(pool *marketuniv3.Pool) (quoteunified.PoolEdge, bool) {
	if pool == nil {
		return quoteunified.PoolEdge{}, false
	}
	return quoteunified.PoolEdge{
		Version: quoteunified.PoolVersionV3,
		PoolV3:  pool.Address,
		Token0:  pool.Token0,
		Token1:  pool.Token1,
	}, true
}

func NewPancakeV3PoolEdgeSource(
	registry marketpancake.PoolRegistry,
	pools marketpancake.PoolRepository,
) PoolEdgeSource {
	if registry == nil || pools == nil {
		return nil
	}
	return &addressPoolEdgeSource[*marketpancake.Pool]{
		name:     "pancakev3",
		registry: registry,
		pools:    pools,
		toEdge:   pancakeV3PoolEdge,
	}
}

func pancakeV3PoolEdge(pool *marketpancake.Pool) (quoteunified.PoolEdge, bool) {
	if pool == nil {
		return quoteunified.PoolEdge{}, false
	}
	return quoteunified.PoolEdge{
		Version:       quoteunified.PoolVersionPancakeV3,
		PoolPancakeV3: pool.Address,
		Token0:        pool.Token0,
		Token1:        pool.Token1,
	}, true
}

func NewQuickSwapV3PoolEdgeSource(
	registry marketquick.PoolRegistry,
	pools marketquick.PoolRepository,
) PoolEdgeSource {
	if registry == nil || pools == nil {
		return nil
	}
	return &addressPoolEdgeSource[*marketquick.Pool]{
		name:     "quickswapv3",
		registry: registry,
		pools:    pools,
		toEdge:   quickSwapV3PoolEdge,
	}
}

func quickSwapV3PoolEdge(pool *marketquick.Pool) (quoteunified.PoolEdge, bool) {
	if pool == nil {
		return quoteunified.PoolEdge{}, false
	}
	return quoteunified.PoolEdge{
		Version:         quoteunified.PoolVersionQuickSwapV3,
		PoolQuickSwapV3: pool.Address,
		Token0:          pool.Token0,
		Token1:          pool.Token1,
	}, true
}

type univ4PoolEdgeSource struct {
	registry marketuniv4.PoolRegistry
	pools    marketuniv4.PoolRepository
}

func NewUniv4PoolEdgeSource(
	registry marketuniv4.PoolRegistry,
	pools marketuniv4.PoolRepository,
) PoolEdgeSource {
	if registry == nil || pools == nil {
		return nil
	}
	return &univ4PoolEdgeSource{registry: registry, pools: pools}
}

func (s *univ4PoolEdgeSource) Name() string { return "univ4" }

func (s *univ4PoolEdgeSource) LoadEdges(ctx context.Context) ([]quoteunified.PoolEdge, error) {
	poolIDs, err := s.registry.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pools: %w", err)
	}
	edges := make([]quoteunified.PoolEdge, 0, len(poolIDs))
	for _, poolID := range poolIDs {
		pool, err := s.pools.Get(ctx, poolID)
		if err != nil {
			return nil, fmt.Errorf("load pool %s: %w", poolID.String(), err)
		}
		if pool == nil {
			continue
		}
		edges = append(edges, quoteunified.PoolEdge{
			Version: quoteunified.PoolVersionV4,
			PoolV4:  pool.ID,
			Token0:  pool.Key.Currency0,
			Token1:  pool.Key.Currency1,
		})
	}
	return edges, nil
}

type balancerPoolEdgeSource struct {
	registry marketbalancer.PoolRegistry
	pools    marketbalancer.PoolRepository
}

func NewBalancerPoolEdgeSource(
	registry marketbalancer.PoolRegistry,
	pools marketbalancer.PoolRepository,
) PoolEdgeSource {
	if registry == nil || pools == nil {
		return nil
	}
	return &balancerPoolEdgeSource{registry: registry, pools: pools}
}

func (s *balancerPoolEdgeSource) Name() string { return "balancer" }

func (s *balancerPoolEdgeSource) LoadEdges(ctx context.Context) ([]quoteunified.PoolEdge, error) {
	poolIDs, err := s.registry.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pools: %w", err)
	}
	edges := make([]quoteunified.PoolEdge, 0)
	for _, poolID := range poolIDs {
		pool, err := s.pools.Get(ctx, poolID)
		if err != nil {
			return nil, fmt.Errorf("load pool %s: %w", poolID.String(), err)
		}
		if pool == nil || len(pool.Tokens) < 2 {
			continue
		}
		for i := 0; i < len(pool.Tokens); i++ {
			for j := i + 1; j < len(pool.Tokens); j++ {
				edges = append(edges, quoteunified.PoolEdge{
					Version:      quoteunified.PoolVersionBalancer,
					PoolBalancer: pool.ID,
					Token0:       pool.Tokens[i],
					Token1:       pool.Tokens[j],
				})
			}
		}
	}
	return edges, nil
}
