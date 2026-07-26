package syncv4

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

type PoolAdmissionService = syncapp.PoolAdmissionService[marketv4.PoolID]

type poolAdmissionRegistry struct{ registry marketv4.PoolRegistry }
type poolAdmissionBootstrapper struct{ bootstrap *BootstrapService }

func (b *poolAdmissionBootstrapper) Bootstrap(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) error {
	_, err := b.bootstrap.Bootstrap(ctx, poolID, blockNumber)
	return err
}

func (r *poolAdmissionRegistry) ListTracked(ctx context.Context) ([]marketv4.PoolID, error) {
	return r.registry.List(ctx)
}

func (r *poolAdmissionRegistry) Register(ctx context.Context, poolID marketv4.PoolID) error {
	key, err := r.registry.GetKey(ctx, poolID)
	if err != nil {
		return fmt.Errorf("resolve pool key: %w", err)
	}
	return r.registry.Add(ctx, poolID, key)
}

func (r *poolAdmissionRegistry) Unregister(ctx context.Context, poolID marketv4.PoolID) error {
	return r.registry.Remove(ctx, poolID)
}

func NewPoolAdmissionService(
	registry marketv4.PoolRegistry,
	bootstrap *BootstrapService,
	readiness *ReadinessService,
) *PoolAdmissionService {
	return syncapp.NewPoolAdmissionService(
		readiness,
		&poolAdmissionRegistry{registry: registry},
		&poolAdmissionBootstrapper{bootstrap: bootstrap},
	)
}
