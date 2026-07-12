package arbitrageapp

import (
	"context"
	"errors"
	"testing"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

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
		Univ3Pools:  []common.Address{poolAB},
	}); err != nil {
		t.Fatalf("report univ3: %v", err)
	}
	coord.mu.Lock()
	flushed := coord.lastFlushed
	pending := len(coord.pending)
	coord.mu.Unlock()
	if flushed != 0 {
		t.Fatalf("expected barrier to wait, lastFlushed=%d", flushed)
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
	if err := coord.waitForIdle(context.Background()); err != nil {
		t.Fatalf("wait for scan: %v", err)
	}
	coord.mu.Lock()
	flushed = coord.lastFlushed
	pending = len(coord.pending)
	coord.mu.Unlock()
	if flushed != 100 {
		t.Fatalf("expected flush at block 100, got %d", flushed)
	}
	if pending != 0 {
		t.Fatalf("expected pending cleared, got %d", pending)
	}
}

func TestBlockCoordinatorCommitsOnlyAfterSuccessfulFlush(t *testing.T) {
	coord := NewBlockCoordinator([]SyncProtocol{SyncProtocolUniv3}, nil, nil, nil, nil, nil, nil)
	attempts := 0
	coord.flushFn = func(context.Context, uint64, []common.Address, []common.Address, []common.Address, []marketuniv4.PoolID, []marketbalancer.PoolID) error {
		attempts++
		if attempts == 1 {
			return errors.New("publish failed")
		}
		return nil
	}
	report := ProtocolBlockReport{Protocol: SyncProtocolUniv3, BlockNumber: 100}
	if err := coord.ReportApplied(context.Background(), report); err != nil {
		t.Fatalf("report first attempt: %v", err)
	}
	if err := coord.waitForIdle(context.Background()); err != nil {
		t.Fatalf("wait first attempt: %v", err)
	}
	coord.mu.Lock()
	lastFlushed := coord.lastFlushed
	_, pending := coord.pending[100]
	coord.mu.Unlock()
	if lastFlushed != 0 || !pending {
		t.Fatalf("failed flush must remain pending, lastFlushed=%d pending=%t", lastFlushed, pending)
	}
	if err := coord.ReportApplied(context.Background(), report); err != nil {
		t.Fatalf("retry flush: %v", err)
	}
	if err := coord.waitForIdle(context.Background()); err != nil {
		t.Fatalf("wait retry: %v", err)
	}
	coord.mu.Lock()
	lastFlushed = coord.lastFlushed
	_, pending = coord.pending[100]
	coord.mu.Unlock()
	if lastFlushed != 100 || pending {
		t.Fatalf("successful retry must commit, lastFlushed=%d pending=%t", lastFlushed, pending)
	}
}

func TestBlockCoordinatorCancelsOlderScanBeforeNewBlock(t *testing.T) {
	coord := NewBlockCoordinator([]SyncProtocol{SyncProtocolUniv3}, nil, nil, nil, nil, nil, nil)
	started := make(chan struct{})
	canceled := make(chan struct{})
	coord.flushFn = func(ctx context.Context, blockNumber uint64, _ []common.Address, _ []common.Address, _ []common.Address, _ []marketuniv4.PoolID, _ []marketbalancer.PoolID) error {
		if blockNumber == 100 {
			close(started)
			<-ctx.Done()
			close(canceled)
			return ctx.Err()
		}
		return nil
	}
	if err := coord.ReportApplied(context.Background(), ProtocolBlockReport{Protocol: SyncProtocolUniv3, BlockNumber: 100}); err != nil {
		t.Fatalf("report block 100: %v", err)
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
	coord.mu.Lock()
	lastFlushed := coord.lastFlushed
	coord.mu.Unlock()
	if lastFlushed != 0 {
		t.Fatalf("canceled block must not commit, got %d", lastFlushed)
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
	if err := coord.waitForIdle(context.Background()); err != nil {
		t.Fatalf("wait for scan: %v", err)
	}
	coord.mu.Lock()
	flushed := coord.lastFlushed
	coord.mu.Unlock()
	if flushed != 42 {
		t.Fatalf("expected immediate flush at 42, got %d", flushed)
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
	coord.mu.Lock()
	flushed := coord.lastFlushed
	pending := len(coord.pending)
	coord.mu.Unlock()
	if flushed != 0 || pending != 0 {
		t.Fatalf("disabled protocol should be ignored, flushed=%d pending=%d", flushed, pending)
	}
}

type testMarketViewCommitter struct {
	blocks   []uint64
	versions []domainchain.MarketVersion
	err      error
}

func (c *testMarketViewCommitter) Commit(_ context.Context, version domainchain.MarketVersion, _ []common.Address, _ []common.Address, _ []common.Address, _ []marketuniv4.PoolID, _ []marketbalancer.PoolID) error {
	c.blocks = append(c.blocks, version.Number)
	c.versions = append(c.versions, version)
	return c.err
}

func TestBlockCoordinatorCommitsMarketViewBeforeStartingScan(t *testing.T) {
	committer := &testMarketViewCommitter{}
	coord := NewBlockCoordinator([]SyncProtocol{SyncProtocolUniv3}, nil, nil, nil, nil, committer, nil)
	scanned := make(chan struct{}, 1)
	coord.flushFn = func(context.Context, uint64, []common.Address, []common.Address, []common.Address, []marketuniv4.PoolID, []marketbalancer.PoolID) error {
		if len(committer.blocks) != 1 || committer.blocks[0] != 12 {
			t.Fatalf("scan started before market view commit: %+v", committer.blocks)
		}
		scanned <- struct{}{}
		return nil
	}
	if err := coord.ReportApplied(context.Background(), ProtocolBlockReport{Protocol: SyncProtocolUniv3, BlockNumber: 12}); err != nil {
		t.Fatalf("report: %v", err)
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

func TestBlockCoordinatorKeepsOldViewWhenCommitFails(t *testing.T) {
	committer := &testMarketViewCommitter{err: errors.New("pool block mismatch")}
	coord := NewBlockCoordinator([]SyncProtocol{SyncProtocolUniv3}, nil, nil, nil, nil, committer, nil)
	started := false
	coord.flushFn = func(context.Context, uint64, []common.Address, []common.Address, []common.Address, []marketuniv4.PoolID, []marketbalancer.PoolID) error {
		started = true
		return nil
	}
	if err := coord.ReportApplied(context.Background(), ProtocolBlockReport{Protocol: SyncProtocolUniv3, BlockNumber: 13}); err != nil {
		t.Fatalf("commit failure should not stop shared-head notify: %v", err)
	}
	if started {
		t.Fatal("scan started after market view commit failed")
	}
	coord.mu.Lock()
	flushing := coord.flushing[13]
	coord.mu.Unlock()
	if flushing {
		t.Fatal("expected flushing flag cleared after commit failure")
	}
}

func TestBlockCoordinatorRecommitsSameHeightDifferentHash(t *testing.T) {
	committer := &testMarketViewCommitter{}
	coord := NewBlockCoordinator([]SyncProtocol{SyncProtocolUniv3}, nil, nil, nil, nil, committer, nil)
	headA := domainchain.BlockHeader{Number: 20, Hash: common.HexToHash("0xaa")}
	headB := domainchain.BlockHeader{Number: 20, Hash: common.HexToHash("0xbb")}
	if err := coord.PrepareHead(context.Background(), headA); err != nil {
		t.Fatalf("prepare A: %v", err)
	}
	if err := coord.ReportApplied(context.Background(), ProtocolBlockReport{Protocol: SyncProtocolUniv3, BlockNumber: 20}); err != nil {
		t.Fatalf("report A: %v", err)
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
