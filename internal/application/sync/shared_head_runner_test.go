package syncapp_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
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

func (h *recordingHeadHandler) HandleHead(_ context.Context, head blockchain.BlockHeader) error {
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

func TestSharedHeadRunnerAppliesAllHandlersBeforeNextHead(t *testing.T) {
	slowStarted := make(chan struct{}, 1)
	slowRelease := make(chan struct{})
	slow := &recordingHeadHandler{name: "slow", started: slowStarted, release: slowRelease}
	fast := &recordingHeadHandler{name: "fast"}

	runner := syncapp.NewSharedHeadRunner(
		&staticHeadSubscriber{heads: []blockchain.BlockHeader{
			{Number: 10, Hash: common.HexToHash("0xa")},
			{Number: 11, Hash: common.HexToHash("0xb")},
		}},
		[]syncapp.NamedHeadHandler{
			{Name: "slow", Handler: slow},
			{Name: "fast", Handler: fast},
		},
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
	runner := syncapp.NewSharedHeadRunner(nil, []syncapp.NamedHeadHandler{
		{Name: "ok", Handler: ok},
		{Name: "bad", Handler: bad},
	}, nil)

	err := runner.HandleHead(context.Background(), blockchain.BlockHeader{Number: 5, Hash: common.HexToHash("0x5")})
	if err == nil {
		t.Fatal("expected shared head failure")
	}
}

func TestSharedHeadRunnerContinuesAfterApplyFailure(t *testing.T) {
	ok := &recordingHeadHandler{name: "ok"}
	bad := &recordingHeadHandler{name: "bad", failAt: 5}
	runner := syncapp.NewSharedHeadRunner(
		&staticHeadSubscriber{heads: []blockchain.BlockHeader{
			{Number: 5, Hash: common.HexToHash("0x5")},
			{Number: 6, Hash: common.HexToHash("0x6")},
		}},
		[]syncapp.NamedHeadHandler{
			{Name: "ok", Handler: ok},
			{Name: "bad", Handler: bad},
		},
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

func TestSharedHeadRunnerRunsBeforeHeadHookBeforeHandlers(t *testing.T) {
	prepared := false
	handler := &beforeHeadRecordingHandler{prepared: &prepared}
	runner := syncapp.NewSharedHeadRunner(nil, []syncapp.NamedHeadHandler{{Name: "univ3", Handler: handler}}, nil)
	runner.SetBeforeHead(func(context.Context, blockchain.BlockHeader) error {
		prepared = true
		return nil
	})
	if err := runner.HandleHead(context.Background(), blockchain.BlockHeader{Number: 9}); err != nil {
		t.Fatalf("handle head: %v", err)
	}
	if !handler.sawPrepared {
		t.Fatal("handler ran before the before-head hook")
	}
}

type beforeHeadRecordingHandler struct {
	prepared    *bool
	sawPrepared bool
}

func (h *beforeHeadRecordingHandler) HandleHead(context.Context, blockchain.BlockHeader) error {
	h.sawPrepared = *h.prepared
	return nil
}

func TestRunStartupSkipsHeadSyncWhenNil(t *testing.T) {
	var headSyncCalls atomic.Int32
	err := syncapp.RunStartup(context.Background(), &startupBlockReader{
		headers: []blockchain.BlockHeader{{Number: 1, Hash: common.HexToHash("0x1")}},
	}, syncapp.SyncPhases{
		StartAll:       func(context.Context, uint64) error { return nil },
		CatchUpAll:     func(context.Context, uint64) error { return nil },
		MarkPoolsReady: func(context.Context) error { return nil },
		SetLocalHead:   func(blockchain.BlockHeader) {},
		SetSystemReady: func(bool) {},
		RunHeadSync:    nil,
		RunScheduler: func(context.Context) error {
			headSyncCalls.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("run startup: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if headSyncCalls.Load() != 1 {
		t.Fatalf("expected scheduler to still start, got %d", headSyncCalls.Load())
	}
}
