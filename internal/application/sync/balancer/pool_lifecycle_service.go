package balancersync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
)

type PoolLifecycleService = syncapp.PoolLifecycleService[marketbalancer.PoolID]

type poolLifecycleProtocol struct {
	registry  marketbalancer.PoolRegistry
	bootstrap *BootstrapService
}

func (p *poolLifecycleProtocol) Bootstrap(ctx context.Context, poolID marketbalancer.PoolID, blockNumber uint64) error {
	_, err := p.bootstrap.Bootstrap(ctx, poolID, blockNumber)
	return err
}

func (p *poolLifecycleProtocol) BootstrapAll(
	ctx context.Context,
	poolIDs []marketbalancer.PoolID,
	blockNumber uint64,
) error {
	return p.bootstrap.BootstrapAll(ctx, poolIDs, blockNumber)
}

func (p *poolLifecycleProtocol) ListTracked(ctx context.Context) ([]marketbalancer.PoolID, error) {
	return p.registry.List(ctx)
}

func (p *poolLifecycleProtocol) Register(ctx context.Context, poolID marketbalancer.PoolID) error {
	spec, err := p.registry.GetSpec(ctx, poolID)
	if err != nil {
		return fmt.Errorf("resolve pool spec: %w", err)
	}
	return p.registry.Add(ctx, poolID, spec)
}

func (p *poolLifecycleProtocol) Unregister(ctx context.Context, poolID marketbalancer.PoolID) error {
	return p.registry.Remove(ctx, poolID)
}

func NewPoolLifecycleService(
	readiness *ReadinessService,
	registry marketbalancer.PoolRegistry,
	bootstrap *BootstrapService,
) *PoolLifecycleService {
	return syncapp.NewPoolLifecycleService(readiness, &poolLifecycleProtocol{
		registry: registry, bootstrap: bootstrap,
	})
}
