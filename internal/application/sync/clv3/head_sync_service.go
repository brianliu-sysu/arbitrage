package clv3sync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

type HeadSyncService = syncapp.HeadSyncService[common.Address, marketclv3.PoolEvent]

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
		syncapp.HeadSyncHooks[common.Address, marketclv3.PoolEvent]{
			FetchHeadLogs: func(ctx context.Context, poolAddresses []common.Address, blockNumber uint64) ([]syncapp.RawLog, error) {
				if fetcher == nil {
					return nil, fmt.Errorf("log fetcher is not configured")
				}
				return fetcher.FetchLogs(ctx, LogFilter{
					PoolAddresses: poolAddresses,
					FromBlock:     blockNumber,
					ToBlock:       blockNumber,
				})
			},
			ParseEvents: func(logs []syncapp.RawLog) ([]marketclv3.PoolEvent, error) {
				if parser == nil {
					return nil, fmt.Errorf("event parser is not configured")
				}
				return parser.ParsePoolEvents(logs)
			},
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
			MarkPoolsReady: blockApply.MarkPoolsReady,
		},
	)
}
