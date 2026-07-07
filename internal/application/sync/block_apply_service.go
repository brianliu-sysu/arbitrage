package syncapp

import (
	"context"
	"fmt"
	"sort"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// ApplyBlockRequest carries inputs for a single block application pass.
type ApplyBlockRequest[PoolID comparable, Event any] struct {
	BlockNumber      uint64
	BlockHash        common.Hash
	Events           []Event
	TrackedPools     []PoolID
	SuppressListener bool
}

// ApplyBlockResult lists pools touched during block application.
type ApplyBlockResult[PoolID comparable] struct {
	BlockNumber  uint64
	ChangedPools []PoolID
}

// BlockApplyOptions toggles protocol-specific block apply behavior.
type BlockApplyOptions struct {
	FilterUntrackedEvents  bool
	SkipPoolAlreadyAtBlock bool
}

// BlockApplyHooks configures protocol-specific block application behavior.
type BlockApplyHooks[PoolID comparable, Event any, Pool any, Checkpoint any] struct {
	FormatPoolID func(PoolID) string
	LessPoolID   func(a, b PoolID) bool

	IsNilPool        func(Pool) bool
	LoadPool         func(context.Context, PoolID) (Pool, error)
	SavePool         func(context.Context, Pool) error
	AdvanceIdlePools func(context.Context, []PoolID, uint64) error

	EventPoolID                  func(Event) PoolID
	EventTxIndex                 func(Event) uint
	EventLogIndex                func(Event) uint
	EventBlockNumber             func(Event) uint64
	EventKind                    func(Event) string
	ProtocolLabel                string
	ExtraEventLogFields          func(Event) []zap.Field
	PoolStateAfterApplyLogFields func(Pool, Event, bool) []zap.Field

	ApplyEvent            func(Pool, Event) error
	AfterApplyPool        func(context.Context, PoolID, Pool, uint64) error
	PoolLastBlock         func(Pool) uint64
	SetPoolStatus         func(Pool, market.PoolStatus)
	IsPoolAlreadyAtBlock  func(Pool, uint64) bool
	SetPoolReady          func(PoolID, bool)
	MaybeSnapshot         func(context.Context, Pool, uint64) error
	NewCheckpoint         func(PoolID, uint64, common.Hash) Checkpoint
	SaveCheckpoints       func(context.Context, []Checkpoint) error
	NotifyPoolsChanged    func(context.Context, uint64, []PoolID) error
	SetPoolReadyForStatus func(context.Context, PoolID) error
}

// BlockApplyService applies pool events for a single block.
type BlockApplyService[PoolID comparable, Event any, Pool any, Checkpoint any] struct {
	options BlockApplyOptions
	hooks   BlockApplyHooks[PoolID, Event, Pool, Checkpoint]
	logger  *zap.Logger
}

// NewBlockApplyService builds a block apply service with protocol hooks.
func NewBlockApplyService[PoolID comparable, Event any, Pool any, Checkpoint any](
	options BlockApplyOptions,
	hooks BlockApplyHooks[PoolID, Event, Pool, Checkpoint],
) *BlockApplyService[PoolID, Event, Pool, Checkpoint] {
	return &BlockApplyService[PoolID, Event, Pool, Checkpoint]{
		options: options,
		hooks:   hooks,
		logger:  zap.NewNop(),
	}
}

// PoolsChangedNotifier receives pools updated after a block is applied.
type PoolsChangedNotifier[PoolID comparable] interface {
	OnPoolsChanged(context.Context, uint64, []PoolID) error
}

// SetLogger configures debug logging for pool event application.
func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) SetLogger(logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	s.logger = logger
}

// SetListener replaces the pool change listener, typically during application wiring.
func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) SetListener(listener PoolsChangedNotifier[PoolID]) {
	if listener == nil {
		s.hooks.NotifyPoolsChanged = func(context.Context, uint64, []PoolID) error { return nil }
		return
	}
	s.hooks.NotifyPoolsChanged = listener.OnPoolsChanged
}

// ApplyBlock applies events and advances sync progress for tracked pools.
func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) ApplyBlock(
	ctx context.Context,
	req ApplyBlockRequest[PoolID, Event],
) (ApplyBlockResult[PoolID], error) {
	var zero ApplyBlockResult[PoolID]
	if req.BlockNumber == 0 {
		return zero, fmt.Errorf("block number must be positive")
	}

	trackedSet := make(map[PoolID]struct{}, len(req.TrackedPools))
	if s.options.FilterUntrackedEvents {
		for _, poolID := range req.TrackedPools {
			trackedSet[poolID] = struct{}{}
		}
	}

	grouped := GroupEventsByPool(req.Events, s.hooks.EventPoolID)
	changedSet := make(map[PoolID]struct{}, len(grouped))
	changed := make([]PoolID, 0, len(req.TrackedPools))
	pendingCheckpoints := make([]Checkpoint, 0, len(req.TrackedPools))

	for poolID, events := range grouped {
		if s.options.FilterUntrackedEvents {
			if _, ok := trackedSet[poolID]; !ok {
				continue
			}
		}
		changedSet[poolID] = struct{}{}
		sort.Slice(events, func(i, j int) bool {
			if s.hooks.EventTxIndex(events[i]) != s.hooks.EventTxIndex(events[j]) {
				return s.hooks.EventTxIndex(events[i]) < s.hooks.EventTxIndex(events[j])
			}
			return s.hooks.EventLogIndex(events[i]) < s.hooks.EventLogIndex(events[j])
		})

		pool, err := s.hooks.LoadPool(ctx, poolID)
		if err != nil {
			return zero, fmt.Errorf("load pool %s: %w", s.hooks.FormatPoolID(poolID), err)
		}
		if s.hooks.IsNilPool(pool) {
			return zero, fmt.Errorf("pool %s not found", s.hooks.FormatPoolID(poolID))
		}

		if s.options.SkipPoolAlreadyAtBlock && s.hooks.IsPoolAlreadyAtBlock(pool, req.BlockNumber) {
			s.logger.Debug("pool block already applied",
				zap.String("protocol", s.hooks.ProtocolLabel),
				zap.String("pool", s.hooks.FormatPoolID(poolID)),
				zap.Uint64("block", req.BlockNumber),
				zap.Uint64("poolLastBlock", s.hooks.PoolLastBlock(pool)),
			)
			pendingCheckpoints = append(pendingCheckpoints, s.hooks.NewCheckpoint(poolID, req.BlockNumber, req.BlockHash))
			if s.hooks.MaybeSnapshot != nil {
				if err := s.hooks.MaybeSnapshot(ctx, pool, req.BlockNumber); err != nil {
					return zero, fmt.Errorf("snapshot pool %s: %w", s.hooks.FormatPoolID(poolID), err)
				}
			}
			changed = append(changed, poolID)
			continue
		}

		poolLastBlock := s.hooks.PoolLastBlock(pool)
		for _, event := range events {
			skipped := s.hooks.EventBlockNumber(event) < poolLastBlock
			fields := []zap.Field{
				zap.String("protocol", s.hooks.ProtocolLabel),
				zap.String("pool", s.hooks.FormatPoolID(poolID)),
				zap.Uint64("block", req.BlockNumber),
				zap.Uint64("eventBlock", s.hooks.EventBlockNumber(event)),
				zap.String("kind", s.hooks.EventKind(event)),
				zap.Uint("txIndex", s.hooks.EventTxIndex(event)),
				zap.Uint("logIndex", s.hooks.EventLogIndex(event)),
				zap.Uint64("poolLastBlock", poolLastBlock),
				zap.Bool("skipped", skipped),
			}
			if s.hooks.ExtraEventLogFields != nil {
				fields = append(fields, s.hooks.ExtraEventLogFields(event)...)
			}
			s.logger.Debug("pool event", fields...)

			if err := s.hooks.ApplyEvent(pool, event); err != nil {
				s.hooks.SetPoolStatus(pool, market.PoolStatusError)
				_ = s.hooks.SavePool(ctx, pool)
				s.hooks.SetPoolReady(poolID, false)
				return zero, fmt.Errorf("apply event on pool %s: %w", s.hooks.FormatPoolID(poolID), err)
			}
			if s.hooks.PoolStateAfterApplyLogFields != nil && !skipped {
				stateFields := s.hooks.PoolStateAfterApplyLogFields(pool, event, skipped)
				if len(stateFields) > 0 {
					base := []zap.Field{
						zap.String("protocol", s.hooks.ProtocolLabel),
						zap.String("pool", s.hooks.FormatPoolID(poolID)),
						zap.Uint64("block", req.BlockNumber),
						zap.String("kind", s.hooks.EventKind(event)),
					}
					s.logger.Debug("pool state", append(base, stateFields...)...)
				}
			}
			poolLastBlock = s.hooks.PoolLastBlock(pool)
		}

		if s.hooks.AfterApplyPool != nil {
			if err := s.hooks.AfterApplyPool(ctx, poolID, pool, req.BlockNumber); err != nil {
				s.hooks.SetPoolStatus(pool, market.PoolStatusError)
				_ = s.hooks.SavePool(ctx, pool)
				s.hooks.SetPoolReady(poolID, false)
				return zero, fmt.Errorf("after apply pool %s: %w", s.hooks.FormatPoolID(poolID), err)
			}
		}

		if err := s.hooks.SavePool(ctx, pool); err != nil {
			return zero, fmt.Errorf("save pool %s: %w", s.hooks.FormatPoolID(poolID), err)
		}
		pendingCheckpoints = append(pendingCheckpoints, s.hooks.NewCheckpoint(poolID, req.BlockNumber, req.BlockHash))

		if s.hooks.MaybeSnapshot != nil {
			if err := s.hooks.MaybeSnapshot(ctx, pool, req.BlockNumber); err != nil {
				return zero, fmt.Errorf("snapshot pool %s: %w", s.hooks.FormatPoolID(poolID), err)
			}
		}

		changed = append(changed, poolID)
	}

	idlePools := make([]PoolID, 0, len(req.TrackedPools))
	for _, poolID := range req.TrackedPools {
		if _, ok := changedSet[poolID]; ok {
			continue
		}
		idlePools = append(idlePools, poolID)
	}
	if len(idlePools) > 0 {
		if err := s.hooks.AdvanceIdlePools(ctx, idlePools, req.BlockNumber); err != nil {
			return zero, fmt.Errorf("advance sync progress: %w", err)
		}
		for _, poolID := range idlePools {
			pendingCheckpoints = append(pendingCheckpoints, s.hooks.NewCheckpoint(poolID, req.BlockNumber, req.BlockHash))
		}
		changed = append(changed, idlePools...)
	}

	if len(pendingCheckpoints) > 0 {
		if err := s.hooks.SaveCheckpoints(ctx, pendingCheckpoints); err != nil {
			return zero, fmt.Errorf("save checkpoints: %w", err)
		}
	}

	sort.Slice(changed, func(i, j int) bool {
		return s.hooks.LessPoolID(changed[i], changed[j])
	})

	if len(changed) > 0 && !req.SuppressListener && s.hooks.NotifyPoolsChanged != nil {
		if err := s.hooks.NotifyPoolsChanged(ctx, req.BlockNumber, changed); err != nil {
			return zero, fmt.Errorf("notify changed pools: %w", err)
		}
	}

	return ApplyBlockResult[PoolID]{
		BlockNumber:  req.BlockNumber,
		ChangedPools: changed,
	}, nil
}

// MarkPoolsReady marks pools as ready and updates readiness tracking.
func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) MarkPoolsReady(ctx context.Context, poolIDs []PoolID) error {
	for _, poolID := range poolIDs {
		if err := s.hooks.SetPoolReadyForStatus(ctx, poolID); err != nil {
			return err
		}
		s.hooks.SetPoolReady(poolID, true)
	}
	return nil
}

// GroupEventsByPool groups events by their pool identifier.
func GroupEventsByPool[PoolID comparable, Event any](events []Event, poolID func(Event) PoolID) map[PoolID][]Event {
	grouped := make(map[PoolID][]Event)
	for _, event := range events {
		id := poolID(event)
		grouped[id] = append(grouped[id], event)
	}
	return grouped
}
