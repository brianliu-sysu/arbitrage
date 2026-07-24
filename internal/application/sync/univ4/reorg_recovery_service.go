package syncv4

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

type ReorgRecoveryService = syncapp.ReorgRecoveryService[marketv4.PoolID, *marketv4.Pool]

type reorgRecoveryProtocol struct {
	snapshots *SnapshotService
	bootstrap PoolBootstrapReader
	registry  marketv4.PoolRegistry
}

func (p *reorgRecoveryProtocol) FormatPoolID(poolID marketv4.PoolID) string {
	return poolID.String()
}

func (p *reorgRecoveryProtocol) IsNilPool(pool *marketv4.Pool) bool {
	return pool == nil
}

func (p *reorgRecoveryProtocol) DeleteSnapshotsAfter(
	ctx context.Context,
	poolID marketv4.PoolID,
	blockNumber uint64,
) error {
	return p.snapshots.DeleteAfterBlock(ctx, poolID, blockNumber)
}

func (p *reorgRecoveryProtocol) RestorePoolState(
	ctx context.Context,
	pool *marketv4.Pool,
	poolID marketv4.PoolID,
	ancestor uint64,
) (uint64, error) {
	return restoreV4PoolState(ctx, p.snapshots, p.bootstrap, p.registry, pool, poolID, ancestor)
}

func (p *reorgRecoveryProtocol) SetPoolStatus(pool *marketv4.Pool, status market.PoolStatus) {
	pool.Status = status
}

func NewReorgRecoveryService(
	pools marketv4.PoolRepository,
	registry marketv4.PoolRegistry,
	bootstrap PoolBootstrapReader,
	snapshots *SnapshotService,
	blockApply *BlockApplyService,
	readiness *ReadinessService,
) *ReorgRecoveryService {
	return syncapp.NewReorgRecoveryService(
		syncapp.ReorgRecoveryDeps[marketv4.PoolID, *marketv4.Pool]{
			Pools:       pools,
			Coordinator: blockApply,
			Readiness:   readiness,
		},
		&reorgRecoveryProtocol{snapshots: snapshots, bootstrap: bootstrap, registry: registry},
	)
}

func restoreV4PoolState(
	ctx context.Context,
	snapshots *SnapshotService,
	bootstrap PoolBootstrapReader,
	registry marketv4.PoolRegistry,
	pool *marketv4.Pool,
	poolID marketv4.PoolID,
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
		key, err := registry.GetKey(ctx, poolID)
		if err != nil {
			return 0, fmt.Errorf("resolve pool key: %w", err)
		}
		data, err := bootstrap.ReadBootstrapData(ctx, poolID, key, ancestor)
		if err != nil {
			return 0, fmt.Errorf("read chain bootstrap data: %w", err)
		}
		pool.Key = data.Key
		applyBootstrapData(pool, data)
		pool.LastBlockNumber = ancestor
		return syncapp.ReorgReplayFromBlock(0, ancestor, false), nil
	}

	pool.LastBlockNumber = ancestor
	return syncapp.ReorgReplayFromBlock(0, ancestor, false), nil
}
