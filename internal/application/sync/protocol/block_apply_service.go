package protocol

import (
	"context"
	"errors"
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

// BlockApplySnapshot captures protocol state before applying one shared block.
type BlockApplySnapshot[PoolID comparable, Pool any, Checkpoint any] struct {
	pools                map[PoolID]Pool
	checkpoints          map[PoolID]Checkpoint
	hasCheckpoint        map[PoolID]bool
	readiness            map[PoolID]bool
	blockNumbers         map[PoolID]uint64
	poolRepository       BlockApplyPoolRepository[PoolID, Pool]
	checkpointRepository BlockApplyCheckpointRepository[PoolID, Checkpoint]
	snapshots            BlockApplySnapshotService[PoolID, Pool]
	poolReadiness        PoolReadiness[PoolID]
}

// BlockApplyService applies pool events for a single block.
type BlockApplyService[PoolID comparable, Event any, Pool any, Checkpoint any] struct {
	options  BlockApplyOptions
	deps     BlockApplyDeps[PoolID, Pool, Checkpoint]
	protocol BlockApplyProtocol[PoolID, Event, Pool, Checkpoint]
	logger   *zap.Logger
}

// NewBlockApplyService builds a block apply service.
func NewBlockApplyService[PoolID comparable, Event any, Pool any, Checkpoint any](
	options BlockApplyOptions,
	deps BlockApplyDeps[PoolID, Pool, Checkpoint],
	protocol BlockApplyProtocol[PoolID, Event, Pool, Checkpoint],
) *BlockApplyService[PoolID, Event, Pool, Checkpoint] {
	return &BlockApplyService[PoolID, Event, Pool, Checkpoint]{
		options:  options,
		deps:     deps,
		protocol: protocol,
		logger:   zap.NewNop(),
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
	s.deps.Listener = listener
}

// NotifyPoolsChanged reports one completed protocol-level block application.
func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) NotifyPoolsChanged(
	ctx context.Context,
	blockNumber uint64,
	poolIDs []PoolID,
) error {
	if s == nil || s.deps.Listener == nil {
		return nil
	}
	return s.deps.Listener.OnPoolsChanged(ctx, blockNumber, poolIDs)
}

// CaptureState snapshots all tracked protocol state that ApplyBlock can mutate.
func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) CaptureState(
	ctx context.Context,
	poolIDs []PoolID,
) (*BlockApplySnapshot[PoolID, Pool, Checkpoint], error) {
	snapshot := &BlockApplySnapshot[PoolID, Pool, Checkpoint]{
		pools:                make(map[PoolID]Pool, len(poolIDs)),
		checkpoints:          make(map[PoolID]Checkpoint, len(poolIDs)),
		hasCheckpoint:        make(map[PoolID]bool, len(poolIDs)),
		readiness:            make(map[PoolID]bool, len(poolIDs)),
		blockNumbers:         make(map[PoolID]uint64, len(poolIDs)),
		poolRepository:       s.deps.Pools,
		checkpointRepository: s.deps.Checkpoints,
		snapshots:            s.deps.Snapshots,
		poolReadiness:        s.deps.Readiness,
	}
	for _, poolID := range poolIDs {
		pool, err := s.deps.Pools.Get(ctx, poolID)
		if err != nil {
			return nil, fmt.Errorf("snapshot pool %s: %w", s.protocol.FormatPoolID(poolID), err)
		}
		if s.protocol.IsNilPool(pool) {
			return nil, fmt.Errorf("snapshot pool %s: pool not found", s.protocol.FormatPoolID(poolID))
		}
		snapshot.pools[poolID] = pool
		snapshot.blockNumbers[poolID] = s.protocol.PoolLastBlock(pool)
		if s.deps.Readiness != nil {
			snapshot.readiness[poolID] = s.deps.Readiness.IsPoolReady(poolID)
		}
		checkpoint, err := s.deps.Checkpoints.Get(ctx, poolID)
		if err != nil {
			return nil, fmt.Errorf("snapshot checkpoint %s: %w", s.protocol.FormatPoolID(poolID), err)
		}
		if s.protocol.IsNilCheckpoint(checkpoint) {
			continue
		}
		snapshot.checkpoints[poolID] = checkpoint
		snapshot.hasCheckpoint[poolID] = true
	}
	return snapshot, nil
}

// CaptureRecoveryState captures pool state for transactional reorg preparation.
func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) CaptureRecoveryState(
	ctx context.Context,
	poolIDs []PoolID,
) (ReorgRecoveryState, error) {
	return s.CaptureState(ctx, poolIDs)
}

// Restore replaces protocol state with the values captured before ApplyBlock.
func (s *BlockApplySnapshot[PoolID, Pool, Checkpoint]) Restore(ctx context.Context) error {
	var restoreErr error
	for poolID, pool := range s.pools {
		if err := s.poolRepository.Save(ctx, pool); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore pool: %w", err))
		}
		if s.hasCheckpoint[poolID] {
			if err := s.checkpointRepository.Save(ctx, s.checkpoints[poolID]); err != nil {
				restoreErr = errors.Join(restoreErr, fmt.Errorf("restore checkpoint: %w", err))
			}
		} else {
			if err := s.checkpointRepository.Delete(ctx, poolID); err != nil {
				restoreErr = errors.Join(restoreErr, fmt.Errorf("delete new checkpoint: %w", err))
			}
		}
		if s.poolReadiness != nil {
			s.poolReadiness.SetPoolReady(poolID, s.readiness[poolID])
		}
		if s.snapshots != nil {
			if err := s.snapshots.DeleteAfterBlock(ctx, poolID, s.blockNumbers[poolID]); err != nil {
				restoreErr = errors.Join(restoreErr, fmt.Errorf("delete new snapshots: %w", err))
			}
		}
	}
	return restoreErr
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

	eventsByPool := s.groupApplicableEvents(req)
	appliedPools, err := s.applyChangedPools(ctx, req, eventsByPool)
	if err != nil {
		return zero, err
	}

	advancedPools, err := s.advanceIdlePools(ctx, req.BlockNumber, req.TrackedPools, eventsByPool)
	if err != nil {
		return zero, err
	}

	changedPools := make([]PoolID, 0, len(appliedPools)+len(advancedPools))
	changedPools = append(changedPools, appliedPools...)
	changedPools = append(changedPools, advancedPools...)
	if err := s.saveBlockCheckpoints(ctx, req, changedPools); err != nil {
		return zero, err
	}

	s.sortPoolIDs(changedPools)
	s.sortPoolIDs(appliedPools)
	if err := s.notifyAppliedPools(ctx, req, appliedPools); err != nil {
		return zero, err
	}

	return ApplyBlockResult[PoolID]{
		BlockNumber:  req.BlockNumber,
		ChangedPools: changedPools,
	}, nil
}

func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) groupApplicableEvents(
	req ApplyBlockRequest[PoolID, Event],
) map[PoolID][]Event {
	var trackedSet map[PoolID]struct{}
	if s.options.FilterUntrackedEvents {
		trackedSet = make(map[PoolID]struct{}, len(req.TrackedPools))
		for _, poolID := range req.TrackedPools {
			trackedSet[poolID] = struct{}{}
		}
	}

	eventsByPool := make(map[PoolID][]Event)
	for _, event := range req.Events {
		poolID := s.protocol.DescribeEvent(event).PoolID
		if s.options.FilterUntrackedEvents {
			if _, tracked := trackedSet[poolID]; !tracked {
				continue
			}
		}
		eventsByPool[poolID] = append(eventsByPool[poolID], event)
	}
	for _, events := range eventsByPool {
		s.sortEvents(events)
	}
	return eventsByPool
}

func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) sortEvents(events []Event) {
	sort.Slice(events, func(i, j int) bool {
		left := s.protocol.DescribeEvent(events[i])
		right := s.protocol.DescribeEvent(events[j])
		if left.TxIndex != right.TxIndex {
			return left.TxIndex < right.TxIndex
		}
		return left.LogIndex < right.LogIndex
	})
}

func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) applyChangedPools(
	ctx context.Context,
	req ApplyBlockRequest[PoolID, Event],
	eventsByPool map[PoolID][]Event,
) ([]PoolID, error) {
	appliedPools := make([]PoolID, 0, len(eventsByPool))
	for poolID, events := range eventsByPool {
		if err := s.applyPoolBlock(ctx, req, poolID, events); err != nil {
			return nil, err
		}
		appliedPools = append(appliedPools, poolID)
	}
	return appliedPools, nil
}

func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) applyPoolBlock(
	ctx context.Context,
	req ApplyBlockRequest[PoolID, Event],
	poolID PoolID,
	events []Event,
) error {
	pool, err := s.deps.Pools.Get(ctx, poolID)
	if err != nil {
		return fmt.Errorf("load pool %s: %w", s.protocol.FormatPoolID(poolID), err)
	}
	if s.protocol.IsNilPool(pool) {
		return fmt.Errorf("pool %s not found", s.protocol.FormatPoolID(poolID))
	}

	if s.options.SkipPoolAlreadyAtBlock && s.protocol.PoolLastBlock(pool) >= req.BlockNumber {
		s.logPoolAlreadyApplied(poolID, pool, req.BlockNumber)
		return s.maybeSnapshot(ctx, poolID, pool, req.BlockNumber)
	}

	if err := s.applyPoolEvents(ctx, poolID, pool, events, req.BlockNumber); err != nil {
		return err
	}
	if after, ok := s.protocol.(AfterPoolApply[PoolID, Pool]); ok {
		if err := after.AfterApplyPool(ctx, poolID, pool, req.BlockNumber); err != nil {
			s.markPoolApplyFailed(ctx, poolID, pool)
			return fmt.Errorf("after apply pool %s: %w", s.protocol.FormatPoolID(poolID), err)
		}
	}
	if err := s.deps.Pools.Save(ctx, pool); err != nil {
		return fmt.Errorf("save pool %s: %w", s.protocol.FormatPoolID(poolID), err)
	}
	return s.maybeSnapshot(ctx, poolID, pool, req.BlockNumber)
}

func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) applyPoolEvents(
	ctx context.Context,
	poolID PoolID,
	pool Pool,
	events []Event,
	blockNumber uint64,
) error {
	poolLastBlock := s.protocol.PoolLastBlock(pool)
	for _, event := range events {
		meta := s.protocol.DescribeEvent(event)
		skipped := meta.BlockNumber < poolLastBlock
		s.logPoolEvent(poolID, event, meta, blockNumber, poolLastBlock, skipped)
		if err := s.protocol.ApplyEvent(pool, event); err != nil {
			s.markPoolApplyFailed(ctx, poolID, pool)
			return fmt.Errorf("apply event on pool %s: %w", s.protocol.FormatPoolID(poolID), err)
		}
		if !skipped {
			s.logPoolState(poolID, pool, event, meta, blockNumber)
		}
		poolLastBlock = s.protocol.PoolLastBlock(pool)
	}
	return nil
}

func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) logPoolAlreadyApplied(
	poolID PoolID,
	pool Pool,
	blockNumber uint64,
) {
	s.logger.Debug("pool block already applied",
		zap.String("protocol", s.protocol.Label()),
		zap.String("pool", s.protocol.FormatPoolID(poolID)),
		zap.Uint64("block", blockNumber),
		zap.Uint64("poolLastBlock", s.protocol.PoolLastBlock(pool)),
	)
}

func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) logPoolEvent(
	poolID PoolID,
	event Event,
	meta EventMetadata[PoolID],
	blockNumber uint64,
	poolLastBlock uint64,
	skipped bool,
) {
	fields := []zap.Field{
		zap.String("protocol", s.protocol.Label()),
		zap.String("pool", s.protocol.FormatPoolID(poolID)),
		zap.Uint64("block", blockNumber),
		zap.Uint64("eventBlock", meta.BlockNumber),
		zap.String("kind", meta.Kind),
		zap.Uint("txIndex", meta.TxIndex),
		zap.Uint("logIndex", meta.LogIndex),
		zap.Uint64("poolLastBlock", poolLastBlock),
		zap.Bool("skipped", skipped),
	}
	if enricher, ok := s.protocol.(EventLogEnricher[Event]); ok {
		fields = append(fields, enricher.EventLogFields(event)...)
	}
	s.logger.Debug("pool event", fields...)
}

func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) logPoolState(
	poolID PoolID,
	pool Pool,
	event Event,
	meta EventMetadata[PoolID],
	blockNumber uint64,
) {
	enricher, ok := s.protocol.(PoolStateLogEnricher[Pool, Event])
	if !ok {
		return
	}
	stateFields := enricher.PoolStateLogFields(pool, event)
	if len(stateFields) == 0 {
		return
	}
	fields := []zap.Field{
		zap.String("protocol", s.protocol.Label()),
		zap.String("pool", s.protocol.FormatPoolID(poolID)),
		zap.Uint64("block", blockNumber),
		zap.String("kind", meta.Kind),
	}
	s.logger.Debug("pool state", append(fields, stateFields...)...)
}

func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) markPoolApplyFailed(
	ctx context.Context,
	poolID PoolID,
	pool Pool,
) {
	s.protocol.SetPoolStatus(pool, market.PoolStatusError)
	_ = s.deps.Pools.Save(ctx, pool)
	s.setPoolReady(poolID, false)
}

func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) maybeSnapshot(
	ctx context.Context,
	poolID PoolID,
	pool Pool,
	blockNumber uint64,
) error {
	if s.deps.Snapshots == nil {
		return nil
	}
	if err := s.deps.Snapshots.MaybeSnapshot(ctx, pool, blockNumber); err != nil {
		return fmt.Errorf("snapshot pool %s: %w", s.protocol.FormatPoolID(poolID), err)
	}
	return nil
}

func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) advanceIdlePools(
	ctx context.Context,
	blockNumber uint64,
	trackedPools []PoolID,
	eventsByPool map[PoolID][]Event,
) ([]PoolID, error) {
	idlePools := make([]PoolID, 0, len(trackedPools))
	for _, poolID := range trackedPools {
		if _, changed := eventsByPool[poolID]; changed {
			continue
		}
		idlePools = append(idlePools, poolID)
	}
	if len(idlePools) > 0 {
		if err := s.deps.Pools.AdvanceSyncProgressMany(ctx, idlePools, blockNumber); err != nil {
			return nil, fmt.Errorf("advance sync progress: %w", err)
		}
	}
	return idlePools, nil
}

func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) saveBlockCheckpoints(
	ctx context.Context,
	req ApplyBlockRequest[PoolID, Event],
	poolIDs []PoolID,
) error {
	if len(poolIDs) == 0 {
		return nil
	}
	checkpoints := make([]Checkpoint, 0, len(poolIDs))
	for _, poolID := range poolIDs {
		checkpoints = append(checkpoints, s.protocol.NewCheckpoint(poolID, req.BlockNumber, req.BlockHash))
	}
	if err := s.deps.Checkpoints.SaveMany(ctx, checkpoints); err != nil {
		return fmt.Errorf("save checkpoints: %w", err)
	}
	return nil
}

func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) notifyAppliedPools(
	ctx context.Context,
	req ApplyBlockRequest[PoolID, Event],
	appliedPools []PoolID,
) error {
	if !req.SuppressListener && s.deps.Listener != nil {
		if err := s.deps.Listener.OnPoolsChanged(ctx, req.BlockNumber, appliedPools); err != nil {
			return fmt.Errorf("notify changed pools: %w", err)
		}
	}
	return nil
}

func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) sortPoolIDs(poolIDs []PoolID) {
	sort.Slice(poolIDs, func(i, j int) bool {
		return s.protocol.FormatPoolID(poolIDs[i]) < s.protocol.FormatPoolID(poolIDs[j])
	})
}

// MarkPoolsReady marks pools as ready and updates readiness tracking.
func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) MarkPoolsReady(ctx context.Context, poolIDs []PoolID) error {
	for _, poolID := range poolIDs {
		pool, err := s.deps.Pools.Get(ctx, poolID)
		if err != nil {
			return fmt.Errorf("load pool %s: %w", s.protocol.FormatPoolID(poolID), err)
		}
		if s.protocol.IsNilPool(pool) {
			return fmt.Errorf("pool %s not found", s.protocol.FormatPoolID(poolID))
		}
		s.protocol.SetPoolStatus(pool, market.PoolStatusReady)
		if err := s.deps.Pools.Save(ctx, pool); err != nil {
			return fmt.Errorf("save ready pool %s: %w", s.protocol.FormatPoolID(poolID), err)
		}
		if s.deps.Readiness != nil {
			s.deps.Readiness.SetPoolReady(poolID, true)
		}
	}
	return nil
}

func (s *BlockApplyService[PoolID, Event, Pool, Checkpoint]) setPoolReady(poolID PoolID, ready bool) {
	if s.deps.Readiness != nil {
		s.deps.Readiness.SetPoolReady(poolID, ready)
	}
}
