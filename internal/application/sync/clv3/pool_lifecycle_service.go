package clv3sync

import (
	"context"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/ethereum/go-ethereum/common"
)

type PoolLifecycleService = syncapp.PoolLifecycleService[common.Address]

type poolLifecycleProtocol struct {
	registry  PoolRegistry
	bootstrap *BootstrapService
}

func (p *poolLifecycleProtocol) Bootstrap(ctx context.Context, poolID common.Address, blockNumber uint64) error {
	_, err := p.bootstrap.Bootstrap(ctx, poolID, blockNumber)
	return err
}

func (p *poolLifecycleProtocol) ListTracked(ctx context.Context) ([]common.Address, error) {
	return p.registry.List(ctx)
}

func (p *poolLifecycleProtocol) Register(ctx context.Context, poolID common.Address) error {
	return p.registry.Add(ctx, poolID)
}

func (p *poolLifecycleProtocol) Unregister(ctx context.Context, poolID common.Address) error {
	return p.registry.Remove(ctx, poolID)
}

func NewPoolLifecycleService(
	registry PoolRegistry,
	bootstrap *BootstrapService,
	readiness *ReadinessService,
) *PoolLifecycleService {
	return syncapp.NewPoolLifecycleService(readiness, &poolLifecycleProtocol{
		registry: registry, bootstrap: bootstrap,
	})
}
