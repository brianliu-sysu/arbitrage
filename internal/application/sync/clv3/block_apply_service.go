package clv3sync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

type BlockApplyService = syncapp.BlockApplyService[common.Address, marketclv3.PoolEvent, *marketclv3.Pool, *blockchain.Checkpoint]

type ApplyBlockRequest = syncapp.ApplyBlockRequest[common.Address, marketclv3.PoolEvent]
type ApplyBlockResult = syncapp.ApplyBlockResult[common.Address]

func NewBlockApplyService(
	pools PoolRepository,
	checkpoints blockchain.CheckpointRepository,
	snapshots *SnapshotService,
	readiness *ReadinessService,
	listener ChangedPoolsListener,
) *BlockApplyService {
	if listener == nil {
		listener = NopChangedPoolsListener{}
	}
	return syncapp.NewBlockApplyService(
		syncapp.BlockApplyOptions{},
		syncapp.BlockApplyHooks[common.Address, marketclv3.PoolEvent, *marketclv3.Pool, *blockchain.Checkpoint]{
			FormatPoolID: func(address common.Address) string { return address.Hex() },
			LessPoolID:   func(a, b common.Address) bool { return a.Hex() < b.Hex() },
			IsNilPool:    func(pool *marketclv3.Pool) bool { return pool == nil },
			LoadPool:     pools.Get,
			SavePool:     pools.Save,
			AdvanceIdlePools: func(ctx context.Context, addresses []common.Address, blockNumber uint64) error {
				return pools.AdvanceSyncProgressMany(ctx, addresses, blockNumber)
			},
			EventPoolID:      func(event marketclv3.PoolEvent) common.Address { return event.Meta.PoolAddress },
			EventTxIndex:     func(event marketclv3.PoolEvent) uint { return event.Meta.TxIndex },
			EventLogIndex:    func(event marketclv3.PoolEvent) uint { return event.Meta.LogIndex },
			EventBlockNumber: func(event marketclv3.PoolEvent) uint64 { return event.Meta.BlockNumber },
			EventKind:        func(event marketclv3.PoolEvent) string { return event.Kind.String() },
			ProtocolLabel:    "clv3",
			ApplyEvent:       func(pool *marketclv3.Pool, event marketclv3.PoolEvent) error { return pool.Apply(event) },
			PoolLastBlock:    func(pool *marketclv3.Pool) uint64 { return pool.LastBlockNumber },
			SetPoolStatus:    func(pool *marketclv3.Pool, status market.PoolStatus) { pool.Status = status },
			IsPoolAlreadyAtBlock: func(*marketclv3.Pool, uint64) bool { return false },
			SetPoolReady: readiness.SetPoolReady,
			MaybeSnapshot: func(ctx context.Context, pool *marketclv3.Pool, blockNumber uint64) error {
				if snapshots == nil {
					return nil
				}
				return snapshots.MaybeCreateSnapshot(ctx, pool, blockNumber)
			},
			NewCheckpoint: func(poolAddress common.Address, blockNumber uint64, blockHash common.Hash) *blockchain.Checkpoint {
				return &blockchain.Checkpoint{
					PoolAddress: poolAddress,
					BlockNumber: blockNumber,
					BlockHash:   blockHash,
				}
			},
			SaveCheckpoints: func(ctx context.Context, pending []*blockchain.Checkpoint) error {
				return checkpoints.SaveMany(ctx, pending)
			},
			NotifyPoolsChanged: listener.OnPoolsChanged,
			SetPoolReadyForStatus: func(ctx context.Context, poolAddress common.Address) error {
				pool, err := pools.Get(ctx, poolAddress)
				if err != nil {
					return fmt.Errorf("load pool %s: %w", poolAddress.Hex(), err)
				}
				if pool == nil {
					return fmt.Errorf("pool %s not found", poolAddress.Hex())
				}
				pool.Status = market.PoolStatusReady
				if err := pools.Save(ctx, pool); err != nil {
					return fmt.Errorf("save ready pool %s: %w", poolAddress.Hex(), err)
				}
				return nil
			},
		},
	)
}
