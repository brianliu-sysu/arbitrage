package protocol

import (
	"context"
	"fmt"
	"sync"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum/common"
)

// ReorgRecovery restores protocol state after a chain-level reorg is detected.
type ReorgRecovery[PoolID comparable] interface {
	Prepare(ctx context.Context, reorg blockchain.Reorg, pools []PoolID) (ReorgPlan[PoolID], error)
}

type ReorgPlan[PoolID comparable] interface {
	ReplayFrom() uint64
	PoolsForBlock(uint64) []PoolID
	Commit(context.Context) error
	Rollback(context.Context) error
}

// BlockConsumerProtocol defines protocol-specific shared block consumption.
type BlockConsumerProtocol[PoolID comparable, Event any] interface {
	FilterHeadLogs(context.Context, []PoolID, []RawLog) ([]RawLog, error)
	ParseEvents([]RawLog) ([]Event, error)
	CaptureState(context.Context, []PoolID) (ReorgRecoveryState, error)
	ApplyBlock(context.Context, ApplyBlockRequest[PoolID, Event]) error
	MarkPoolsReady(context.Context, []PoolID) error
}

// BlockConsumer applies shared block logs to one protocol's state.
type BlockConsumer[PoolID comparable, Event any] struct {
	lifecycle *PoolLifecycleService[PoolID]
	reorg     ReorgRecovery[PoolID]
	protocol  BlockConsumerProtocol[PoolID, Event]

	handleMu sync.Mutex
}

// FilterLogsByTrackedAddresses keeps logs emitted by tracked address-based pools.
func FilterLogsByTrackedAddresses(_ context.Context, addresses []common.Address, logs []RawLog) ([]RawLog, error) {
	tracked := make(map[common.Address]struct{}, len(addresses))
	for _, address := range addresses {
		tracked[address] = struct{}{}
	}
	filtered := make([]RawLog, 0, len(logs))
	for _, log := range logs {
		if _, ok := tracked[log.Address]; ok {
			filtered = append(filtered, log)
		}
	}
	return filtered, nil
}

func NewBlockConsumer[PoolID comparable, Event any](
	lifecycle *PoolLifecycleService[PoolID],
	reorg ReorgRecovery[PoolID],
	protocol BlockConsumerProtocol[PoolID, Event],
) *BlockConsumer[PoolID, Event] {
	return &BlockConsumer[PoolID, Event]{
		lifecycle: lifecycle,
		reorg:     reorg,
		protocol:  protocol,
	}
}

// WithBlockConsumptionPaused serializes management with shared block consumption.
func (s *BlockConsumer[PoolID, Event]) WithBlockConsumptionPaused(ctx context.Context, fn func(context.Context) error) error {
	if fn == nil {
		return nil
	}
	s.handleMu.Lock()
	defer s.handleMu.Unlock()
	return fn(ctx)
}

// HandleBlock consumes logs fetched once by the shared-head runner.
func (s *BlockConsumer[PoolID, Event]) HandleBlock(ctx context.Context, head blockchain.BlockHeader, logs []RawLog) error {
	prepared, err := s.PrepareBlock(ctx, head, logs)
	if err != nil {
		return err
	}
	return prepared.Apply(ctx)
}

// PrepareBlock filters and parses protocol logs without mutating pool state.
func (s *BlockConsumer[PoolID, Event]) PrepareBlock(ctx context.Context, head blockchain.BlockHeader, sharedLogs []RawLog) (PreparedBlock, error) {
	pools := s.lifecycle.ListActive()
	return s.prepareBlockForPools(ctx, head, sharedLogs, pools, true, false)
}

func (s *BlockConsumer[PoolID, Event]) prepareBlockForPools(
	ctx context.Context,
	head blockchain.BlockHeader,
	sharedLogs []RawLog,
	pools []PoolID,
	markReady bool,
	suppressListener bool,
) (PreparedBlock, error) {
	logs, err := s.protocol.FilterHeadLogs(ctx, pools, sharedLogs)
	if err != nil {
		return nil, fmt.Errorf("filter shared logs for head %d: %w", head.Number, err)
	}
	events, err := s.protocol.ParseEvents(logs)
	if err != nil {
		return nil, fmt.Errorf("parse events for head %d: %w", head.Number, err)
	}
	state, err := s.protocol.CaptureState(ctx, pools)
	if err != nil {
		return nil, fmt.Errorf("capture state for head %d: %w", head.Number, err)
	}
	return newPreparedBlock(func(ctx context.Context) error {
		return s.applyPreparedBlock(ctx, head, events, pools, markReady, suppressListener)
	}, func(ctx context.Context) error {
		if state == nil {
			return nil
		}
		return state.Restore(ctx)
	}), nil
}

func (s *BlockConsumer[PoolID, Event]) applyPreparedBlock(
	ctx context.Context,
	head blockchain.BlockHeader,
	events []Event,
	pools []PoolID,
	markReady bool,
	suppressListener bool,
) error {
	s.handleMu.Lock()
	defer s.handleMu.Unlock()
	if err := s.protocol.ApplyBlock(ctx, ApplyBlockRequest[PoolID, Event]{
		BlockNumber:      head.Number,
		BlockHash:        head.Hash,
		Events:           events,
		TrackedPools:     pools,
		SuppressListener: suppressListener,
	}); err != nil {
		return fmt.Errorf("apply head block %d: %w", head.Number, err)
	}
	if markReady {
		if err := s.protocol.MarkPoolsReady(ctx, pools); err != nil {
			return fmt.Errorf("mark pools ready: %w", err)
		}
	}
	return nil
}

type preparedConsumerReorg[PoolID comparable, Event any] struct {
	consumer *BlockConsumer[PoolID, Event]
	plan     ReorgPlan[PoolID]
}

func (p *preparedConsumerReorg[PoolID, Event]) ReplayFrom() uint64 {
	return p.plan.ReplayFrom()
}

func (p *preparedConsumerReorg[PoolID, Event]) PrepareBlock(
	ctx context.Context,
	head blockchain.BlockHeader,
	logs []RawLog,
) (PreparedBlock, error) {
	pools := p.plan.PoolsForBlock(head.Number)
	if len(pools) == 0 {
		return newPreparedBlock(func(context.Context) error { return nil }, nil), nil
	}
	return p.consumer.prepareBlockForPools(ctx, head, logs, pools, false, true)
}

func (p *preparedConsumerReorg[PoolID, Event]) Commit(ctx context.Context) error {
	return p.plan.Commit(ctx)
}

func (p *preparedConsumerReorg[PoolID, Event]) Rollback(ctx context.Context) error {
	return p.plan.Rollback(ctx)
}

func (s *BlockConsumer[PoolID, Event]) PrepareReorg(
	ctx context.Context,
	reorg blockchain.Reorg,
) (PreparedReorg, error) {
	if s.reorg == nil {
		return nil, fmt.Errorf("reorg recovery service is not configured")
	}
	var plan ReorgPlan[PoolID]
	err := s.WithBlockConsumptionPaused(ctx, func(ctx context.Context) error {
		var err error
		plan, err = s.reorg.Prepare(ctx, reorg, s.lifecycle.ListActive())
		return err
	})
	if err != nil {
		return nil, err
	}
	return &preparedConsumerReorg[PoolID, Event]{consumer: s, plan: plan}, nil
}
