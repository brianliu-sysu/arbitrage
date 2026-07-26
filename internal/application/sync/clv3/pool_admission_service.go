package clv3sync

import (
	"context"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/ethereum/go-ethereum/common"
)

type PoolAdmissionService = syncapp.PoolAdmissionService[common.Address]

type poolAdmissionRegistry struct{ registry PoolRegistry }
type poolAdmissionBootstrapper struct{ bootstrap *BootstrapService }

func (b *poolAdmissionBootstrapper) Bootstrap(ctx context.Context, poolID common.Address, blockNumber uint64) error {
	_, err := b.bootstrap.Bootstrap(ctx, poolID, blockNumber)
	return err
}

func (r *poolAdmissionRegistry) ListTracked(ctx context.Context) ([]common.Address, error) {
	return r.registry.List(ctx)
}

func (r *poolAdmissionRegistry) Register(ctx context.Context, poolID common.Address) error {
	return r.registry.Add(ctx, poolID)
}

func (r *poolAdmissionRegistry) Unregister(ctx context.Context, poolID common.Address) error {
	return r.registry.Remove(ctx, poolID)
}

func NewPoolAdmissionService(
	registry PoolRegistry,
	bootstrap *BootstrapService,
	readiness *ReadinessService,
) *PoolAdmissionService {
	return syncapp.NewPoolAdmissionService(
		readiness,
		&poolAdmissionRegistry{registry: registry},
		&poolAdmissionBootstrapper{bootstrap: bootstrap},
	)
}
