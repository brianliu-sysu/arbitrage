package syncv4

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

type HeadSyncService = syncapp.HeadSyncService[marketv4.PoolID, marketv4.PoolEvent]

func NewHeadSyncService(
	fetcher LogFetcher,
	parser EventParser,
	blockApply *BlockApplyService,
	lifecycle *PoolLifecycleService,
	reorg *ReorgRecoveryService,
	readiness *ReadinessService,
	catchup *CatchupService,
	blocks BlockReader,
	subscriber HeadSubscriber,
) *HeadSyncService {
	_ = readiness
	return syncapp.NewHeadSyncService(
		lifecycle,
		reorg,
		catchup,
		blocks,
		subscriber,
		syncapp.HeadSyncHooks[marketv4.PoolID, marketv4.PoolEvent]{
			FetchHeadLogs: func(ctx context.Context, poolIDs []marketv4.PoolID, blockNumber uint64) ([]syncapp.RawLog, error) {
				if fetcher == nil {
					return nil, fmt.Errorf("log fetcher is not configured")
				}
				return fetcher.FetchLogs(ctx, LogFilter{
					PoolIDs:   poolIDs,
					FromBlock: blockNumber,
					ToBlock:   blockNumber,
				})
			},
			ParseEvents: func(logs []syncapp.RawLog) ([]marketv4.PoolEvent, error) {
				if parser == nil {
					return nil, fmt.Errorf("event parser is not configured")
				}
				return parser.ParsePoolEvents(logs)
			},
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
			MarkPoolsReady: blockApply.MarkPoolsReady,
		},
	)
}
