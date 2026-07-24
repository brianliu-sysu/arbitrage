package runner_test

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/runner"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum/common"
)

type recordingHeadHandler struct {
	name    string
	mu      sync.Mutex
	heads   []uint64
	delay   time.Duration
	failAt  uint64
	started chan struct{}
	release chan struct{}
}

type recordingBlockHandler struct {
	heads []uint64
	logs  int
}

type blockHandlerFunc func(context.Context, blockchain.BlockHeader, []blockchain.RawLog) error

func (f blockHandlerFunc) HandleBlock(ctx context.Context, head blockchain.BlockHeader, logs []blockchain.RawLog) error {
	return f(ctx, head, logs)
}

func (f blockHandlerFunc) PrepareBlock(_ context.Context, head blockchain.BlockHeader, logs []blockchain.RawLog) (syncapp.PreparedBlock, error) {
	return &preparedBlockFunc{
		apply: func(ctx context.Context) error {
			return f.HandleBlock(ctx, head, logs)
		},
	}, nil
}

type preparedBlockFunc struct {
	apply    func(context.Context) error
	rollback func(context.Context) error
}

func (f *preparedBlockFunc) Apply(ctx context.Context) error {
	return f.apply(ctx)
}

func (f *preparedBlockFunc) Rollback(ctx context.Context) error {
	if f.rollback == nil {
		return nil
	}
	return f.rollback(ctx)
}

type preparingBlockHandler struct {
	prepareErr error
	applyErr   error
	applied    *atomic.Int32
	rolledBack *atomic.Int32
	recovered  *atomic.Int32
	replayed   *atomic.Int32
	replayFrom uint64
}

func (h *preparingBlockHandler) HandleBlock(context.Context, blockchain.BlockHeader, []blockchain.RawLog) error {
	panic("legacy HandleBlock should not be called")
}

func (h *preparingBlockHandler) PrepareBlock(context.Context, blockchain.BlockHeader, []blockchain.RawLog) (syncapp.PreparedBlock, error) {
	if h.prepareErr != nil {
		return nil, h.prepareErr
	}
	return &preparedBlockFunc{
		apply: func(context.Context) error {
			h.applied.Add(1)
			return h.applyErr
		},
		rollback: func(context.Context) error {
			if h.rolledBack != nil {
				h.rolledBack.Add(1)
			}
			return nil
		},
	}, nil
}

func (h *preparingBlockHandler) PrepareReorg(_ context.Context, reorg blockchain.Reorg) (syncapp.PreparedReorg, error) {
	replayFrom := h.replayFrom
	if replayFrom == 0 {
		replayFrom = reorg.RemoteHead.Number + 1
	}
	return &preparedReorgFunc{
		replayFrom: replayFrom,
		prepare: func(context.Context, blockchain.BlockHeader, []blockchain.RawLog) (syncapp.PreparedBlock, error) {
			return &preparedBlockFunc{apply: func(context.Context) error {
				if h.replayed != nil {
					h.replayed.Add(1)
				}
				return nil
			}}, nil
		},
		commit: func(context.Context) error {
			if h.recovered != nil {
				h.recovered.Add(1)
			}
			return nil
		},
	}, nil
}

type preparedReorgFunc struct {
	replayFrom uint64
	prepare    func(context.Context, blockchain.BlockHeader, []blockchain.RawLog) (syncapp.PreparedBlock, error)
	commit     func(context.Context) error
}

func (p *preparedReorgFunc) ReplayFrom() uint64 { return p.replayFrom }

func (p *preparedReorgFunc) PrepareBlock(ctx context.Context, head blockchain.BlockHeader, logs []blockchain.RawLog) (syncapp.PreparedBlock, error) {
	return p.prepare(ctx, head, logs)
}

func (p *preparedReorgFunc) Commit(ctx context.Context) error {
	if p.commit == nil {
		return nil
	}
	return p.commit(ctx)
}

func (p *preparedReorgFunc) Rollback(context.Context) error { return nil }

type sharedBlockReader struct {
	headers map[uint64]blockchain.BlockHeader
}

func (r *sharedBlockReader) GetLatestBlockHeader(context.Context) (blockchain.BlockHeader, error) {
	return blockchain.BlockHeader{}, nil
}

func (r *sharedBlockReader) GetBlockHeaders(_ context.Context, blockNumbers []uint64) (map[uint64]blockchain.BlockHeader, error) {
	headers := make(map[uint64]blockchain.BlockHeader, len(blockNumbers))
	for _, blockNumber := range blockNumbers {
		header, ok := r.headers[blockNumber]
		if !ok {
			header = blockchain.BlockHeader{Number: blockNumber}
		}
		headers[blockNumber] = header
	}
	return headers, nil
}

func (h *recordingBlockHandler) HandleBlock(_ context.Context, head blockchain.BlockHeader, logs []blockchain.RawLog) error {
	h.heads = append(h.heads, head.Number)
	h.logs += len(logs)
	return nil
}

func (h *recordingBlockHandler) PrepareBlock(_ context.Context, head blockchain.BlockHeader, logs []blockchain.RawLog) (syncapp.PreparedBlock, error) {
	return &preparedBlockFunc{
		apply: func(ctx context.Context) error {
			return h.HandleBlock(ctx, head, logs)
		},
	}, nil
}

type recordingHeadLogFetcher struct {
	calls int
	hash  common.Hash
	logs  []blockchain.RawLog
}

func (f *recordingHeadLogFetcher) FetchBlockLogs(_ context.Context, hash common.Hash) ([]blockchain.RawLog, error) {
	f.calls++
	f.hash = hash
	return append([]blockchain.RawLog(nil), f.logs...), nil
}

type testHeadCoordinator struct {
	prepare  func(context.Context, blockchain.BlockHeader) error
	finalize func(context.Context, blockchain.BlockHeader) error
}

func (c *testHeadCoordinator) PrepareHead(ctx context.Context, head blockchain.BlockHeader) error {
	if c.prepare == nil {
		return nil
	}
	return c.prepare(ctx, head)
}

func (c *testHeadCoordinator) FinalizeHead(ctx context.Context, head blockchain.BlockHeader) error {
	if c.finalize == nil {
		return nil
	}
	return c.finalize(ctx, head)
}

func (h *recordingHeadHandler) HandleBlock(_ context.Context, head blockchain.BlockHeader, _ []blockchain.RawLog) error {
	if h.started != nil {
		select {
		case h.started <- struct{}{}:
		default:
		}
	}
	if h.release != nil {
		<-h.release
	}
	if h.delay > 0 {
		time.Sleep(h.delay)
	}
	if h.failAt != 0 && head.Number == h.failAt {
		return errors.New("boom")
	}
	h.mu.Lock()
	h.heads = append(h.heads, head.Number)
	h.mu.Unlock()
	return nil
}

func (h *recordingHeadHandler) PrepareBlock(_ context.Context, head blockchain.BlockHeader, logs []blockchain.RawLog) (syncapp.PreparedBlock, error) {
	return &preparedBlockFunc{
		apply: func(ctx context.Context) error {
			return h.HandleBlock(ctx, head, logs)
		},
	}, nil
}

func (h *recordingHeadHandler) snapshot() []uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]uint64(nil), h.heads...)
}

type staticHeadSubscriber struct {
	heads []blockchain.BlockHeader
}

func (s *staticHeadSubscriber) SubscribeNewHead(ctx context.Context) (<-chan blockchain.BlockHeader, error) {
	out := make(chan blockchain.BlockHeader)
	go func() {
		defer close(out)
		for _, head := range s.heads {
			select {
			case <-ctx.Done():
				return
			case out <- head:
			}
		}
		<-ctx.Done()
	}()
	return out, nil
}

func newTestSharedHeadRunner(
	t *testing.T,
	subscriber syncapp.HeadSubscriber,
	handlers []syncapp.NamedHeadHandler,
	fetcher syncapp.HeadLogFetcher,
	blocks syncapp.CanonicalBlockReader,
) *syncapp.SharedHeadRunner {
	return newTestSharedHeadRunnerWithCoordinator(t, subscriber, handlers, fetcher, blocks, nil)
}

func newTestSharedHeadRunnerWithCoordinator(
	t *testing.T,
	subscriber syncapp.HeadSubscriber,
	handlers []syncapp.NamedHeadHandler,
	fetcher syncapp.HeadLogFetcher,
	blocks syncapp.CanonicalBlockReader,
	coordinator syncapp.HeadCoordinator,
) *syncapp.SharedHeadRunner {
	t.Helper()
	if subscriber == nil {
		subscriber = &staticHeadSubscriber{}
	}
	if fetcher == nil {
		fetcher = &recordingHeadLogFetcher{}
	}
	if blocks == nil {
		blocks = &sharedBlockReader{}
	}
	runner, err := syncapp.NewSharedHeadRunner(
		syncapp.SharedHeadDependencies{
			Subscriber:  subscriber,
			LogFetcher:  fetcher,
			Blocks:      blocks,
			Coordinator: coordinator,
		},
		handlers,
		16,
		nil,
	)
	if err != nil {
		t.Fatalf("new shared head runner: %v", err)
	}
	return runner
}

func TestNewSharedHeadRunnerRejectsIncompleteDependencies(t *testing.T) {
	valid := syncapp.SharedHeadDependencies{
		Subscriber: &staticHeadSubscriber{},
		LogFetcher: &recordingHeadLogFetcher{},
		Blocks:     &sharedBlockReader{},
	}
	validHandlers := []syncapp.NamedHeadHandler{{
		Name:    "test",
		Handler: &recordingBlockHandler{},
	}}
	tests := []struct {
		name     string
		deps     syncapp.SharedHeadDependencies
		handlers []syncapp.NamedHeadHandler
		depth    uint64
	}{
		{name: "subscriber", deps: syncapp.SharedHeadDependencies{LogFetcher: valid.LogFetcher, Blocks: valid.Blocks}, handlers: validHandlers, depth: 16},
		{name: "log fetcher", deps: syncapp.SharedHeadDependencies{Subscriber: valid.Subscriber, Blocks: valid.Blocks}, handlers: validHandlers, depth: 16},
		{name: "block reader", deps: syncapp.SharedHeadDependencies{Subscriber: valid.Subscriber, LogFetcher: valid.LogFetcher}, handlers: validHandlers, depth: 16},
		{name: "reorg depth", deps: valid, handlers: validHandlers},
		{name: "handlers", deps: valid, depth: 16},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := syncapp.NewSharedHeadRunner(test.deps, test.handlers, test.depth, nil); err == nil {
				t.Fatalf("expected missing %s to fail", test.name)
			}
		})
	}
}

func TestSharedHeadRunnerAppliesAllHandlersBeforeNextHead(t *testing.T) {
	slowStarted := make(chan struct{}, 1)
	slowRelease := make(chan struct{})
	slow := &recordingHeadHandler{name: "slow", started: slowStarted, release: slowRelease}
	fast := &recordingHeadHandler{name: "fast"}

	runner := newTestSharedHeadRunner(
		t,
		&staticHeadSubscriber{heads: []blockchain.BlockHeader{
			{Number: 10, Hash: common.HexToHash("0xa")},
			{Number: 11, Hash: common.HexToHash("0xb")},
		}},
		[]syncapp.NamedHeadHandler{
			{Name: "slow", Handler: slow},
			{Name: "fast", Handler: fast},
		},
		nil,
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runner.Run(ctx)
	}()

	select {
	case <-slowStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for slow handler to start on first head")
	}

	// Fast protocol may finish head 10, but must not see head 11 while slow is held.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		got := fast.snapshot()
		for _, number := range got {
			if number == 11 {
				t.Fatalf("fast handler advanced to head 11 before slow finished head 10: %v", got)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(slowRelease)
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("shared head runner: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for shared head runner exit")
	}

	if got := slow.snapshot(); len(got) < 1 || got[0] != 10 {
		t.Fatalf("expected slow to apply head 10 first, got %v", got)
	}
	if got := fast.snapshot(); len(got) < 1 || got[0] != 10 {
		t.Fatalf("expected fast to apply head 10 first, got %v", got)
	}
}

func TestSharedHeadRunnerHandleHeadFailsWhenAnyProtocolFails(t *testing.T) {
	ok := &recordingHeadHandler{name: "ok"}
	bad := &recordingHeadHandler{name: "bad", failAt: 5}
	runner := newTestSharedHeadRunner(t, nil, []syncapp.NamedHeadHandler{
		{Name: "ok", Handler: ok},
		{Name: "bad", Handler: bad},
	}, nil, nil)

	err := runner.HandleHead(context.Background(), blockchain.BlockHeader{Number: 5, Hash: common.HexToHash("0x5")})
	if err == nil {
		t.Fatal("expected shared head failure")
	}
}

func TestSharedHeadRunnerPublishesOnlyAfterAllProtocolsApply(t *testing.T) {
	order := make([]string, 0, 3)
	first := blockHandlerFunc(func(context.Context, blockchain.BlockHeader, []blockchain.RawLog) error {
		order = append(order, "univ3")
		return nil
	})
	second := blockHandlerFunc(func(context.Context, blockchain.BlockHeader, []blockchain.RawLog) error {
		order = append(order, "pancakev3")
		return nil
	})
	runner := newTestSharedHeadRunnerWithCoordinator(t, nil, []syncapp.NamedHeadHandler{
		{Name: "univ3", Handler: first},
		{Name: "pancakev3", Handler: second},
	}, nil, nil, &testHeadCoordinator{finalize: func(context.Context, blockchain.BlockHeader) error {
		order = append(order, "publish")
		return nil
	}})

	if err := runner.HandleHead(context.Background(), blockchain.BlockHeader{Number: 8}); err != nil {
		t.Fatalf("handle head: %v", err)
	}
	if got := strings.Join(order, ","); got != "univ3,pancakev3,publish" {
		t.Fatalf("unexpected apply order: %s", got)
	}
}

func TestSharedHeadRunnerDoesNotApplyWhenAnyProtocolPrepareFails(t *testing.T) {
	var applied atomic.Int32
	runner := newTestSharedHeadRunner(t, nil, []syncapp.NamedHeadHandler{
		{Name: "univ3", Handler: &preparingBlockHandler{applied: &applied}},
		{Name: "pancakev3", Handler: &preparingBlockHandler{prepareErr: errors.New("parse failed"), applied: &applied}},
	}, nil, nil)

	err := runner.HandleHead(context.Background(), blockchain.BlockHeader{Number: 9})
	if err == nil {
		t.Fatal("expected prepare failure")
	}
	if applied.Load() != 0 {
		t.Fatalf("expected no state apply after prepare failure, got %d", applied.Load())
	}
}

func TestSharedHeadRunnerRollsBackAppliedProtocolsWhenLaterApplyFails(t *testing.T) {
	var applied atomic.Int32
	var rolledBack atomic.Int32
	var published atomic.Bool
	runner := newTestSharedHeadRunnerWithCoordinator(t, nil, []syncapp.NamedHeadHandler{
		{Name: "univ3", Handler: &preparingBlockHandler{applied: &applied, rolledBack: &rolledBack}},
		{Name: "pancakev3", Handler: &preparingBlockHandler{applyErr: errors.New("apply failed"), applied: &applied, rolledBack: &rolledBack}},
	}, nil, nil, &testHeadCoordinator{finalize: func(context.Context, blockchain.BlockHeader) error {
		published.Store(true)
		return nil
	}})

	err := runner.HandleHead(context.Background(), blockchain.BlockHeader{Number: 9})
	if err == nil {
		t.Fatal("expected apply failure")
	}
	if applied.Load() != 2 {
		t.Fatalf("expected both protocols to attempt apply, got %d", applied.Load())
	}
	if rolledBack.Load() != 2 {
		t.Fatalf("expected both attempted protocols to roll back, got %d", rolledBack.Load())
	}
	if published.Load() {
		t.Fatal("failed block must not be published")
	}
}

func TestSharedHeadRunnerDetectsReorgOnceAndRecoversEveryProtocol(t *testing.T) {
	var applied atomic.Int32
	var recovered atomic.Int32
	var replayed atomic.Int32
	fetcher := &recordingHeadLogFetcher{}
	oldHeaders := make(map[uint64]blockchain.BlockHeader, 11)
	for blockNumber := uint64(0); blockNumber <= 10; blockNumber++ {
		hash := common.BigToHash(new(big.Int).SetUint64(blockNumber))
		var parent common.Hash
		if blockNumber > 0 {
			parent = oldHeaders[blockNumber-1].Hash
		}
		oldHeaders[blockNumber] = blockchain.BlockHeader{Number: blockNumber, Hash: hash, ParentHash: parent}
	}
	oldHeaders[10] = blockchain.BlockHeader{Number: 10, Hash: common.HexToHash("0xa10"), ParentHash: oldHeaders[9].Hash}
	blocks := &sharedBlockReader{headers: oldHeaders}
	runner := newTestSharedHeadRunner(t, nil, []syncapp.NamedHeadHandler{
		{Name: "univ3", Handler: &preparingBlockHandler{applied: &applied, recovered: &recovered, replayed: &replayed, replayFrom: 10}},
		{Name: "univ4", Handler: &preparingBlockHandler{applied: &applied, recovered: &recovered, replayed: &replayed, replayFrom: 10}},
	}, fetcher, blocks)
	if err := runner.InitializeLocalHead(context.Background(), blockchain.BlockHeader{
		Number: 10,
		Hash:   common.HexToHash("0xa10"),
	}); err != nil {
		t.Fatalf("initialize local head: %v", err)
	}
	blocks.headers[10] = blockchain.BlockHeader{Number: 10, Hash: common.HexToHash("0xb10"), ParentHash: oldHeaders[9].Hash}
	blocks.headers[11] = blockchain.BlockHeader{Number: 11, Hash: common.HexToHash("0xb11"), ParentHash: blocks.headers[10].Hash}

	err := runner.HandleHead(context.Background(), blockchain.BlockHeader{
		Number:     11,
		Hash:       common.HexToHash("0xb11"),
		ParentHash: common.HexToHash("0xb10"),
	})
	if err != nil {
		t.Fatalf("handle reorg head: %v", err)
	}
	if recovered.Load() != 2 {
		t.Fatalf("expected both protocols to recover, got %d", recovered.Load())
	}
	if applied.Load() != 0 {
		t.Fatalf("reorg replay must replace normal apply, got %d applies", applied.Load())
	}
	if replayed.Load() != 4 {
		t.Fatalf("expected two recovery blocks for both protocols, got %d applies", replayed.Load())
	}
	if fetcher.calls != 2 {
		t.Fatalf("expected one shared log fetch per recovery block, got %d", fetcher.calls)
	}
}

func TestSharedHeadRunnerCatchesUpEveryProtocolForHeadGap(t *testing.T) {
	var applied atomic.Int32
	fetcher := &recordingHeadLogFetcher{}
	blocks := &sharedBlockReader{headers: map[uint64]blockchain.BlockHeader{
		10: {Number: 10, Hash: common.HexToHash("0x10")},
		11: {Number: 11, Hash: common.HexToHash("0x11")},
		12: {Number: 12, Hash: common.HexToHash("0x12")},
	}}
	runner := newTestSharedHeadRunner(t, nil, []syncapp.NamedHeadHandler{
		{Name: "univ3", Handler: &preparingBlockHandler{applied: &applied}},
		{Name: "univ4", Handler: &preparingBlockHandler{applied: &applied}},
	}, fetcher, blocks)
	if err := runner.InitializeLocalHead(context.Background(), blockchain.BlockHeader{Number: 10, Hash: common.HexToHash("0x10")}); err != nil {
		t.Fatalf("initialize local head: %v", err)
	}

	err := runner.HandleHead(context.Background(), blockchain.BlockHeader{
		Number:     13,
		Hash:       common.HexToHash("0x13"),
		ParentHash: common.HexToHash("0x12"),
	})
	if err != nil {
		t.Fatalf("handle gap head: %v", err)
	}
	if applied.Load() != 6 {
		t.Fatalf("expected two gap blocks and current head applied by both protocols, got %d", applied.Load())
	}
	if fetcher.calls != 3 {
		t.Fatalf("expected one shared log fetch per canonical block, got %d", fetcher.calls)
	}
}

func TestSharedHeadRunnerFetchesLogsOnceForAllBlockHandlers(t *testing.T) {
	first := &recordingBlockHandler{}
	second := &recordingBlockHandler{}
	fetcher := &recordingHeadLogFetcher{logs: []blockchain.RawLog{{BlockNumber: 7}}}
	runner := newTestSharedHeadRunner(t, nil, []syncapp.NamedHeadHandler{
		{Name: "first", Handler: first},
		{Name: "second", Handler: second},
	}, fetcher, nil)

	hash := common.HexToHash("0x7")
	if err := runner.HandleHead(context.Background(), blockchain.BlockHeader{Number: 7, Hash: hash}); err != nil {
		t.Fatalf("handle shared block: %v", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("expected one shared log fetch, got %d", fetcher.calls)
	}
	if fetcher.hash != hash {
		t.Fatalf("expected block-hash-bound log fetch %s, got %s", hash.Hex(), fetcher.hash.Hex())
	}
	if first.logs != 1 || second.logs != 1 {
		t.Fatalf("expected both handlers to consume shared logs, got first=%d second=%d", first.logs, second.logs)
	}
}

func TestSharedHeadRunnerContinuesAfterApplyFailure(t *testing.T) {
	ok := &recordingHeadHandler{name: "ok"}
	bad := &recordingHeadHandler{name: "bad", failAt: 5}
	runner := newTestSharedHeadRunner(
		t,
		&staticHeadSubscriber{heads: []blockchain.BlockHeader{
			{Number: 5, Hash: common.HexToHash("0x5")},
			{Number: 6, Hash: common.HexToHash("0x6")},
		}},
		[]syncapp.NamedHeadHandler{
			{Name: "ok", Handler: ok},
			{Name: "bad", Handler: bad},
		},
		nil,
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runner.Run(ctx)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := ok.snapshot(); len(got) >= 1 && got[len(got)-1] == 6 {
			cancel()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("shared head runner: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for shared head runner exit")
	}

	got := ok.snapshot()
	if len(got) < 1 || got[len(got)-1] != 6 {
		t.Fatalf("expected ok handler to continue to head 6 after failure, got %v", got)
	}
}

func TestSharedHeadRunnerPreparesCoordinatorBeforeHandlers(t *testing.T) {
	prepared := false
	handler := &coordinatorOrderRecordingHandler{prepared: &prepared}
	runner := newTestSharedHeadRunnerWithCoordinator(
		t,
		nil,
		[]syncapp.NamedHeadHandler{{Name: "univ3", Handler: handler}},
		nil,
		nil,
		&testHeadCoordinator{prepare: func(context.Context, blockchain.BlockHeader) error {
			prepared = true
			return nil
		}},
	)
	if err := runner.HandleHead(context.Background(), blockchain.BlockHeader{Number: 9}); err != nil {
		t.Fatalf("handle head: %v", err)
	}
	if !handler.sawPrepared {
		t.Fatal("handler ran before the before-head hook")
	}
}

type coordinatorOrderRecordingHandler struct {
	prepared    *bool
	sawPrepared bool
}

func (h *coordinatorOrderRecordingHandler) HandleBlock(context.Context, blockchain.BlockHeader, []blockchain.RawLog) error {
	h.sawPrepared = *h.prepared
	return nil
}

func (h *coordinatorOrderRecordingHandler) PrepareBlock(_ context.Context, head blockchain.BlockHeader, logs []blockchain.RawLog) (syncapp.PreparedBlock, error) {
	return &preparedBlockFunc{
		apply: func(ctx context.Context) error {
			return h.HandleBlock(ctx, head, logs)
		},
	}, nil
}

type schedulerStartup struct {
	schedulerCalls *atomic.Int32
}

func (s *schedulerStartup) StartAll(context.Context, uint64) error   { return nil }
func (s *schedulerStartup) CatchUpAll(context.Context, uint64) error { return nil }
func (s *schedulerStartup) MarkAllPoolsReady(context.Context) error  { return nil }
func (s *schedulerStartup) SetSystemReady(bool)                      {}
func (s *schedulerStartup) RunScheduler(context.Context) error {
	s.schedulerCalls.Add(1)
	return nil
}

func TestRunStartupRunsScheduler(t *testing.T) {
	var schedulerCalls atomic.Int32
	err := syncapp.RunStartupAt(
		context.Background(),
		blockchain.BlockHeader{Number: 1, Hash: common.HexToHash("0x1")},
		&schedulerStartup{schedulerCalls: &schedulerCalls},
	)
	if err != nil {
		t.Fatalf("run startup: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if schedulerCalls.Load() != 1 {
		t.Fatalf("expected scheduler to start, got %d", schedulerCalls.Load())
	}
}
