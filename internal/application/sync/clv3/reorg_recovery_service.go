package clv3sync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

type ReorgRecoveryService = syncapp.ReorgRecoveryService[common.Address, marketclv3.PoolEvent, *marketclv3.Pool]

func NewReorgRecoveryService(
	config Config,
	blocks BlockReader,
	checkpoints blockchain.CheckpointRepository,
	pools PoolRepository,
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
		syncapp.ReorgRecoveryHooks[common.Address, marketclv3.PoolEvent, *marketclv3.Pool]{
			FormatPoolID: func(address common.Address) string { return address.Hex() },
			DeleteSnapshotsAfter: func(ctx context.Context, poolAddress common.Address, ancestor uint64) error {
				return snapshots.DeleteAfterBlock(ctx, poolAddress, ancestor)
			},
			LoadPool:  pools.Get,
			SavePool:  pools.Save,
			IsNilPool: func(pool *marketclv3.Pool) bool { return pool == nil },
			RestorePoolState: func(ctx context.Context, pool *marketclv3.Pool, poolAddress common.Address, ancestor uint64) (uint64, error) {
				return restoreCLV3PoolState(ctx, snapshots, bootstrap, pool, poolAddress, ancestor)
			},
			SetPoolStatus: func(pool *marketclv3.Pool, status market.PoolStatus) { pool.Status = status },
			SetPoolReady:  readiness.SetPoolReady,
			FetchReplayLogs: func(ctx context.Context, poolAddress common.Address, fromBlock, toBlock uint64) ([]syncapp.RawLog, error) {
				if fetcher == nil {
					return nil, fmt.Errorf("log fetcher is not configured")
				}
				return fetcher.FetchLogs(ctx, LogFilter{
					PoolAddresses: []common.Address{poolAddress},
					FromBlock:     fromBlock,
					ToBlock:       toBlock,
				})
			},
			ParseEvents: func(logs []syncapp.RawLog) ([]marketclv3.PoolEvent, error) {
				if parser == nil {
					return nil, fmt.Errorf("event parser is not configured")
				}
				return parser.ParsePoolEvents(logs)
			},
			EventBlockNumber: func(event marketclv3.PoolEvent) uint64 { return event.Meta.BlockNumber },
			ApplyBlock: func(ctx context.Context, blockNumber uint64, blockHash common.Hash, events []marketclv3.PoolEvent, tracked []common.Address) error {
				if blockApply == nil {
					return fmt.Errorf("block apply service is not configured")
				}
				_, err := blockApply.ApplyBlock(ctx, ApplyBlockRequest{
					BlockNumber:  blockNumber,
					BlockHash:    blockHash,
					Events:       events,
					TrackedPools: tracked,
				})
				return err
			},
		},
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
