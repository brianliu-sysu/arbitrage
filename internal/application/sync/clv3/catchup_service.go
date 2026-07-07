package clv3sync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

type CatchupService = syncapp.CatchupService[common.Address, marketclv3.PoolEvent]

func NewCatchupService(
	config Config,
	pools PoolRepository,
	checkpoints blockchain.CheckpointRepository,
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
		syncapp.CatchupHooks[common.Address, marketclv3.PoolEvent]{
			FormatPoolID: func(address common.Address) string { return address.Hex() },
			LessPoolID: func(a, b common.Address) bool { return a.Hex() < b.Hex() },
			LoadStartBlock: func(ctx context.Context, poolAddress common.Address) (uint64, error) {
				return loadCLV3CatchupStartBlock(ctx, checkpoints, pools, poolAddress)
			},
			FetchLogs: func(ctx context.Context, poolAddresses []common.Address, fromBlock, toBlock uint64) ([]syncapp.RawLog, error) {
				if fetcher == nil {
					return nil, fmt.Errorf("log fetcher is not configured")
				}
				return fetcher.FetchLogs(ctx, LogFilter{
					PoolAddresses: poolAddresses,
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
			ApplyBlock: func(ctx context.Context, blockNumber uint64, blockHash common.Hash, events []marketclv3.PoolEvent, tracked []common.Address, suppressListener bool) error {
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

func loadCLV3CatchupStartBlock(
	ctx context.Context,
	checkpoints blockchain.CheckpointRepository,
	pools PoolRepository,
	poolAddress common.Address,
) (uint64, error) {
	checkpoint, err := checkpoints.Get(ctx, poolAddress)
	if err != nil {
		return 0, fmt.Errorf("load checkpoint: %w", err)
	}

	pool, err := pools.Get(ctx, poolAddress)
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
