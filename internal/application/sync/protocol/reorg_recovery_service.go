package protocol

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
)

// ReorgRecoveryState restores state captured before recovery preparation.
type ReorgRecoveryState interface {
	Restore(context.Context) error
}

type ReorgPoolRepository[PoolID comparable, Pool any] interface {
	Get(context.Context, PoolID) (Pool, error)
	Save(context.Context, Pool) error
}

type ReorgRecoveryCoordinator[PoolID comparable] interface {
	CaptureRecoveryState(context.Context, []PoolID) (ReorgRecoveryState, error)
	NotifyPoolsChanged(context.Context, uint64, []PoolID) error
}

type ReorgRecoveryDeps[PoolID comparable, Pool any] struct {
	Pools       ReorgPoolRepository[PoolID, Pool]
	Coordinator ReorgRecoveryCoordinator[PoolID]
	Readiness   PoolReadiness[PoolID]
}

// ReorgRecoveryProtocol defines protocol-specific state restoration behavior.
// Canonical replay logs are deliberately absent: the shared runner owns all
// chain reads.
type ReorgRecoveryProtocol[PoolID comparable, Pool any] interface {
	FormatPoolID(PoolID) string
	IsNilPool(Pool) bool
	DeleteSnapshotsAfter(context.Context, PoolID, uint64) error
	RestorePoolState(context.Context, Pool, PoolID, uint64) (uint64, error)
	SetPoolStatus(Pool, market.PoolStatus)
}

// ReorgRecoveryService prepares pool state for shared canonical replay.
type ReorgRecoveryService[PoolID comparable, Pool any] struct {
	deps     ReorgRecoveryDeps[PoolID, Pool]
	protocol ReorgRecoveryProtocol[PoolID, Pool]
}

func NewReorgRecoveryService[PoolID comparable, Pool any](
	deps ReorgRecoveryDeps[PoolID, Pool],
	protocol ReorgRecoveryProtocol[PoolID, Pool],
) *ReorgRecoveryService[PoolID, Pool] {
	return &ReorgRecoveryService[PoolID, Pool]{deps: deps, protocol: protocol}
}

// reorgRecoveryPlan tracks the block at which each pool must join replay.
type reorgRecoveryPlan[PoolID comparable, Pool any] struct {
	reorg      blockchain.Reorg
	poolIDs    []PoolID
	replayFrom map[PoolID]uint64
	earliest   uint64
	state      ReorgRecoveryState
	deps       ReorgRecoveryDeps[PoolID, Pool]
	protocol   ReorgRecoveryProtocol[PoolID, Pool]
}

// Prepare restores pools to their latest usable snapshot/bootstrap state and
// returns a plan. It does not fetch or apply canonical logs.
func (s *ReorgRecoveryService[PoolID, Pool]) Prepare(
	ctx context.Context,
	reorg blockchain.Reorg,
	poolIDs []PoolID,
) (ReorgPlan[PoolID], error) {
	state, err := s.deps.Coordinator.CaptureRecoveryState(ctx, poolIDs)
	if err != nil {
		return nil, fmt.Errorf("capture state before reorg recovery: %w", err)
	}
	plan := &reorgRecoveryPlan[PoolID, Pool]{
		reorg:      reorg,
		poolIDs:    append([]PoolID(nil), poolIDs...),
		replayFrom: make(map[PoolID]uint64, len(poolIDs)),
		earliest:   reorg.CommonAncestor + 1,
		state:      state,
		deps:       s.deps,
		protocol:   s.protocol,
	}
	for _, poolID := range poolIDs {
		s.deps.Readiness.SetPoolReady(poolID, false)
		if deleteErr := s.protocol.DeleteSnapshotsAfter(ctx, poolID, reorg.CommonAncestor); deleteErr != nil {
			return nil, plan.rollbackPrepare(ctx, fmt.Errorf(
				"delete snapshots for pool %s: %w",
				s.protocol.FormatPoolID(poolID),
				deleteErr,
			))
		}
		pool, loadErr := s.deps.Pools.Get(ctx, poolID)
		if loadErr != nil {
			return nil, plan.rollbackPrepare(ctx, fmt.Errorf("load pool %s: %w", s.protocol.FormatPoolID(poolID), loadErr))
		}
		if s.protocol.IsNilPool(pool) {
			return nil, plan.rollbackPrepare(ctx, fmt.Errorf("pool %s not found", s.protocol.FormatPoolID(poolID)))
		}
		fromBlock, restoreErr := s.protocol.RestorePoolState(ctx, pool, poolID, reorg.CommonAncestor)
		if restoreErr != nil {
			return nil, plan.rollbackPrepare(ctx, fmt.Errorf("restore pool %s: %w", s.protocol.FormatPoolID(poolID), restoreErr))
		}
		plan.replayFrom[poolID] = fromBlock
		if fromBlock < plan.earliest {
			plan.earliest = fromBlock
		}
		s.protocol.SetPoolStatus(pool, market.PoolStatusSyncing)
		if saveErr := s.deps.Pools.Save(ctx, pool); saveErr != nil {
			return nil, plan.rollbackPrepare(ctx, fmt.Errorf("save pool %s: %w", s.protocol.FormatPoolID(poolID), saveErr))
		}
	}
	return plan, nil
}

func (p *reorgRecoveryPlan[PoolID, Pool]) ReplayFrom() uint64 {
	return p.earliest
}

func (p *reorgRecoveryPlan[PoolID, Pool]) PoolsForBlock(blockNumber uint64) []PoolID {
	pools := make([]PoolID, 0, len(p.poolIDs))
	for _, poolID := range p.poolIDs {
		if p.replayFrom[poolID] <= blockNumber {
			pools = append(pools, poolID)
		}
	}
	return pools
}

func (p *reorgRecoveryPlan[PoolID, Pool]) Commit(ctx context.Context) error {
	for _, poolID := range p.poolIDs {
		pool, err := p.deps.Pools.Get(ctx, poolID)
		if err != nil {
			return fmt.Errorf("reload pool %s: %w", p.protocol.FormatPoolID(poolID), err)
		}
		p.protocol.SetPoolStatus(pool, market.PoolStatusReady)
		if err := p.deps.Pools.Save(ctx, pool); err != nil {
			return fmt.Errorf("save ready pool %s: %w", p.protocol.FormatPoolID(poolID), err)
		}
		p.deps.Readiness.SetPoolReady(poolID, true)
	}
	if err := p.deps.Coordinator.NotifyPoolsChanged(ctx, p.reorg.RemoteHead.Number, p.poolIDs); err != nil {
		return fmt.Errorf("notify reorg recovery at block %d: %w", p.reorg.RemoteHead.Number, err)
	}
	return nil
}

func (p *reorgRecoveryPlan[PoolID, Pool]) Rollback(ctx context.Context) error {
	if p.state == nil {
		return nil
	}
	return p.state.Restore(ctx)
}

func (p *reorgRecoveryPlan[PoolID, Pool]) rollbackPrepare(ctx context.Context, cause error) error {
	if rollbackErr := p.Rollback(ctx); rollbackErr != nil {
		return fmt.Errorf("%w; rollback prepared recovery: %v", cause, rollbackErr)
	}
	return cause
}
