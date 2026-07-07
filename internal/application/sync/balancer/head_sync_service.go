package balancersync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

type HeadSyncService = syncapp.HeadSyncService[marketbalancer.PoolID, marketbalancer.PoolEvent]

func NewHeadSyncService(
	fetcher LogFetcher,
	parser EventParser,
	blockApply *BlockApplyService,
	lifecycle *PoolLifecycleService,
	registry marketbalancer.PoolRegistry,
	reorg syncapp.ReorgRecovery[marketbalancer.PoolID],
	catchup *CatchupService,
	blocks BlockReader,
	subscriber HeadSubscriber,
) *HeadSyncService {
	return syncapp.NewHeadSyncService(
		lifecycle,
		reorg,
		catchup,
		blocks,
		subscriber,
		syncapp.HeadSyncHooks[marketbalancer.PoolID, marketbalancer.PoolEvent]{
			FetchHeadLogs: func(ctx context.Context, poolIDs []marketbalancer.PoolID, blockNumber uint64) ([]syncapp.RawLog, error) {
				if fetcher == nil {
					return nil, fmt.Errorf("log fetcher is not configured")
				}
				poolAddresses, err := bindBalancerPoolAddresses(ctx, registry, parser, poolIDs)
				if err != nil {
					return nil, err
				}
				return fetcher.FetchLogs(ctx, LogFilter{
					PoolIDs:       poolIDs,
					PoolAddresses: poolAddresses,
					FromBlock:     blockNumber,
					ToBlock:       blockNumber,
				})
			},
			ParseEvents: func(logs []syncapp.RawLog) ([]marketbalancer.PoolEvent, error) {
				if parser == nil {
					return nil, fmt.Errorf("event parser is not configured")
				}
				return parser.ParsePoolEvents(logs)
			},
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
			MarkPoolsReady: blockApply.MarkPoolsReady,
		},
	)
}
