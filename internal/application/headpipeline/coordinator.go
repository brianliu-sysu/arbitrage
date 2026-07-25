package headpipeline

import (
	"context"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/marketchange"
)

type MarketCoordinator interface {
	PrepareHead(domainchain.BlockHeader) bool
	CommitHead(context.Context, domainchain.BlockHeader) (domainchain.MarketVersion, marketchange.Changes, bool)
}

type ScanScheduler interface {
	CancelCurrent(context.Context) error
	Start(context.Context, domainchain.MarketVersion, marketchange.Changes)
	Stop(context.Context) error
}

// Coordinator sequences market publication and arbitrage scanning around a chain head.
type Coordinator struct {
	markets MarketCoordinator
	scans   ScanScheduler
}

func NewCoordinator(markets MarketCoordinator, scans ScanScheduler) *Coordinator {
	return &Coordinator{markets: markets, scans: scans}
}

func (c *Coordinator) PrepareHead(ctx context.Context, head domainchain.BlockHeader) error {
	if c == nil || c.markets == nil || !c.markets.PrepareHead(head) || c.scans == nil {
		return nil
	}
	return c.scans.CancelCurrent(ctx)
}

func (c *Coordinator) FinalizeHead(ctx context.Context, head domainchain.BlockHeader) error {
	if c == nil || c.markets == nil {
		return nil
	}
	version, changes, committed := c.markets.CommitHead(ctx, head)
	if !committed || c.scans == nil {
		return nil
	}
	c.scans.Start(ctx, version, changes)
	return nil
}

// Stop cancels and waits for the active scan before runtime resources close.
func (c *Coordinator) Stop(ctx context.Context) error {
	if c == nil || c.scans == nil {
		return nil
	}
	return c.scans.Stop(ctx)
}
