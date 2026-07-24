package syncv4

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

type BlockConsumer = syncapp.BlockConsumer[marketv4.PoolID, marketv4.PoolEvent]

type blockConsumerAdapter struct {
	parser     EventParser
	blockApply *BlockApplyService
}

func NewBlockConsumer(
	parser EventParser,
	blockApply *BlockApplyService,
	lifecycle *PoolLifecycleService,
	reorg *ReorgRecoveryService,
) *BlockConsumer {
	adapter := &blockConsumerAdapter{parser: parser, blockApply: blockApply}
	return syncapp.NewBlockConsumer(
		lifecycle,
		reorg,
		adapter,
	)
}

func (a *blockConsumerAdapter) FilterHeadLogs(_ context.Context, poolIDs []marketv4.PoolID, logs []syncapp.RawLog) ([]syncapp.RawLog, error) {
	tracked := make(map[common.Hash]struct{}, len(poolIDs))
	for _, poolID := range poolIDs {
		tracked[poolID.Hash()] = struct{}{}
	}
	filtered := make([]syncapp.RawLog, 0, len(logs))
	for _, log := range logs {
		if len(log.Topics) < 2 {
			continue
		}
		if _, ok := tracked[log.Topics[1]]; ok {
			filtered = append(filtered, log)
		}
	}
	return filtered, nil
}

func (a *blockConsumerAdapter) ParseEvents(logs []syncapp.RawLog) ([]marketv4.PoolEvent, error) {
	if a.parser == nil {
		return nil, fmt.Errorf("event parser is not configured")
	}
	return a.parser.ParsePoolEvents(logs)
}

func (a *blockConsumerAdapter) CaptureState(
	ctx context.Context,
	pools []marketv4.PoolID,
) (syncapp.ReorgRecoveryState, error) {
	if a.blockApply == nil {
		return nil, fmt.Errorf("block apply service is not configured")
	}
	snapshot, err := a.blockApply.CaptureState(ctx, pools)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (a *blockConsumerAdapter) ApplyBlock(
	ctx context.Context,
	req syncapp.ApplyBlockRequest[marketv4.PoolID, marketv4.PoolEvent],
) error {
	if a.blockApply == nil {
		return fmt.Errorf("block apply service is not configured")
	}
	_, err := a.blockApply.ApplyBlock(ctx, req)
	return err
}

func (a *blockConsumerAdapter) MarkPoolsReady(ctx context.Context, pools []marketv4.PoolID) error {
	if a.blockApply == nil {
		return fmt.Errorf("block apply service is not configured")
	}
	return a.blockApply.MarkPoolsReady(ctx, pools)
}
