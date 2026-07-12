package syncapp

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
)

// SyncPhases implements the version-specific steps of pool sync startup.
type SyncPhases struct {
	StartAll       func(context.Context, uint64) error
	CatchUpAll     func(context.Context, uint64) error
	MarkPoolsReady func(context.Context) error
	SetLocalHead   func(blockchain.BlockHeader)
	SetSystemReady func(bool)
	RunHeadSync    func(context.Context) error
	RunScheduler   func(context.Context) error
}

// RunStartup bootstraps pools, catches up to the current head, then runs live sync.
func RunStartup(ctx context.Context, blocks BlockReader, phases SyncPhases) error {
	latest, err := blocks.GetLatestBlockHeader(ctx)
	if err != nil {
		return fmt.Errorf("load latest block: %w", err)
	}

	if err := phases.StartAll(ctx, latest.Number); err != nil {
		return fmt.Errorf("start pools: %w", err)
	}

	if err := phases.CatchUpAll(ctx, latest.Number); err != nil {
		return fmt.Errorf("catch up pools: %w", err)
	}

	if err := phases.MarkPoolsReady(ctx); err != nil {
		return fmt.Errorf("mark pools ready: %w", err)
	}

	phases.SetLocalHead(latest)
	phases.SetSystemReady(true)

	if phases.RunScheduler != nil {
		go func() {
			_ = phases.RunScheduler(ctx)
		}()
	}

	if phases.RunHeadSync == nil {
		return nil
	}
	return phases.RunHeadSync(ctx)
}

// SyncOrchestrator coordinates bootstrap, catchup, and live head sync startup.
type SyncOrchestrator[PoolID comparable] struct {
	blocks    BlockReader
	lifecycle *PoolLifecycleService[PoolID]
	catchup   interface {
		CatchUpAll(context.Context, uint64) error
		CatchUpPool(context.Context, PoolID, uint64) error
	}
	headSync interface {
		Run(context.Context) error
		SetLocalHead(blockchain.BlockHeader)
		WithHeadSyncPaused(context.Context, func(context.Context) error) error
	}
	blockApply interface {
		MarkPoolsReady(context.Context, []PoolID) error
	}
	scheduler interface {
		Run(context.Context) error
	}
	readiness *ReadinessService[PoolID]
}

func NewSyncOrchestrator[PoolID comparable](
	blocks BlockReader,
	lifecycle *PoolLifecycleService[PoolID],
	catchup interface {
		CatchUpAll(context.Context, uint64) error
		CatchUpPool(context.Context, PoolID, uint64) error
	},
	headSync interface {
		Run(context.Context) error
		SetLocalHead(blockchain.BlockHeader)
		WithHeadSyncPaused(context.Context, func(context.Context) error) error
	},
	blockApply interface {
		MarkPoolsReady(context.Context, []PoolID) error
	},
	scheduler interface {
		Run(context.Context) error
	},
	readiness *ReadinessService[PoolID],
) *SyncOrchestrator[PoolID] {
	return &SyncOrchestrator[PoolID]{
		blocks:     blocks,
		lifecycle:  lifecycle,
		catchup:    catchup,
		headSync:   headSync,
		blockApply: blockApply,
		scheduler:  scheduler,
		readiness:  readiness,
	}
}

func (o *SyncOrchestrator[PoolID]) Start(ctx context.Context) error {
	var schedulerRun func(context.Context) error
	if o.scheduler != nil {
		schedulerRun = o.scheduler.Run
	}

	return RunStartup(ctx, o.blocks, SyncPhases{
		StartAll:   o.lifecycle.StartAll,
		CatchUpAll: o.catchup.CatchUpAll,
		MarkPoolsReady: func(ctx context.Context) error {
			return o.blockApply.MarkPoolsReady(ctx, o.lifecycle.ListActive())
		},
		SetLocalHead:   o.headSync.SetLocalHead,
		SetSystemReady: o.readiness.SetSystemReady,
		RunHeadSync:    o.headSync.Run,
		RunScheduler:   schedulerRun,
	})
}

// StartBootstrap cold-starts pools and catchup, then returns without subscribing to new heads.
// Use SharedHeadRunner when multiple protocols must apply the same head together.
func (o *SyncOrchestrator[PoolID]) StartBootstrap(ctx context.Context) error {
	var schedulerRun func(context.Context) error
	if o.scheduler != nil {
		schedulerRun = o.scheduler.Run
	}

	return RunStartup(ctx, o.blocks, SyncPhases{
		StartAll:   o.lifecycle.StartAll,
		CatchUpAll: o.catchup.CatchUpAll,
		MarkPoolsReady: func(ctx context.Context) error {
			return o.blockApply.MarkPoolsReady(ctx, o.lifecycle.ListActive())
		},
		SetLocalHead:   o.headSync.SetLocalHead,
		SetSystemReady: o.readiness.SetSystemReady,
		RunHeadSync:    nil,
		RunScheduler:   schedulerRun,
	})
}

// AddPool registers a pool, catches it up until the chain head is stable, then marks it ready.
func (o *SyncOrchestrator[PoolID]) AddPool(ctx context.Context, poolID PoolID) error {
	if o == nil {
		return fmt.Errorf("sync orchestrator is nil")
	}
	if o.blocks == nil || o.lifecycle == nil || o.catchup == nil || o.headSync == nil || o.blockApply == nil {
		return fmt.Errorf("sync orchestrator is missing dependencies")
	}
	return o.headSync.WithHeadSyncPaused(ctx, func(ctx context.Context) error {
		head, err := o.blocks.GetLatestBlockHeader(ctx)
		if err != nil {
			return fmt.Errorf("load latest block: %w", err)
		}
		if err := o.lifecycle.RegisterAndBootstrapInactive(ctx, poolID, head.Number); err != nil {
			return err
		}
		if err := o.catchUpPoolUntilStableHead(ctx, poolID, head); err != nil {
			_ = o.lifecycle.Remove(ctx, poolID)
			return err
		}
		o.lifecycle.Activate(poolID)
		if err := o.blockApply.MarkPoolsReady(ctx, []PoolID{poolID}); err != nil {
			_ = o.lifecycle.Remove(ctx, poolID)
			return fmt.Errorf("mark pool ready: %w", err)
		}
		return nil
	})
}

func (o *SyncOrchestrator[PoolID]) catchUpPoolUntilStableHead(ctx context.Context, poolID PoolID, head blockchain.BlockHeader) error {
	for {
		if err := o.catchup.CatchUpPool(ctx, poolID, head.Number); err != nil {
			return fmt.Errorf("catch up pool to block %d: %w", head.Number, err)
		}
		current, err := o.blocks.GetLatestBlockHeader(ctx)
		if err != nil {
			return fmt.Errorf("load latest block after catchup: %w", err)
		}
		if current.Number <= head.Number {
			return nil
		}
		head = current
	}
}
