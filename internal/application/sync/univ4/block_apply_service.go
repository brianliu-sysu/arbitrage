package syncv4

import (
	"context"
	"fmt"
	"sort"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// BlockApplyService applies V4 pool events for a single block.
type BlockApplyService struct {
	pools       marketv4.PoolRepository
	checkpoints blockchain.V4CheckpointRepository
	snapshots   *SnapshotService
	readiness   *ReadinessService
	listener    ChangedPoolsListener
	logger      *zap.Logger
}

func NewBlockApplyService(
	pools marketv4.PoolRepository,
	checkpoints blockchain.V4CheckpointRepository,
	snapshots *SnapshotService,
	readiness *ReadinessService,
	listener ChangedPoolsListener,
) *BlockApplyService {
	if listener == nil {
		listener = NopChangedPoolsListener{}
	}
	return &BlockApplyService{
		pools:       pools,
		checkpoints: checkpoints,
		snapshots:   snapshots,
		readiness:   readiness,
		listener:    listener,
		logger:      zap.NewNop(),
	}
}

// SetLogger configures debug logging for pool event application.
func (s *BlockApplyService) SetLogger(logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	s.logger = logger
}

func (s *BlockApplyService) SetListener(listener ChangedPoolsListener) {
	if listener == nil {
		listener = NopChangedPoolsListener{}
	}
	s.listener = listener
}

type ApplyBlockRequest struct {
	BlockNumber      uint64
	BlockHash        common.Hash
	Events           []marketv4.PoolEvent
	TrackedPools     []marketv4.PoolID
	SuppressListener bool
}

type ApplyBlockResult struct {
	BlockNumber  uint64
	ChangedPools []marketv4.PoolID
}

func (s *BlockApplyService) ApplyBlock(ctx context.Context, req ApplyBlockRequest) (ApplyBlockResult, error) {
	if req.BlockNumber == 0 {
		return ApplyBlockResult{}, fmt.Errorf("block number must be positive")
	}

	grouped := groupEventsByPool(req.Events)
	changedSet := make(map[marketv4.PoolID]struct{}, len(grouped))
	changed := make([]marketv4.PoolID, 0, len(req.TrackedPools))
	pendingCheckpoints := make([]*blockchain.V4Checkpoint, 0, len(req.TrackedPools))

	for poolID, events := range grouped {
		changedSet[poolID] = struct{}{}
		sort.Slice(events, func(i, j int) bool {
			if events[i].Meta.TxIndex != events[j].Meta.TxIndex {
				return events[i].Meta.TxIndex < events[j].Meta.TxIndex
			}
			return events[i].Meta.LogIndex < events[j].Meta.LogIndex
		})

		pool, err := s.pools.Get(ctx, poolID)
		if err != nil {
			return ApplyBlockResult{}, fmt.Errorf("load pool %s: %w", poolID, err)
		}
		if pool == nil {
			return ApplyBlockResult{}, fmt.Errorf("pool %s not found", poolID)
		}

		if pool.LastBlockNumber >= req.BlockNumber {
			s.logger.Debug("pool block already applied",
				zap.String("protocol", "univ4"),
				zap.String("pool", poolID.String()),
				zap.Uint64("block", req.BlockNumber),
				zap.Uint64("poolLastBlock", pool.LastBlockNumber),
			)
			pendingCheckpoints = append(pendingCheckpoints, newCheckpoint(poolID, req.BlockNumber, req.BlockHash))
			if s.snapshots != nil {
				if err := s.snapshots.MaybeCreateSnapshot(ctx, pool, req.BlockNumber); err != nil {
					return ApplyBlockResult{}, fmt.Errorf("snapshot pool %s: %w", poolID, err)
				}
			}
			changed = append(changed, poolID)
			continue
		}

		poolLastBlock := pool.LastBlockNumber
		for _, event := range events {
			skipped := event.Meta.BlockNumber < poolLastBlock
			s.logger.Debug("pool event",
				append([]zap.Field{
					zap.String("protocol", "univ4"),
					zap.String("pool", poolID.String()),
					zap.Uint64("block", req.BlockNumber),
					zap.Uint64("eventBlock", event.Meta.BlockNumber),
					zap.String("kind", event.Kind.String()),
					zap.Uint("txIndex", event.Meta.TxIndex),
					zap.Uint("logIndex", event.Meta.LogIndex),
					zap.Uint64("poolLastBlock", poolLastBlock),
					zap.Bool("skipped", skipped),
				}, v4EventLogFields(event)...)...,
			)
			if err := pool.Apply(event); err != nil {
				pool.Status = market.PoolStatusError
				_ = s.pools.Save(ctx, pool)
				s.readiness.SetPoolReady(poolID, false)
				return ApplyBlockResult{}, fmt.Errorf("apply event on pool %s: %w", poolID, err)
			}
			if !skipped {
				s.logger.Debug("pool state",
					append([]zap.Field{
						zap.String("protocol", "univ4"),
						zap.String("pool", poolID.String()),
						zap.Uint64("block", req.BlockNumber),
						zap.String("kind", event.Kind.String()),
					}, syncapp.PoolStateLogFields(pool.State, pool.LastBlockNumber, pool.Status)...)...,
				)
			}
			poolLastBlock = pool.LastBlockNumber
		}

		if err := s.pools.Save(ctx, pool); err != nil {
			return ApplyBlockResult{}, fmt.Errorf("save pool %s: %w", poolID, err)
		}
		pendingCheckpoints = append(pendingCheckpoints, newCheckpoint(poolID, req.BlockNumber, req.BlockHash))

		if s.snapshots != nil {
			if err := s.snapshots.MaybeCreateSnapshot(ctx, pool, req.BlockNumber); err != nil {
				return ApplyBlockResult{}, fmt.Errorf("snapshot pool %s: %w", poolID, err)
			}
		}

		changed = append(changed, poolID)
	}

	idlePools := make([]marketv4.PoolID, 0, len(req.TrackedPools))
	for _, poolID := range req.TrackedPools {
		if _, ok := changedSet[poolID]; ok {
			continue
		}
		idlePools = append(idlePools, poolID)
	}
	if len(idlePools) > 0 {
		if err := s.syncIdlePools(ctx, idlePools, req.BlockNumber); err != nil {
			return ApplyBlockResult{}, err
		}
		for _, poolID := range idlePools {
			pendingCheckpoints = append(pendingCheckpoints, newCheckpoint(poolID, req.BlockNumber, req.BlockHash))
		}
		changed = append(changed, idlePools...)
	}

	if len(pendingCheckpoints) > 0 {
		if err := s.checkpoints.SaveMany(ctx, pendingCheckpoints); err != nil {
			return ApplyBlockResult{}, fmt.Errorf("save checkpoints: %w", err)
		}
	}

	sort.Slice(changed, func(i, j int) bool {
		return changed[i].String() < changed[j].String()
	})

	if len(changed) > 0 && !req.SuppressListener {
		if err := s.listener.OnPoolsChanged(ctx, req.BlockNumber, changed); err != nil {
			return ApplyBlockResult{}, fmt.Errorf("notify changed pools: %w", err)
		}
	}

	return ApplyBlockResult{
		BlockNumber:  req.BlockNumber,
		ChangedPools: changed,
	}, nil
}

func newCheckpoint(poolID marketv4.PoolID, blockNumber uint64, blockHash common.Hash) *blockchain.V4Checkpoint {
	return &blockchain.V4Checkpoint{
		PoolID:      poolID,
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
	}
}

func (s *BlockApplyService) MarkPoolsReady(ctx context.Context, poolIDs []marketv4.PoolID) error {
	for _, poolID := range poolIDs {
		pool, err := s.pools.Get(ctx, poolID)
		if err != nil {
			return fmt.Errorf("load pool %s: %w", poolID, err)
		}
		if pool == nil {
			return fmt.Errorf("pool %s not found", poolID)
		}
		pool.Status = market.PoolStatusReady
		if err := s.pools.Save(ctx, pool); err != nil {
			return fmt.Errorf("save ready pool %s: %w", poolID, err)
		}
		s.readiness.SetPoolReady(poolID, true)
	}
	return nil
}

func groupEventsByPool(events []marketv4.PoolEvent) map[marketv4.PoolID][]marketv4.PoolEvent {
	grouped := make(map[marketv4.PoolID][]marketv4.PoolEvent)
	for _, event := range events {
		poolID := event.Meta.PoolID
		grouped[poolID] = append(grouped[poolID], event)
	}
	return grouped
}

func (s *BlockApplyService) syncIdlePools(ctx context.Context, poolIDs []marketv4.PoolID, blockNumber uint64) error {
	if err := s.pools.AdvanceSyncProgressMany(ctx, poolIDs, blockNumber); err != nil {
		return fmt.Errorf("advance sync progress: %w", err)
	}
	return nil
}
