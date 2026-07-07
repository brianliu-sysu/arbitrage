package syncv4

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

type CatchupService = syncapp.CatchupService[marketv4.PoolID, marketv4.PoolEvent]

func NewCatchupService(
	config Config,
	pools marketv4.PoolRepository,
	checkpoints blockchain.V4CheckpointRepository,
	fetcher LogFetcher,
	parser EventParser,
	blockApply *BlockApplyService,
	lifecycle *PoolLifecycleService,
	blocks BlockReader,
) *CatchupService {
	return syncapp.NewCatchupService(
		config,
		lifecycle,
		blocks,
		syncapp.CatchupHooks[marketv4.PoolID, marketv4.PoolEvent]{
			FormatPoolID: func(poolID marketv4.PoolID) string { return poolID.String() },
			LessPoolID:   func(a, b marketv4.PoolID) bool { return a.String() < b.String() },
			LoadStartBlock: func(ctx context.Context, poolID marketv4.PoolID) (uint64, error) {
				return loadV4CatchupStartBlock(ctx, checkpoints, pools, poolID)
			},
			FetchLogs: func(ctx context.Context, poolIDs []marketv4.PoolID, fromBlock, toBlock uint64) ([]syncapp.RawLog, error) {
				if fetcher == nil {
					return nil, fmt.Errorf("log fetcher is not configured")
				}
				return fetcher.FetchLogs(ctx, LogFilter{
					PoolIDs:   poolIDs,
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
		},
	)
}

func loadV4CatchupStartBlock(
	ctx context.Context,
	checkpoints blockchain.V4CheckpointRepository,
	pools marketv4.PoolRepository,
	poolID marketv4.PoolID,
) (uint64, error) {
	checkpoint, err := checkpoints.Get(ctx, poolID)
	if err != nil {
		return 0, fmt.Errorf("load checkpoint: %w", err)
	}

	pool, err := pools.Get(ctx, poolID)
	if err != nil {
		return 0, fmt.Errorf("load pool: %w", err)
	}

	var checkpointBlock uint64
	if checkpoint != nil {
		checkpointBlock = checkpoint.BlockNumber
	}
	var poolLastBlock uint64
	if pool != nil {
		poolLastBlock = pool.LastBlockNumber
	}
	return syncapp.CatchupStartBlock(checkpointBlock, poolLastBlock), nil
}
