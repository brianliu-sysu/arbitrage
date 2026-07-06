package syncapp

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
)

// SyncPhases implements the version-specific steps of pool sync startup.
type SyncPhases struct {
	StartAll         func(context.Context, uint64) error
	CatchUpAll       func(context.Context, uint64) error
	RefreshFromChain func(context.Context) error
	MarkPoolsReady   func(context.Context) error
	SetLocalHead     func(blockchain.BlockHeader)
	SetSystemReady   func(bool)
	RunHeadSync      func(context.Context) error
	RunScheduler     func(context.Context) error
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

	latest, err = blocks.GetLatestBlockHeader(ctx)
	if err != nil {
		return fmt.Errorf("refresh latest block: %w", err)
	}

	// V4 bootstrap can take a long time to load tick state. Re-bootstrap at the
	// refreshed head so we do not rely on event catchup to close a stale block gap.
	if err := phases.StartAll(ctx, latest.Number); err != nil {
		return fmt.Errorf("refresh pool bootstrap: %w", err)
	}

	if err := phases.CatchUpAll(ctx, latest.Number); err != nil {
		return fmt.Errorf("catch up pools: %w", err)
	}

	if phases.RefreshFromChain != nil {
		if err := phases.RefreshFromChain(ctx); err != nil {
			return fmt.Errorf("refresh pools from chain: %w", err)
		}
	}

	if err := phases.MarkPoolsReady(ctx); err != nil {
		return fmt.Errorf("mark pools ready: %w", err)
	}

	latest, err = blocks.GetLatestBlockHeader(ctx)
	if err != nil {
		return fmt.Errorf("refresh latest block before head sync: %w", err)
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
