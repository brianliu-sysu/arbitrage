package clv3sync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

type BlockConsumer = syncapp.BlockConsumer[common.Address, marketclv3.PoolEvent]

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

func (a *blockConsumerAdapter) FilterHeadLogs(
	ctx context.Context,
	pools []common.Address,
	logs []syncapp.RawLog,
) ([]syncapp.RawLog, error) {
	return syncapp.FilterLogsByTrackedAddresses(ctx, pools, logs)
}

func (a *blockConsumerAdapter) ParseEvents(logs []syncapp.RawLog) ([]marketclv3.PoolEvent, error) {
	if a.parser == nil {
		return nil, fmt.Errorf("event parser is not configured")
	}
	return a.parser.ParsePoolEvents(logs)
}

func (a *blockConsumerAdapter) CaptureState(
	ctx context.Context,
	pools []common.Address,
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
	req syncapp.ApplyBlockRequest[common.Address, marketclv3.PoolEvent],
) error {
	if a.blockApply == nil {
		return fmt.Errorf("block apply service is not configured")
	}
	_, err := a.blockApply.ApplyBlock(ctx, req)
	return err
}

func (a *blockConsumerAdapter) MarkPoolsReady(ctx context.Context, pools []common.Address) error {
	if a.blockApply == nil {
		return fmt.Errorf("block apply service is not configured")
	}
	return a.blockApply.MarkPoolsReady(ctx, pools)
}
