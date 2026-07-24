package arbitrageapp

import (
	"context"
	"errors"
	"testing"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/marketchange"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

type blockScanExecutorFunc func(context.Context, domainchain.MarketVersion, MarketChanges) error

func (f blockScanExecutorFunc) Execute(ctx context.Context, version domainchain.MarketVersion, changes MarketChanges) error {
	return f(ctx, version, changes)
}

func TestBlockCoordinatorWaitsForEnabledProtocols(t *testing.T) {
	tokenA := common.HexToAddress("0x0000000000000000000000000000000000000001")
	tokenB := common.HexToAddress("0x0000000000000000000000000000000000000002")
	poolAB := common.HexToAddress("0x0000000000000000000000000000000000000010")

	scan := NewScanService(domainarb.NewDependencyGraph())
	scan.RegisterRoutes([]domainarb.RouteRef{{
		ID: "cycle-ab",
		Route: quoteunified.Route{
			TokenIn:  tokenA,
			TokenOut: tokenA,
			Hops: []quoteunified.RouteHop{
				{Version: quoteunified.PoolVersionV3, PoolV3: poolAB, TokenIn: tokenA, TokenOut: tokenB},
				{Version: quoteunified.PoolVersionV3, PoolV3: poolAB, TokenIn: tokenB, TokenOut: tokenA},
			},
		},
	}})

	coord := NewBlockCoordinator(
		[]SyncProtocol{SyncProtocolUniv3, SyncProtocolUniv4},
		nil,
		scan,
		nil,
		nil,
		nil,
		nil,
	)

	if err := coord.ReportApplied(context.Background(), ProtocolBlockReport{
		Protocol:    SyncProtocolUniv3,
		BlockNumber: 100,
		Changes:     MarketChanges{Univ3: []common.Address{poolAB}},
	}); err != nil {
		t.Fatalf("report univ3: %v", err)
	}
	coord.barrier.mu.Lock()
	lastVersionNumber := coord.barrier.lastVersion.Number
	pending := len(coord.barrier.pending)
	coord.barrier.mu.Unlock()
	if lastVersionNumber != 0 {
		t.Fatalf("expected barrier to wait, last version=%d", lastVersionNumber)
	}
	if pending != 1 {
		t.Fatalf("expected one pending block, got %d", pending)
	}

	if err := coord.ReportApplied(context.Background(), ProtocolBlockReport{
		Protocol:    SyncProtocolUniv4,
		BlockNumber: 100,
	}); err != nil {
		t.Fatalf("report univ4: %v", err)
	}
	if err := coord.FinalizeHead(context.Background(), domainchain.BlockHeader{Number: 100}); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if err := coord.waitForIdle(context.Background()); err != nil {
		t.Fatalf("wait for scan: %v", err)
	}
	coord.barrier.mu.Lock()
	lastVersionNumber = coord.barrier.lastVersion.Number
	pending = len(coord.barrier.pending)
	coord.barrier.mu.Unlock()
	if lastVersionNumber != 100 {
		t.Fatalf("expected committed version at block 100, got %d", lastVersionNumber)
	}
	if pending != 0 {
		t.Fatalf("expected pending cleared, got %d", pending)
	}
}

func TestBlockCoordinatorCommitsOnlyAfterSuccessfulFlush(t *testing.T) {
	coord := NewBlockCoordinator([]SyncProtocol{SyncProtocolUniv3}, nil, nil, nil, nil, nil, nil)
	attempts := 0
	coord.executor = blockScanExecutorFunc(func(context.Context, domainchain.MarketVersion, MarketChanges) error {
		attempts++
		if attempts == 1 {
			return errors.New("publish failed")
		}
		return nil
	})
	report := ProtocolBlockReport{Protocol: SyncProtocolUniv3, BlockNumber: 100}
	if err := coord.ReportApplied(context.Background(), report); err != nil {
		t.Fatalf("report first attempt: %v", err)
	}
	if err := coord.FinalizeHead(context.Background(), domainchain.BlockHeader{Number: 100}); err != nil {
		t.Fatalf("finalize first attempt: %v", err)
	}
	if err := coord.waitForIdle(context.Background()); err != nil {
		t.Fatalf("wait first attempt: %v", err)
	}
	coord.barrier.mu.Lock()
	lastVersionNumber := coord.barrier.lastVersion.Number
	_, pending := coord.barrier.pending[100]
	coord.barrier.mu.Unlock()
	if lastVersionNumber != 0 || !pending {
		t.Fatalf("failed scan must remain pending, last version=%d pending=%t", lastVersionNumber, pending)
	}
	if err := coord.ReportApplied(context.Background(), report); err != nil {
		t.Fatalf("retry flush: %v", err)
	}
	if err := coord.FinalizeHead(context.Background(), domainchain.BlockHeader{Number: 100}); err != nil {
		t.Fatalf("finalize retry: %v", err)
	}
	if err := coord.waitForIdle(context.Background()); err != nil {
		t.Fatalf("wait retry: %v", err)
	}
	coord.barrier.mu.Lock()
	lastVersionNumber = coord.barrier.lastVersion.Number
	_, pending = coord.barrier.pending[100]
	coord.barrier.mu.Unlock()
	if lastVersionNumber != 100 || pending {
		t.Fatalf("successful retry must commit, last version=%d pending=%t", lastVersionNumber, pending)
	}
}

func TestBlockCoordinatorCancelsOlderScanBeforeNewBlock(t *testing.T) {
	coord := NewBlockCoordinator([]SyncProtocol{SyncProtocolUniv3}, nil, nil, nil, nil, nil, nil)
	started := make(chan struct{})
	canceled := make(chan struct{})
	coord.executor = blockScanExecutorFunc(func(ctx context.Context, version domainchain.MarketVersion, _ MarketChanges) error {
		if version.Number == 100 {
			close(started)
			<-ctx.Done()
			close(canceled)
			return ctx.Err()
		}
		return nil
	})
	if err := coord.ReportApplied(context.Background(), ProtocolBlockReport{Protocol: SyncProtocolUniv3, BlockNumber: 100}); err != nil {
		t.Fatalf("report block 100: %v", err)
	}
	if err := coord.FinalizeHead(context.Background(), domainchain.BlockHeader{Number: 100}); err != nil {
		t.Fatalf("finalize block 100: %v", err)
	}
	<-started
	if err := coord.CancelBefore(context.Background(), 101); err != nil {
		t.Fatalf("cancel before block 101: %v", err)
	}
	select {
	case <-canceled:
	default:
		t.Fatal("old scan was not canceled before returning")
	}
	coord.barrier.mu.Lock()
	lastVersionNumber := coord.barrier.lastVersion.Number
	coord.barrier.mu.Unlock()
	if lastVersionNumber != 0 {
		t.Fatalf("canceled block must not commit, got %d", lastVersionNumber)
	}
}

func TestBlockCoordinatorFlushesImmediatelyWithoutEnabledSet(t *testing.T) {
	coord := NewBlockCoordinator(nil, nil, nil, nil, nil, nil, nil)
	if err := coord.ReportApplied(context.Background(), ProtocolBlockReport{
		Protocol:    SyncProtocolUniv3,
		BlockNumber: 42,
	}); err != nil {
		t.Fatalf("report: %v", err)
	}
	if err := coord.FinalizeHead(context.Background(), domainchain.BlockHeader{Number: 42}); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if err := coord.waitForIdle(context.Background()); err != nil {
		t.Fatalf("wait for scan: %v", err)
	}
	coord.barrier.mu.Lock()
	lastVersionNumber := coord.barrier.lastVersion.Number
	coord.barrier.mu.Unlock()
	if lastVersionNumber != 42 {
		t.Fatalf("expected immediate commit at 42, got %d", lastVersionNumber)
	}
}

func TestBlockCoordinatorIgnoresDisabledProtocol(t *testing.T) {
	coord := NewBlockCoordinator([]SyncProtocol{SyncProtocolUniv3}, nil, nil, nil, nil, nil, nil)
	if err := coord.ReportApplied(context.Background(), ProtocolBlockReport{
		Protocol:    SyncProtocolUniv4,
		BlockNumber: 7,
	}); err != nil {
		t.Fatalf("report disabled protocol: %v", err)
	}
	coord.barrier.mu.Lock()
	lastVersionNumber := coord.barrier.lastVersion.Number
	pending := len(coord.barrier.pending)
	coord.barrier.mu.Unlock()
	if lastVersionNumber != 0 || pending != 0 {
		t.Fatalf("disabled protocol should be ignored, last version=%d pending=%d", lastVersionNumber, pending)
	}
}

type testMarketPublisher struct {
	blocks   []uint64
	versions []domainchain.MarketVersion
	err      error
}

func (c *testMarketPublisher) Publish(_ context.Context, version domainchain.MarketVersion, _ marketchange.Changes) error {
	c.blocks = append(c.blocks, version.Number)
	c.versions = append(c.versions, version)
	return c.err
}

func TestBlockCoordinatorCommitsMarketViewBeforeStartingScan(t *testing.T) {
	committer := &testMarketPublisher{}
	coord := NewBlockCoordinator([]SyncProtocol{SyncProtocolUniv3}, nil, nil, nil, nil, committer, nil)
	scanned := make(chan struct{}, 1)
	coord.executor = blockScanExecutorFunc(func(context.Context, domainchain.MarketVersion, MarketChanges) error {
		if len(committer.blocks) != 1 || committer.blocks[0] != 12 {
			t.Fatalf("scan started before market view commit: %+v", committer.blocks)
		}
		scanned <- struct{}{}
		return nil
	})
	if err := coord.ReportApplied(context.Background(), ProtocolBlockReport{Protocol: SyncProtocolUniv3, BlockNumber: 12}); err != nil {
		t.Fatalf("report: %v", err)
	}
	if err := coord.FinalizeHead(context.Background(), domainchain.BlockHeader{Number: 12}); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if err := coord.waitForIdle(context.Background()); err != nil {
		t.Fatalf("wait scan: %v", err)
	}
	select {
	case <-scanned:
	default:
		t.Fatal("scan did not start")
	}
}

func TestBlockCoordinatorUnifiedPublishWaitsForFinalizeHead(t *testing.T) {
	publisher := &testMarketPublisher{}
	coord := NewBlockCoordinator(
		[]SyncProtocol{SyncProtocolUniv3, SyncProtocolPancakeV3},
		nil, nil, nil, nil, publisher, nil,
	)
	head := domainchain.BlockHeader{Number: 15, Hash: common.HexToHash("0x15")}
	if err := coord.PrepareHead(context.Background(), head); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := coord.ReportApplied(context.Background(), ProtocolBlockReport{Protocol: SyncProtocolUniv3, BlockNumber: 15}); err != nil {
		t.Fatalf("report univ3: %v", err)
	}
	if err := coord.ReportApplied(context.Background(), ProtocolBlockReport{Protocol: SyncProtocolPancakeV3, BlockNumber: 15}); err != nil {
		t.Fatalf("report pancake: %v", err)
	}
	if len(publisher.versions) != 0 {
		t.Fatalf("published before unified finalize: %+v", publisher.versions)
	}
	if err := coord.FinalizeHead(context.Background(), head); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if len(publisher.versions) != 1 || !publisher.versions[0].SameBlock(domainchain.MarketVersion{Number: head.Number, Hash: head.Hash}) {
		t.Fatalf("unexpected published versions: %+v", publisher.versions)
	}
}

func TestBlockCoordinatorKeepsOldViewWhenCommitFails(t *testing.T) {
	committer := &testMarketPublisher{err: errors.New("pool block mismatch")}
	coord := NewBlockCoordinator([]SyncProtocol{SyncProtocolUniv3}, nil, nil, nil, nil, committer, nil)
	started := false
	coord.executor = blockScanExecutorFunc(func(context.Context, domainchain.MarketVersion, MarketChanges) error {
		started = true
		return nil
	})
	if err := coord.ReportApplied(context.Background(), ProtocolBlockReport{Protocol: SyncProtocolUniv3, BlockNumber: 13}); err != nil {
		t.Fatalf("commit failure should not stop shared-head notify: %v", err)
	}
	if err := coord.FinalizeHead(context.Background(), domainchain.BlockHeader{Number: 13}); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if started {
		t.Fatal("scan started after market view commit failed")
	}
	coord.barrier.mu.Lock()
	_, flushing := coord.barrier.flushing[13]
	coord.barrier.mu.Unlock()
	if flushing {
		t.Fatal("expected flushing flag cleared after commit failure")
	}
}

func TestBlockCoordinatorRecommitsSameHeightDifferentHash(t *testing.T) {
	committer := &testMarketPublisher{}
	coord := NewBlockCoordinator([]SyncProtocol{SyncProtocolUniv3}, nil, nil, nil, nil, committer, nil)
	headA := domainchain.BlockHeader{Number: 20, Hash: common.HexToHash("0xaa")}
	headB := domainchain.BlockHeader{Number: 20, Hash: common.HexToHash("0xbb")}
	if err := coord.PrepareHead(context.Background(), headA); err != nil {
		t.Fatalf("prepare A: %v", err)
	}
	if err := coord.ReportApplied(context.Background(), ProtocolBlockReport{Protocol: SyncProtocolUniv3, BlockNumber: 20}); err != nil {
		t.Fatalf("report A: %v", err)
	}
	if err := coord.FinalizeHead(context.Background(), headA); err != nil {
		t.Fatalf("finalize A: %v", err)
	}
	if err := coord.waitForIdle(context.Background()); err != nil {
		t.Fatalf("wait A: %v", err)
	}
	if err := coord.PrepareHead(context.Background(), headB); err != nil {
		t.Fatalf("prepare B: %v", err)
	}
	if err := coord.ReportApplied(context.Background(), ProtocolBlockReport{Protocol: SyncProtocolUniv3, BlockNumber: 20}); err != nil {
		t.Fatalf("report B: %v", err)
	}
	if err := coord.FinalizeHead(context.Background(), headB); err != nil {
		t.Fatalf("finalize B: %v", err)
	}
	if err := coord.waitForIdle(context.Background()); err != nil {
		t.Fatalf("wait B: %v", err)
	}
	if len(committer.versions) != 2 {
		t.Fatalf("expected two commits at same height, got %+v", committer.versions)
	}
	if committer.versions[0].Hash == committer.versions[1].Hash || committer.versions[1].Generation <= committer.versions[0].Generation {
		t.Fatalf("expected distinct hash and increasing generation: %+v", committer.versions)
	}
}
