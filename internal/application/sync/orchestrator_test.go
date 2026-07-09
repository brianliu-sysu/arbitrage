package syncapp_test

import (
	"context"
	"testing"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum/common"
)

type startupBlockReader struct {
	headers []blockchain.BlockHeader
	calls   int
}

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

	startCalls := make([]uint64, 0, 2)
	catchupCalls := make([]uint64, 0, 1)
	var localHead blockchain.BlockHeader

	err := syncapp.RunStartup(context.Background(), reader, syncapp.SyncPhases{
		StartAll: func(_ context.Context, blockNumber uint64) error {
			startCalls = append(startCalls, blockNumber)
			return nil
		},
		CatchUpAll: func(_ context.Context, blockNumber uint64) error {
			catchupCalls = append(catchupCalls, blockNumber)
			return nil
		},
		MarkPoolsReady: func(context.Context) error { return nil },
		SetLocalHead:   func(head blockchain.BlockHeader) { localHead = head },
		SetSystemReady: func(bool) {},
		RunHeadSync:    func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("run startup: %v", err)
	}

	if len(startCalls) != 1 || startCalls[0] != 100 {
		t.Fatalf("expected StartAll at initial head 100, got %v", startCalls)
	}
	if len(catchupCalls) != 1 || catchupCalls[0] != 100 {
		t.Fatalf("expected CatchUpAll at initial head 100, got %v", catchupCalls)
	}
	if localHead.Number != 100 || localHead.Hash != common.HexToHash("0x100") {
		t.Fatalf("expected local head to stay at caught-up block 100, got %+v", localHead)
	}
	if reader.calls != 1 {
		t.Fatalf("expected latest header to be read once, got %d", reader.calls)
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

type onboardingHeadSync struct {
	paused bool
}

func (h *onboardingHeadSync) Run(context.Context) error {
	return nil
}

func (h *onboardingHeadSync) SetLocalHead(blockchain.BlockHeader) {}

func (h *onboardingHeadSync) WithHeadSyncPaused(ctx context.Context, fn func(context.Context) error) error {
	h.paused = true
	return fn(ctx)
}

type onboardingBlockApply struct {
	marked    []int
	readiness *syncapp.ReadinessService[int]
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
	registered := make([]int, 0, 1)
	bootstrapped := make([]uint64, 0, 1)
	lifecycle := syncapp.NewPoolLifecycleService(readiness, syncapp.LifecycleHooks[int]{
		Bootstrap: func(_ context.Context, poolID int, blockNumber uint64) error {
			if poolID != 7 {
				t.Fatalf("unexpected pool id %d", poolID)
			}
			bootstrapped = append(bootstrapped, blockNumber)
			return nil
		},
		ListTracked: func(context.Context) ([]int, error) {
			return nil, nil
		},
		Register: func(_ context.Context, poolID int) error {
			registered = append(registered, poolID)
			return nil
		},
		Unregister: func(context.Context, int) error {
			return nil
		},
	})
	catchup := &onboardingCatchup{}
	headSync := &onboardingHeadSync{}
	blockApply := &onboardingBlockApply{readiness: readiness}

	orchestrator := syncapp.NewSyncOrchestrator[int](
		reader,
		lifecycle,
		catchup,
		headSync,
		blockApply,
		nil,
		readiness,
	)
	if err := orchestrator.AddPool(ctx, 7); err != nil {
		t.Fatalf("add pool: %v", err)
	}

	if !headSync.paused {
		t.Fatal("expected head sync to be paused during onboarding")
	}
	if len(registered) != 1 || registered[0] != 7 {
		t.Fatalf("expected pool registered once, got %v", registered)
	}
	if len(bootstrapped) != 1 || bootstrapped[0] != 100 {
		t.Fatalf("expected bootstrap at initial head 100, got %v", bootstrapped)
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
