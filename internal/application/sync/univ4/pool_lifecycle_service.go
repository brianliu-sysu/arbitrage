package syncv4

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

type PoolLifecycleService = syncapp.PoolLifecycleService[marketv4.PoolID]

type poolLifecycleProtocol struct {
	registry  marketv4.PoolRegistry
	bootstrap *BootstrapService
}

func (p *poolLifecycleProtocol) Bootstrap(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) error {
	_, err := p.bootstrap.Bootstrap(ctx, poolID, blockNumber)
	return err
}

func (p *poolLifecycleProtocol) ListTracked(ctx context.Context) ([]marketv4.PoolID, error) {
	return p.registry.List(ctx)
}

func (p *poolLifecycleProtocol) Register(ctx context.Context, poolID marketv4.PoolID) error {
	key, err := p.registry.GetKey(ctx, poolID)
	if err != nil {
		return fmt.Errorf("resolve pool key: %w", err)
	}
	return p.registry.Add(ctx, poolID, key)
}

func (p *poolLifecycleProtocol) Unregister(ctx context.Context, poolID marketv4.PoolID) error {
	return p.registry.Remove(ctx, poolID)
}

func NewPoolLifecycleService(
	registry marketv4.PoolRegistry,
	bootstrap *BootstrapService,
	readiness *ReadinessService,
) *PoolLifecycleService {
	return syncapp.NewPoolLifecycleService(readiness, &poolLifecycleProtocol{
		registry: registry, bootstrap: bootstrap,
	})
}
