package clv3sync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

type ReorgRecoveryService = syncapp.ReorgRecoveryService[common.Address, *marketclv3.Pool]

type reorgRecoveryProtocol struct {
	snapshots *SnapshotService
	bootstrap PoolBootstrapReader
}

func (p *reorgRecoveryProtocol) FormatPoolID(poolID common.Address) string {
	return poolID.Hex()
}

func (p *reorgRecoveryProtocol) IsNilPool(pool *marketclv3.Pool) bool {
	return pool == nil
}

func (p *reorgRecoveryProtocol) DeleteSnapshotsAfter(
	ctx context.Context,
	poolID common.Address,
	blockNumber uint64,
) error {
	return p.snapshots.DeleteAfterBlock(ctx, poolID, blockNumber)
}

func (p *reorgRecoveryProtocol) RestorePoolState(
	ctx context.Context,
	pool *marketclv3.Pool,
	poolID common.Address,
	ancestor uint64,
) (uint64, error) {
	return restoreCLV3PoolState(ctx, p.snapshots, p.bootstrap, pool, poolID, ancestor)
}

func (p *reorgRecoveryProtocol) SetPoolStatus(pool *marketclv3.Pool, status market.PoolStatus) {
	pool.Status = status
}

func NewReorgRecoveryService(
	pools PoolRepository,
	bootstrap PoolBootstrapReader,
	snapshots *SnapshotService,
	blockApply *BlockApplyService,
	readiness *ReadinessService,
) *ReorgRecoveryService {
	return syncapp.NewReorgRecoveryService(
		syncapp.ReorgRecoveryDeps[common.Address, *marketclv3.Pool]{
			Pools:       pools,
			Coordinator: blockApply,
			Readiness:   readiness,
		},
		&reorgRecoveryProtocol{snapshots: snapshots, bootstrap: bootstrap},
	)
}

func restoreCLV3PoolState(
	ctx context.Context,
	snapshots *SnapshotService,
	bootstrap PoolBootstrapReader,
	pool *marketclv3.Pool,
	poolAddress common.Address,
	ancestor uint64,
) (uint64, error) {
	snapshot, err := snapshots.LoadAtOrBefore(ctx, poolAddress, ancestor)
	if err != nil {
		return 0, err
	}
	if snapshot != nil {
		snapshot.RestoreTo(pool)
		pool.LastBlockNumber = snapshot.BlockNumber
		return syncapp.ReorgReplayFromBlock(snapshot.BlockNumber, ancestor, true), nil
	}

	if bootstrap != nil {
		data, err := bootstrap.ReadBootstrapData(ctx, poolAddress, ancestor)
		if err != nil {
			return 0, fmt.Errorf("read chain bootstrap data: %w", err)
		}
		pool.Token0 = data.Token0
		pool.Token1 = data.Token1
		pool.Fee = data.Fee
		pool.TickSpacing = data.TickSpacing
		applyBootstrapData(pool, data)
		pool.LastBlockNumber = ancestor
		return syncapp.ReorgReplayFromBlock(0, ancestor, false), nil
	}

	pool.LastBlockNumber = ancestor
	return syncapp.ReorgReplayFromBlock(0, ancestor, false), nil
}
