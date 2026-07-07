package balancersync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

type BlockApplyService = syncapp.BlockApplyService[marketbalancer.PoolID, marketbalancer.PoolEvent, *marketbalancer.Pool, *blockchain.BalancerCheckpoint]

type ApplyBlockRequest = syncapp.ApplyBlockRequest[marketbalancer.PoolID, marketbalancer.PoolEvent]
type ApplyBlockResult = syncapp.ApplyBlockResult[marketbalancer.PoolID]

func NewBlockApplyService(
	pools marketbalancer.PoolRepository,
	checkpoints blockchain.BalancerCheckpointRepository,
	snapshots *SnapshotService,
	readiness *ReadinessService,
	listener ChangedPoolsListener,
) *BlockApplyService {
	if listener == nil {
		listener = NopChangedPoolsListener{}
	}
	return syncapp.NewBlockApplyService(
		syncapp.BlockApplyOptions{SkipPoolAlreadyAtBlock: true},
		syncapp.BlockApplyHooks[marketbalancer.PoolID, marketbalancer.PoolEvent, *marketbalancer.Pool, *blockchain.BalancerCheckpoint]{
			FormatPoolID: func(poolID marketbalancer.PoolID) string { return poolID.String() },
			LessPoolID:   func(a, b marketbalancer.PoolID) bool { return a.String() < b.String() },
			IsNilPool:    func(pool *marketbalancer.Pool) bool { return pool == nil },
			LoadPool:     pools.Get,
			SavePool:     pools.Save,
			AdvanceIdlePools: func(ctx context.Context, poolIDs []marketbalancer.PoolID, blockNumber uint64) error {
				return pools.AdvanceSyncProgressMany(ctx, poolIDs, blockNumber)
			},
			EventPoolID:      func(event marketbalancer.PoolEvent) marketbalancer.PoolID { return event.Meta.PoolID },
			EventTxIndex:     func(event marketbalancer.PoolEvent) uint { return event.Meta.TxIndex },
			EventLogIndex:    func(event marketbalancer.PoolEvent) uint { return event.Meta.LogIndex },
			EventBlockNumber: func(event marketbalancer.PoolEvent) uint64 { return event.Meta.BlockNumber },
			EventKind:        func(event marketbalancer.PoolEvent) string { return event.Kind.String() },
			ProtocolLabel:    "balancer",
			ApplyEvent:       func(pool *marketbalancer.Pool, event marketbalancer.PoolEvent) error { return pool.Apply(event) },
			PoolLastBlock:    func(pool *marketbalancer.Pool) uint64 { return pool.LastBlockNumber },
			SetPoolStatus:    func(pool *marketbalancer.Pool, status market.PoolStatus) { pool.Status = status },
			IsPoolAlreadyAtBlock: func(pool *marketbalancer.Pool, blockNumber uint64) bool {
				return pool.LastBlockNumber >= blockNumber
			},
			SetPoolReady: readiness.SetPoolReady,
			MaybeSnapshot: func(ctx context.Context, pool *marketbalancer.Pool, blockNumber uint64) error {
				if snapshots == nil {
					return nil
				}
				return snapshots.MaybeCreateSnapshot(ctx, pool, blockNumber)
			},
			NewCheckpoint: func(poolID marketbalancer.PoolID, blockNumber uint64, blockHash common.Hash) *blockchain.BalancerCheckpoint {
				return &blockchain.BalancerCheckpoint{
					PoolID:      poolID,
					BlockNumber: blockNumber,
					BlockHash:   blockHash,
				}
			},
			SaveCheckpoints: func(ctx context.Context, pending []*blockchain.BalancerCheckpoint) error {
				return checkpoints.SaveMany(ctx, pending)
			},
			NotifyPoolsChanged: listener.OnPoolsChanged,
			SetPoolReadyForStatus: func(ctx context.Context, poolID marketbalancer.PoolID) error {
				pool, err := pools.Get(ctx, poolID)
				if err != nil {
					return fmt.Errorf("load pool %s: %w", poolID.String(), err)
				}
				if pool == nil {
					return fmt.Errorf("pool %s not found", poolID.String())
				}
				pool.Status = market.PoolStatusReady
				if err := pools.Save(ctx, pool); err != nil {
					return fmt.Errorf("save ready pool %s: %w", poolID.String(), err)
				}
				return nil
			},
		},
	)
}
