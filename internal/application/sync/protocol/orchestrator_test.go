package protocol_test

import (
	"context"
	"testing"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum/common"
)

type startupBlockReader struct {
	headers []blockchain.BlockHeader
	calls   int
}

type startupRecorder struct {
	startCalls   []uint64
	catchupCalls []uint64
}

func (s *startupRecorder) StartAll(_ context.Context, blockNumber uint64) error {
	s.startCalls = append(s.startCalls, blockNumber)
	return nil
}

func (s *startupRecorder) CatchUpAll(_ context.Context, blockNumber uint64) error {
	s.catchupCalls = append(s.catchupCalls, blockNumber)
	return nil
}

func (s *startupRecorder) MarkAllPoolsReady(context.Context) error { return nil }
func (s *startupRecorder) SetSystemReady(bool)                     {}
func (s *startupRecorder) RunScheduler(context.Context) error      { return nil }

func (r *startupBlockReader) GetLatestBlockHeader(context.Context) (blockchain.BlockHeader, error) {
	idx := r.calls
	if idx >= len(r.headers) {
		idx = len(r.headers) - 1
	}
	r.calls++
	return r.headers[idx], nil
}

func (r *startupBlockReader) GetBlockHeader(_ context.Context, blockNumber uint64) (blockchain.BlockHeader, error) {
	for _, header := range r.headers {
		if header.Number == blockNumber {
			return header, nil
		}
	}
	return blockchain.BlockHeader{}, nil
}

func TestRunStartupUsesCaughtUpHeadForLiveSync(t *testing.T) {
	reader := &startupBlockReader{
		headers: []blockchain.BlockHeader{
			{Number: 100, Hash: common.HexToHash("0x100")},
			{Number: 105, Hash: common.HexToHash("0x105")},
		},
	}

	startup := &startupRecorder{
		startCalls:   make([]uint64, 0, 2),
		catchupCalls: make([]uint64, 0, 1),
	}
	err := syncapp.RunStartupAt(context.Background(), reader.headers[0], startup)
	if err != nil {
		t.Fatalf("run startup: %v", err)
	}

	if len(startup.startCalls) != 1 || startup.startCalls[0] != 100 {
		t.Fatalf("expected StartAll at initial head 100, got %v", startup.startCalls)
	}
	if len(startup.catchupCalls) != 1 || startup.catchupCalls[0] != 100 {
		t.Fatalf("expected CatchUpAll at initial head 100, got %v", startup.catchupCalls)
	}
}

type onboardingCatchup struct {
	calls []uint64
}

func (c *onboardingCatchup) CatchUpAll(context.Context, uint64) error {
	return nil
}

func (c *onboardingCatchup) CatchUpPool(_ context.Context, _ int, blockNumber uint64) error {
	c.calls = append(c.calls, blockNumber)
	return nil
}

type onboardingBlockConsumer struct {
	paused bool
}

func (h *onboardingBlockConsumer) SetLocalHead(blockchain.BlockHeader) {}

func (h *onboardingBlockConsumer) WithBlockConsumptionPaused(ctx context.Context, fn func(context.Context) error) error {
	h.paused = true
	return fn(ctx)
}

type onboardingBlockApply struct {
	marked    []int
	readiness *syncapp.ReadinessService[int]
}

type onboardingLifecycleProtocol struct {
	t            *testing.T
	registered   []int
	bootstrapped []uint64
}

func (p *onboardingLifecycleProtocol) Bootstrap(_ context.Context, poolID int, blockNumber uint64) error {
	if poolID != 7 {
		p.t.Fatalf("unexpected pool id %d", poolID)
	}
	p.bootstrapped = append(p.bootstrapped, blockNumber)
	return nil
}

func (p *onboardingLifecycleProtocol) ListTracked(context.Context) ([]int, error) {
	return nil, nil
}

func (p *onboardingLifecycleProtocol) Register(_ context.Context, poolID int) error {
	p.registered = append(p.registered, poolID)
	return nil
}

func (p *onboardingLifecycleProtocol) Unregister(context.Context, int) error {
	return nil
}

func (a *onboardingBlockApply) MarkPoolsReady(_ context.Context, pools []int) error {
	a.marked = append(a.marked, pools...)
	if a.readiness != nil {
		a.readiness.MarkPoolsReady(pools)
	}
	return nil
}

func TestSyncOrchestratorAddPoolCatchesUpStableHeadBeforeReady(t *testing.T) {
	ctx := context.Background()
	reader := &startupBlockReader{
		headers: []blockchain.BlockHeader{
			{Number: 100, Hash: common.HexToHash("0x100")},
			{Number: 101, Hash: common.HexToHash("0x101")},
			{Number: 101, Hash: common.HexToHash("0x101")},
		},
	}
	readiness := syncapp.NewReadinessService[int]()
	lifecycleProtocol := &onboardingLifecycleProtocol{
		t:            t,
		registered:   make([]int, 0, 1),
		bootstrapped: make([]uint64, 0, 1),
	}
	lifecycle := syncapp.NewPoolLifecycleService(readiness, lifecycleProtocol)
	catchup := &onboardingCatchup{}
	blockConsumer := &onboardingBlockConsumer{}
	blockApply := &onboardingBlockApply{readiness: readiness}

	orchestrator := syncapp.NewSyncOrchestrator[int](
		reader,
		lifecycle,
		catchup,
		blockConsumer,
		blockApply,
		nil,
		readiness,
	)
	if err := orchestrator.AddPool(ctx, 7); err != nil {
		t.Fatalf("add pool: %v", err)
	}

	if !blockConsumer.paused {
		t.Fatal("expected block consumption to be paused during onboarding")
	}
	if len(lifecycleProtocol.registered) != 1 || lifecycleProtocol.registered[0] != 7 {
		t.Fatalf("expected pool registered once, got %v", lifecycleProtocol.registered)
	}
	if len(lifecycleProtocol.bootstrapped) != 1 || lifecycleProtocol.bootstrapped[0] != 100 {
		t.Fatalf("expected bootstrap at initial head 100, got %v", lifecycleProtocol.bootstrapped)
	}
	if len(catchup.calls) != 2 || catchup.calls[0] != 100 || catchup.calls[1] != 101 {
		t.Fatalf("expected catchup to stable heads [100 101], got %v", catchup.calls)
	}
	if len(blockApply.marked) != 1 || blockApply.marked[0] != 7 {
		t.Fatalf("expected pool marked ready once, got %v", blockApply.marked)
	}
	if !readiness.IsPoolReady(7) {
		t.Fatal("expected pool readiness to be true")
	}
	active := lifecycle.ListActive()
	if len(active) != 1 || active[0] != 7 {
		t.Fatalf("expected pool active after stable catchup, got %v", active)
	}
}
