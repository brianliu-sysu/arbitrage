package balancersync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

type BlockConsumer = syncapp.BlockConsumer[marketbalancer.PoolID, marketbalancer.PoolEvent]

type blockConsumerAdapter struct {
	parser     EventParser
	blockApply *BlockApplyService
	registry   marketbalancer.PoolRegistry
}

func NewBlockConsumer(
	parser EventParser,
	blockApply *BlockApplyService,
	lifecycle *PoolLifecycleService,
	registry marketbalancer.PoolRegistry,
	reorg syncapp.ReorgRecovery[marketbalancer.PoolID],
) *BlockConsumer {
	adapter := &blockConsumerAdapter{parser: parser, blockApply: blockApply, registry: registry}
	return syncapp.NewBlockConsumer(
		lifecycle,
		reorg,
		adapter,
	)
}

func (a *blockConsumerAdapter) FilterHeadLogs(ctx context.Context, poolIDs []marketbalancer.PoolID, logs []syncapp.RawLog) ([]syncapp.RawLog, error) {
	binding, err := bindBalancerPools(ctx, a.registry, a.parser, poolIDs)
	if err != nil {
		return nil, err
	}
	trackedIDs := make(map[common.Hash]struct{}, len(binding.V2PoolIDs))
	for _, poolID := range binding.V2PoolIDs {
		trackedIDs[poolID.Hash()] = struct{}{}
	}
	for _, address := range binding.V3PoolAddresses {
		trackedIDs[common.BytesToHash(common.LeftPadBytes(address.Bytes(), 32))] = struct{}{}
	}
	trackedAddresses := make(map[common.Address]struct{}, len(binding.PoolIDByAddress))
	for address := range binding.PoolIDByAddress {
		trackedAddresses[address] = struct{}{}
	}
	filtered := make([]syncapp.RawLog, 0, len(logs))
	for _, log := range logs {
		if _, ok := trackedAddresses[log.Address]; ok {
			filtered = append(filtered, log)
			continue
		}
		if len(log.Topics) < 2 {
			continue
		}
		if _, ok := trackedIDs[log.Topics[1]]; ok {
			filtered = append(filtered, log)
		}
	}
	return filtered, nil
}

func (a *blockConsumerAdapter) ParseEvents(logs []syncapp.RawLog) ([]marketbalancer.PoolEvent, error) {
	if a.parser == nil {
		return nil, fmt.Errorf("event parser is not configured")
	}
	return a.parser.ParsePoolEvents(logs)
}

func (a *blockConsumerAdapter) CaptureState(
	ctx context.Context,
	pools []marketbalancer.PoolID,
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
	req syncapp.ApplyBlockRequest[marketbalancer.PoolID, marketbalancer.PoolEvent],
) error {
	if a.blockApply == nil {
		return fmt.Errorf("block apply service is not configured")
	}
	_, err := a.blockApply.ApplyBlock(ctx, req)
	return err
}

func (a *blockConsumerAdapter) MarkPoolsReady(ctx context.Context, pools []marketbalancer.PoolID) error {
	if a.blockApply == nil {
		return fmt.Errorf("block apply service is not configured")
	}
	return a.blockApply.MarkPoolsReady(ctx, pools)
}
