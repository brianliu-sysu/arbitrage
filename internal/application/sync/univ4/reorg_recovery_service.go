package syncv4

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

type ReorgRecoveryService = syncapp.ReorgRecoveryService[marketv4.PoolID, marketv4.PoolEvent, *marketv4.Pool]

func NewReorgRecoveryService(
	config Config,
	blocks BlockReader,
	checkpoints blockchain.V4CheckpointRepository,
	pools marketv4.PoolRepository,
	registry marketv4.PoolRegistry,
	bootstrap PoolBootstrapReader,
	snapshots *SnapshotService,
	fetcher LogFetcher,
	parser EventParser,
	blockApply *BlockApplyService,
	readiness *ReadinessService,
) *ReorgRecoveryService {
	return syncapp.NewReorgRecoveryService(
		config.ReorgMaxDepth,
		blocks,
		syncapp.ReorgRecoveryHooks[marketv4.PoolID, marketv4.PoolEvent, *marketv4.Pool]{
			FormatPoolID: func(poolID marketv4.PoolID) string { return poolID.String() },
			DeleteSnapshotsAfter: func(ctx context.Context, poolID marketv4.PoolID, ancestor uint64) error {
				return snapshots.DeleteAfterBlock(ctx, poolID, ancestor)
			},
			LoadPool:  pools.Get,
			SavePool:  pools.Save,
			IsNilPool: func(pool *marketv4.Pool) bool { return pool == nil },
			RestorePoolState: func(ctx context.Context, pool *marketv4.Pool, poolID marketv4.PoolID, ancestor uint64) (uint64, error) {
				return restoreV4PoolState(ctx, snapshots, bootstrap, registry, pool, poolID, ancestor)
			},
			SetPoolStatus: func(pool *marketv4.Pool, status market.PoolStatus) { pool.Status = status },
			SetPoolReady:  readiness.SetPoolReady,
			FetchReplayLogs: func(ctx context.Context, poolID marketv4.PoolID, fromBlock, toBlock uint64) ([]syncapp.RawLog, error) {
				if fetcher == nil {
					return nil, fmt.Errorf("log fetcher is not configured")
				}
				return fetcher.FetchLogs(ctx, LogFilter{
					PoolIDs:   []marketv4.PoolID{poolID},
					FromBlock: fromBlock,
					ToBlock:   toBlock,
				})
			},
			ParseEvents: func(logs []syncapp.RawLog) ([]marketv4.PoolEvent, error) {
				if parser == nil {
					return nil, fmt.Errorf("event parser is not configured")
				}
				return parser.ParsePoolEvents(logs)
			},
			EventBlockNumber: func(event marketv4.PoolEvent) uint64 { return event.Meta.BlockNumber },
			ApplyBlock: func(ctx context.Context, blockNumber uint64, blockHash common.Hash, events []marketv4.PoolEvent, tracked []marketv4.PoolID, suppressListener bool) error {
				if blockApply == nil {
					return fmt.Errorf("block apply service is not configured")
				}
				_, err := blockApply.ApplyBlock(ctx, ApplyBlockRequest{
					BlockNumber:      blockNumber,
					BlockHash:        blockHash,
					Events:           events,
					TrackedPools:     tracked,
					SuppressListener: suppressListener,
				})
				return err
			},
			NotifyRecovered: blockApply.NotifyPoolsChanged,
		},
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
