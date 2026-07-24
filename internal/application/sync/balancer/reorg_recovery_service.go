package balancersync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
)

type ReorgRecoveryService = syncapp.ReorgRecoveryService[marketbalancer.PoolID, *marketbalancer.Pool]

type reorgRecoveryProtocol struct {
	snapshots *SnapshotService
	bootstrap PoolBootstrapReader
	registry  marketbalancer.PoolRegistry
}

func (p *reorgRecoveryProtocol) FormatPoolID(poolID marketbalancer.PoolID) string {
	return poolID.String()
}

func (p *reorgRecoveryProtocol) IsNilPool(pool *marketbalancer.Pool) bool {
	return pool == nil
}

func (p *reorgRecoveryProtocol) DeleteSnapshotsAfter(
	ctx context.Context,
	poolID marketbalancer.PoolID,
	blockNumber uint64,
) error {
	return p.snapshots.DeleteAfterBlock(ctx, poolID, blockNumber)
}

func (p *reorgRecoveryProtocol) RestorePoolState(
	ctx context.Context,
	pool *marketbalancer.Pool,
	poolID marketbalancer.PoolID,
	ancestor uint64,
) (uint64, error) {
	return restoreBalancerPoolState(ctx, p.snapshots, p.bootstrap, p.registry, pool, poolID, ancestor)
}

func (p *reorgRecoveryProtocol) SetPoolStatus(pool *marketbalancer.Pool, status market.PoolStatus) {
	pool.Status = status
}

func NewReorgRecoveryService(
	pools marketbalancer.PoolRepository,
	registry marketbalancer.PoolRegistry,
	bootstrap PoolBootstrapReader,
	snapshots *SnapshotService,
	blockApply *BlockApplyService,
	readiness *ReadinessService,
) *ReorgRecoveryService {
	return syncapp.NewReorgRecoveryService(
		syncapp.ReorgRecoveryDeps[marketbalancer.PoolID, *marketbalancer.Pool]{
			Pools:       pools,
			Coordinator: blockApply,
			Readiness:   readiness,
		},
		&reorgRecoveryProtocol{snapshots: snapshots, bootstrap: bootstrap, registry: registry},
	)
}

func restoreBalancerPoolState(
	ctx context.Context,
	snapshots *SnapshotService,
	bootstrap PoolBootstrapReader,
	registry marketbalancer.PoolRegistry,
	pool *marketbalancer.Pool,
	poolID marketbalancer.PoolID,
	ancestor uint64,
) (uint64, error) {
	snapshot, err := snapshots.LoadAtOrBefore(ctx, poolID, ancestor)
	if err != nil {
		return 0, err
	}
	if snapshot != nil {
		snapshot.RestoreTo(pool)
		pool.LastBlockNumber = snapshot.BlockNumber
		return syncapp.ReorgReplayFromBlock(snapshot.BlockNumber, ancestor, true), nil
	}

	if bootstrap != nil && registry != nil {
		spec, err := registry.GetSpec(ctx, poolID)
		if err != nil {
			return 0, fmt.Errorf("resolve pool spec: %w", err)
		}
		data, err := bootstrap.ReadBootstrapData(ctx, poolID, spec, ancestor)
		if err != nil {
			return 0, fmt.Errorf("read chain bootstrap data: %w", err)
		}
		applyBootstrapData(pool, data)
		pool.LastBlockNumber = ancestor
		return syncapp.ReorgReplayFromBlock(0, ancestor, false), nil
	}

	pool.LastBlockNumber = ancestor
	return syncapp.ReorgReplayFromBlock(0, ancestor, false), nil
}
