package balancersync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

type CatchupService = syncapp.CatchupService[marketbalancer.PoolID, marketbalancer.PoolEvent]

func NewCatchupService(
	config Config,
	pools marketbalancer.PoolRepository,
	checkpoints blockchain.BalancerCheckpointRepository,
	registry marketbalancer.PoolRegistry,
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
		syncapp.CatchupHooks[marketbalancer.PoolID, marketbalancer.PoolEvent]{
			FormatPoolID: func(poolID marketbalancer.PoolID) string { return poolID.String() },
			LessPoolID:   func(a, b marketbalancer.PoolID) bool { return a.String() < b.String() },
			LoadStartBlock: func(ctx context.Context, poolID marketbalancer.PoolID) (uint64, error) {
				return loadBalancerCatchupStartBlock(ctx, checkpoints, pools, poolID)
			},
			FetchLogs: func(ctx context.Context, poolIDs []marketbalancer.PoolID, fromBlock, toBlock uint64) ([]syncapp.RawLog, error) {
				if fetcher == nil {
					return nil, fmt.Errorf("log fetcher is not configured")
				}
				binding, err := bindBalancerPools(ctx, registry, parser, poolIDs)
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
		},
	)
}

func loadBalancerCatchupStartBlock(
	ctx context.Context,
	checkpoints blockchain.BalancerCheckpointRepository,
	pools marketbalancer.PoolRepository,
	poolID marketbalancer.PoolID,
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
