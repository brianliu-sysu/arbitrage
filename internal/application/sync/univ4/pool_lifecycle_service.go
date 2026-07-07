package syncv4

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

type PoolLifecycleService = syncapp.PoolLifecycleService[marketv4.PoolID]

func NewPoolLifecycleService(
	registry marketv4.PoolRegistry,
	bootstrap *BootstrapService,
	readiness *ReadinessService,
) *PoolLifecycleService {
	return syncapp.NewPoolLifecycleService(readiness, syncapp.LifecycleHooks[marketv4.PoolID]{
		Bootstrap: func(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) error {
			_, err := bootstrap.Bootstrap(ctx, poolID, blockNumber)
			return err
		},
		ListTracked: registry.List,
		Register: func(ctx context.Context, poolID marketv4.PoolID) error {
			key, err := registry.GetKey(ctx, poolID)
			if err != nil {
				return fmt.Errorf("resolve pool key: %w", err)
			}
			return registry.Add(ctx, poolID, key)
		},
		Unregister: func(ctx context.Context, poolID marketv4.PoolID) error {
			return registry.Remove(ctx, poolID)
		},
	})
}
