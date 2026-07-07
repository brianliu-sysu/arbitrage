package syncv4

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

type BlockApplyService = syncapp.BlockApplyService[marketv4.PoolID, marketv4.PoolEvent, *marketv4.Pool, *blockchain.V4Checkpoint]

type ApplyBlockRequest = syncapp.ApplyBlockRequest[marketv4.PoolID, marketv4.PoolEvent]
type ApplyBlockResult = syncapp.ApplyBlockResult[marketv4.PoolID]

func NewBlockApplyService(
	pools marketv4.PoolRepository,
	checkpoints blockchain.V4CheckpointRepository,
	snapshots *SnapshotService,
	readiness *ReadinessService,
	listener ChangedPoolsListener,
) *BlockApplyService {
	if listener == nil {
		listener = NopChangedPoolsListener{}
	}
	return syncapp.NewBlockApplyService(
		syncapp.BlockApplyOptions{
			FilterUntrackedEvents:  true,
			SkipPoolAlreadyAtBlock: true,
		},
		syncapp.BlockApplyHooks[marketv4.PoolID, marketv4.PoolEvent, *marketv4.Pool, *blockchain.V4Checkpoint]{
			FormatPoolID: func(poolID marketv4.PoolID) string { return poolID.String() },
			LessPoolID:   func(a, b marketv4.PoolID) bool { return a.String() < b.String() },
			IsNilPool:    func(pool *marketv4.Pool) bool { return pool == nil },
			LoadPool:     pools.Get,
			SavePool:     pools.Save,
			AdvanceIdlePools: func(ctx context.Context, poolIDs []marketv4.PoolID, blockNumber uint64) error {
				return pools.AdvanceSyncProgressMany(ctx, poolIDs, blockNumber)
			},
			EventPoolID:      func(event marketv4.PoolEvent) marketv4.PoolID { return event.Meta.PoolID },
			EventTxIndex:     func(event marketv4.PoolEvent) uint { return event.Meta.TxIndex },
			EventLogIndex:    func(event marketv4.PoolEvent) uint { return event.Meta.LogIndex },
			EventBlockNumber: func(event marketv4.PoolEvent) uint64 { return event.Meta.BlockNumber },
			EventKind:        func(event marketv4.PoolEvent) string { return event.Kind.String() },
			ProtocolLabel:    "univ4",
			ExtraEventLogFields: v4EventLogFields,
			PoolStateAfterApplyLogFields: func(pool *marketv4.Pool, _ marketv4.PoolEvent, skipped bool) []zap.Field {
				if skipped {
					return nil
				}
				return syncapp.PoolStateLogFields(pool.State, pool.LastBlockNumber, pool.Status)
			},
			ApplyEvent: func(pool *marketv4.Pool, event marketv4.PoolEvent) error { return pool.Apply(event) },
			PoolLastBlock: func(pool *marketv4.Pool) uint64 { return pool.LastBlockNumber },
			SetPoolStatus: func(pool *marketv4.Pool, status market.PoolStatus) { pool.Status = status },
			IsPoolAlreadyAtBlock: func(pool *marketv4.Pool, blockNumber uint64) bool {
				return pool.LastBlockNumber >= blockNumber
			},
			SetPoolReady: readiness.SetPoolReady,
			MaybeSnapshot: func(ctx context.Context, pool *marketv4.Pool, blockNumber uint64) error {
				if snapshots == nil {
					return nil
				}
				return snapshots.MaybeCreateSnapshot(ctx, pool, blockNumber)
			},
			NewCheckpoint: func(poolID marketv4.PoolID, blockNumber uint64, blockHash common.Hash) *blockchain.V4Checkpoint {
				return &blockchain.V4Checkpoint{
					PoolID:      poolID,
					BlockNumber: blockNumber,
					BlockHash:   blockHash,
				}
			},
			SaveCheckpoints: func(ctx context.Context, pending []*blockchain.V4Checkpoint) error {
				return checkpoints.SaveMany(ctx, pending)
			},
			NotifyPoolsChanged: listener.OnPoolsChanged,
			SetPoolReadyForStatus: func(ctx context.Context, poolID marketv4.PoolID) error {
				pool, err := pools.Get(ctx, poolID)
				if err != nil {
					return fmt.Errorf("load pool %s: %w", poolID, err)
				}
				if pool == nil {
					return fmt.Errorf("pool %s not found", poolID)
				}
				pool.Status = market.PoolStatusReady
				if err := pools.Save(ctx, pool); err != nil {
					return fmt.Errorf("save ready pool %s: %w", poolID, err)
				}
				return nil
			},
		},
	)
}
