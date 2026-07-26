package balancersync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
)

type PoolAdmissionService = syncapp.PoolAdmissionService[marketbalancer.PoolID]

type poolAdmissionRegistry struct{ registry marketbalancer.PoolRegistry }
type poolAdmissionBootstrapper struct{ bootstrap *BootstrapService }

func (b *poolAdmissionBootstrapper) Bootstrap(ctx context.Context, poolID marketbalancer.PoolID, blockNumber uint64) error {
	_, err := b.bootstrap.Bootstrap(ctx, poolID, blockNumber)
	return err
}

func (b *poolAdmissionBootstrapper) BootstrapAll(
	ctx context.Context,
	poolIDs []marketbalancer.PoolID,
	blockNumber uint64,
) error {
	return b.bootstrap.BootstrapAll(ctx, poolIDs, blockNumber)
}

func (r *poolAdmissionRegistry) ListTracked(ctx context.Context) ([]marketbalancer.PoolID, error) {
	return r.registry.List(ctx)
}

func (r *poolAdmissionRegistry) Register(ctx context.Context, poolID marketbalancer.PoolID) error {
	spec, err := r.registry.GetSpec(ctx, poolID)
	if err != nil {
		return fmt.Errorf("resolve pool spec: %w", err)
	}
	return r.registry.Add(ctx, poolID, spec)
}

func (r *poolAdmissionRegistry) Unregister(ctx context.Context, poolID marketbalancer.PoolID) error {
	return r.registry.Remove(ctx, poolID)
}

func NewPoolAdmissionService(
	readiness *ReadinessService,
	registry marketbalancer.PoolRegistry,
	bootstrap *BootstrapService,
) *PoolAdmissionService {
	return syncapp.NewPoolAdmissionService(
		readiness,
		&poolAdmissionRegistry{registry: registry},
		&poolAdmissionBootstrapper{bootstrap: bootstrap},
	)
}
