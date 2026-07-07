package clv3sync

import (
	"context"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/ethereum/go-ethereum/common"
)

type PoolLifecycleService = syncapp.PoolLifecycleService[common.Address]

func NewPoolLifecycleService(
	registry PoolRegistry,
	bootstrap *BootstrapService,
	readiness *ReadinessService,
) *PoolLifecycleService {
	return syncapp.NewPoolLifecycleService(readiness, syncapp.LifecycleHooks[common.Address]{
		Bootstrap: func(ctx context.Context, poolAddress common.Address, blockNumber uint64) error {
			_, err := bootstrap.Bootstrap(ctx, poolAddress, blockNumber)
			return err
		},
		ListTracked: registry.List,
		Register: func(ctx context.Context, poolAddress common.Address) error {
			return registry.Add(ctx, poolAddress)
		},
		Unregister: func(ctx context.Context, poolAddress common.Address) error {
			return registry.Remove(ctx, poolAddress)
		},
	})
}
