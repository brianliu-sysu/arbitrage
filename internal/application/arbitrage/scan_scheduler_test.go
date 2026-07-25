package arbitrageapp

import (
	"context"
	"sync/atomic"
	"testing"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/marketchange"
)

type scanExecutorFunc func(context.Context, domainchain.MarketVersion, marketchange.Changes) error

func (f scanExecutorFunc) Execute(ctx context.Context, version domainchain.MarketVersion, changes marketchange.Changes) error {
	return f(ctx, version, changes)
}

func TestScanSchedulerStopCancelsActiveScanAndRejectsNewScans(t *testing.T) {
	started := make(chan struct{})
	var executions atomic.Int32
	scheduler := NewScanScheduler(scanExecutorFunc(func(ctx context.Context, _ domainchain.MarketVersion, _ marketchange.Changes) error {
		if executions.Add(1) == 1 {
			close(started)
		}
		<-ctx.Done()
		return ctx.Err()
	}), nil)

	scheduler.Start(context.Background(), domainchain.MarketVersion{Number: 10}, marketchange.Changes{})
	<-started
	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatalf("stop scheduler: %v", err)
	}

	scheduler.Start(context.Background(), domainchain.MarketVersion{Number: 11}, marketchange.Changes{})
	if executions.Load() != 1 {
		t.Fatalf("closed scheduler started another scan: executions=%d", executions.Load())
	}
}
