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

	return phases.RunHeadSync(ctx)
}

// SyncOrchestrator coordinates bootstrap, catchup, and live head sync startup.
type SyncOrchestrator[PoolID comparable] struct {
	blocks     BlockReader
	lifecycle  *PoolLifecycleService[PoolID]
	catchup    interface {
		CatchUpAll(context.Context, uint64) error
	}
	headSync interface {
		Run(context.Context) error
		SetLocalHead(blockchain.BlockHeader)
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
	},
	headSync interface {
		Run(context.Context) error
		SetLocalHead(blockchain.BlockHeader)
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
