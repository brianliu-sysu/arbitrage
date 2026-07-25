package headpipeline

import (
	"context"
	"testing"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/marketchange"
)

type marketStub struct {
	changed   bool
	committed bool
}

func (m *marketStub) PrepareHead(domainchain.BlockHeader) bool { return m.changed }

func (m *marketStub) CommitHead(
	context.Context,
	domainchain.BlockHeader,
) (domainchain.MarketVersion, marketchange.Changes, bool) {
	return domainchain.MarketVersion{Number: 10}, marketchange.Changes{}, m.committed
}

type scanStub struct {
	canceled int
	started  int
}

func (s *scanStub) CancelCurrent(context.Context) error {
	s.canceled++
	return nil
}

func (s *scanStub) Start(context.Context, domainchain.MarketVersion, marketchange.Changes) {
	s.started++
}

func (s *scanStub) Stop(context.Context) error { return nil }

func TestCoordinatorCancelsBeforeChangedHeadAndScansAfterCommit(t *testing.T) {
	markets := &marketStub{changed: true, committed: true}
	scans := &scanStub{}
	coordinator := NewCoordinator(markets, scans)

	if err := coordinator.PrepareHead(context.Background(), domainchain.BlockHeader{Number: 10}); err != nil {
		t.Fatalf("prepare head: %v", err)
	}
	if err := coordinator.FinalizeHead(context.Background(), domainchain.BlockHeader{Number: 10}); err != nil {
		t.Fatalf("finalize head: %v", err)
	}
	if scans.canceled != 1 || scans.started != 1 {
		t.Fatalf("unexpected scan calls: canceled=%d started=%d", scans.canceled, scans.started)
	}
}

func TestCoordinatorDoesNotScanUncommittedHead(t *testing.T) {
	scans := &scanStub{}
	coordinator := NewCoordinator(&marketStub{}, scans)
	if err := coordinator.FinalizeHead(context.Background(), domainchain.BlockHeader{Number: 10}); err != nil {
		t.Fatalf("finalize head: %v", err)
	}
	if scans.started != 0 {
		t.Fatalf("expected no scan, got %d", scans.started)
	}
}
