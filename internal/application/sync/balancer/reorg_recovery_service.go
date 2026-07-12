package balancersync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

type ReorgRecoveryService = syncapp.ReorgRecoveryService[marketbalancer.PoolID, marketbalancer.PoolEvent, *marketbalancer.Pool]

func NewReorgRecoveryService(
	config Config,
	blocks BlockReader,
	pools marketbalancer.PoolRepository,
	registry marketbalancer.PoolRegistry,
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
		syncapp.ReorgRecoveryHooks[marketbalancer.PoolID, marketbalancer.PoolEvent, *marketbalancer.Pool]{
			FormatPoolID: func(poolID marketbalancer.PoolID) string { return poolID.String() },
			DeleteSnapshotsAfter: func(ctx context.Context, poolID marketbalancer.PoolID, ancestor uint64) error {
				return snapshots.DeleteAfterBlock(ctx, poolID, ancestor)
			},
			LoadPool:  pools.Get,
			SavePool:  pools.Save,
			IsNilPool: func(pool *marketbalancer.Pool) bool { return pool == nil },
			RestorePoolState: func(ctx context.Context, pool *marketbalancer.Pool, poolID marketbalancer.PoolID, ancestor uint64) (uint64, error) {
				return restoreBalancerPoolState(ctx, snapshots, bootstrap, registry, pool, poolID, ancestor)
			},
			SetPoolStatus: func(pool *marketbalancer.Pool, status market.PoolStatus) { pool.Status = status },
			SetPoolReady:  readiness.SetPoolReady,
			FetchReplayLogs: func(ctx context.Context, poolID marketbalancer.PoolID, fromBlock, toBlock uint64) ([]syncapp.RawLog, error) {
				if fetcher == nil {
					return nil, fmt.Errorf("log fetcher is not configured")
				}
				binding, err := bindBalancerPools(ctx, registry, parser, []marketbalancer.PoolID{poolID})
				if err != nil {
					return nil, err
				}
				return fetcher.FetchLogs(ctx, logFilterFromBinding(binding, fromBlock, toBlock))
			},
			ParseEvents: func(logs []syncapp.RawLog) ([]marketbalancer.PoolEvent, error) {
				if parser == nil {
					return nil, fmt.Errorf("event parser is not configured")
				}
				return parser.ParsePoolEvents(logs)
			},
			EventBlockNumber: func(event marketbalancer.PoolEvent) uint64 { return event.Meta.BlockNumber },
			ApplyBlock: func(ctx context.Context, blockNumber uint64, blockHash common.Hash, events []marketbalancer.PoolEvent, tracked []marketbalancer.PoolID, suppressListener bool) error {
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
