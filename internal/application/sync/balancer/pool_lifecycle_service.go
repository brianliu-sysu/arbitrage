package balancersync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
)

type PoolLifecycleService = syncapp.PoolLifecycleService[marketbalancer.PoolID]

func NewPoolLifecycleService(
	readiness *ReadinessService,
	registry marketbalancer.PoolRegistry,
	bootstrap *BootstrapService,
) *PoolLifecycleService {
	return syncapp.NewPoolLifecycleService(readiness, syncapp.LifecycleHooks[marketbalancer.PoolID]{
		Bootstrap: func(ctx context.Context, poolID marketbalancer.PoolID, blockNumber uint64) error {
			_, err := bootstrap.Bootstrap(ctx, poolID, blockNumber)
			return err
		},
		ListTracked: registry.List,
		Register: func(ctx context.Context, poolID marketbalancer.PoolID) error {
			spec, err := registry.GetSpec(ctx, poolID)
			if err != nil {
				return fmt.Errorf("resolve pool spec: %w", err)
			}
			return registry.Add(ctx, poolID, spec)
		},
		Unregister: registry.Remove,
	})
}
