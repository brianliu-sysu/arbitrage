package blockchain

import (
	"context"
	"math/big"
	"testing"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/brianliu-sysu/arbitrage/internal/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type fakeBlockLogFetcher struct {
	logs []types.Log
}

func (f fakeBlockLogFetcher) FetchBlockLogs(context.Context, uint64, []common.Address) ([]types.Log, error) {
	return append([]types.Log(nil), f.logs...), nil
}

type fakeReplayApplier struct {
	calls int
}

func (a *fakeReplayApplier) ApplyBlock(_ *pool.State, _ []types.Log) error {
	a.calls++
	return nil
}

type fakePoolRepo struct {
	saveBlocks []uint64
}

func (r *fakePoolRepo) Save(_ context.Context, s *storage.PoolSnapshot) error {
	r.saveBlocks = append(r.saveBlocks, s.BlockNumber)
	return nil
}
func (r *fakePoolRepo) SaveHistory(context.Context, *storage.PoolSnapshot) error { return nil }
func (r *fakePoolRepo) Load(context.Context, string, string) (*storage.PoolSnapshot, error) {
	return nil, nil
}
func (r *fakePoolRepo) LoadAll(context.Context, string) (map[string]*storage.PoolSnapshot, error) {
	return nil, nil
}
func (r *fakePoolRepo) LoadAllByStatus(context.Context, string, storage.SnapshotStatus) (map[string]*storage.PoolSnapshot, error) {
	return nil, nil
}
func (r *fakePoolRepo) ListSnapshotStatuses(context.Context, string) (map[string]storage.SnapshotStatus, error) {
	return nil, nil
}
func (r *fakePoolRepo) SetSnapshotStatus(context.Context, string, string, storage.SnapshotStatus) error {
	return nil
}
func (r *fakePoolRepo) LoadTokenMetadata(context.Context, string, string) (*storage.TokenMetadata, error) {
	return nil, nil
}
func (r *fakePoolRepo) SaveTokenMetadata(context.Context, *storage.TokenMetadata) error { return nil }
func (r *fakePoolRepo) Close()                                                          {}

type fakeSyncRepo struct {
	last uint64
}

func (r *fakeSyncRepo) GetLastProcessedBlock(context.Context, string) (uint64, error) {
	return r.last, nil
}

func (r *fakeSyncRepo) SetLastProcessedBlock(_ context.Context, _ string, block uint64) error {
	r.last = block
	return nil
}

func TestProcessBlockAppliesLoadedPoolSynchronously(t *testing.T) {
	addr := common.HexToAddress("0x1000000000000000000000000000000000000001")
	st := pool.NewState(addr, common.Address{}, common.Address{}, 3000)
	st.UpdateFromSwap(big.NewInt(1), 0, big.NewInt(1), 10)

	cache := pool.NewCache()
	cache.Set(addr, st)
	applier := &fakeReplayApplier{}
	poolRepo := &fakePoolRepo{}
	syncRepo := &fakeSyncRepo{}
	processor := NewUniswapV3BlockProcessor(
		"test-chain",
		cache,
		fakeBlockLogFetcher{logs: []types.Log{{Address: addr, BlockNumber: 11}}},
		applier,
		poolRepo,
		syncRepo,
		logx.Nop(),
		nil,
	)
	processor.SetPoolAddresses([]common.Address{addr})

	if err := processor.ProcessBlock(context.Background(), 11); err != nil {
		t.Fatalf("ProcessBlock: %v", err)
	}
	if applier.calls != 1 {
		t.Fatalf("applier calls=%d want 1", applier.calls)
	}
	if st.BlockNumber != 11 {
		t.Fatalf("pool block=%d want 11", st.BlockNumber)
	}
	if len(poolRepo.saveBlocks) != 1 || poolRepo.saveBlocks[0] != 11 {
		t.Fatalf("save blocks=%v want [11]", poolRepo.saveBlocks)
	}
	if syncRepo.last != 11 {
		t.Fatalf("sync last=%d want 11", syncRepo.last)
	}
	if st.PendingLen() != 0 {
		t.Fatalf("pending len=%d want 0", st.PendingLen())
	}
}

func TestProcessBlockQueuesLoadingPoolEvents(t *testing.T) {
	addr := common.HexToAddress("0x2000000000000000000000000000000000000002")
	st := pool.NewState(addr, common.Address{}, common.Address{}, 3000)
	st.UpdateFromSwap(big.NewInt(1), 0, big.NewInt(1), 10)
	st.BeginLoading()

	cache := pool.NewCache()
	cache.Set(addr, st)
	applier := &fakeReplayApplier{}
	poolRepo := &fakePoolRepo{}
	syncRepo := &fakeSyncRepo{}
	processor := NewUniswapV3BlockProcessor(
		"test-chain",
		cache,
		fakeBlockLogFetcher{logs: []types.Log{{Address: addr, BlockNumber: 11}}},
		applier,
		poolRepo,
		syncRepo,
		logx.Nop(),
		nil,
	)
	processor.SetPoolAddresses([]common.Address{addr})

	if err := processor.ProcessBlock(context.Background(), 11); err != nil {
		t.Fatalf("ProcessBlock: %v", err)
	}
	if applier.calls != 0 {
		t.Fatalf("loading pool should not be applied synchronously, calls=%d", applier.calls)
	}
	if st.BlockNumber != 10 {
		t.Fatalf("pool block=%d want 10", st.BlockNumber)
	}
	if st.PendingLen() != 1 {
		t.Fatalf("pending len=%d want 1", st.PendingLen())
	}
	if len(poolRepo.saveBlocks) != 0 {
		t.Fatalf("loading pool should not be persisted synchronously, saves=%v", poolRepo.saveBlocks)
	}
	if syncRepo.last != 11 {
		t.Fatalf("sync last=%d want 11", syncRepo.last)
	}
}

func TestProcessBlockQueuesWhilePendingEventsDrain(t *testing.T) {
	addr := common.HexToAddress("0x3000000000000000000000000000000000000003")
	st := pool.NewState(addr, common.Address{}, common.Address{}, 3000)
	st.UpdateFromSwap(big.NewInt(1), 0, big.NewInt(1), 9)
	st.BeginLoading()
	st.ApplyBlockEvents(10, []types.Log{{Address: addr, BlockNumber: 10}})
	st.CompleteLoading()

	cache := pool.NewCache()
	cache.Set(addr, st)
	applier := &fakeReplayApplier{}
	syncRepo := &fakeSyncRepo{}
	processor := NewUniswapV3BlockProcessor(
		"test-chain",
		cache,
		fakeBlockLogFetcher{logs: []types.Log{{Address: addr, BlockNumber: 11}}},
		applier,
		&fakePoolRepo{},
		syncRepo,
		logx.Nop(),
		nil,
	)
	processor.SetPoolAddresses([]common.Address{addr})

	if err := processor.ProcessBlock(context.Background(), 11); err != nil {
		t.Fatalf("ProcessBlock: %v", err)
	}
	if applier.calls != 0 {
		t.Fatalf("pool with pending events should not be applied synchronously, calls=%d", applier.calls)
	}
	if st.BlockNumber != 9 {
		t.Fatalf("pool block=%d want 9", st.BlockNumber)
	}
	if st.PendingLen() != 2 {
		t.Fatalf("pending len=%d want 2", st.PendingLen())
	}
	if syncRepo.last != 11 {
		t.Fatalf("sync last=%d want 11", syncRepo.last)
	}
}

func TestFinishPoolLoadingDrainsPendingEventsSynchronously(t *testing.T) {
	addr := common.HexToAddress("0x4000000000000000000000000000000000000004")
	st := pool.NewState(addr, common.Address{}, common.Address{}, 3000)
	st.UpdateFromSwap(big.NewInt(1), 0, big.NewInt(1), 10)
	st.BeginLoading()
	st.ApplyBlockEvents(11, []types.Log{{Address: addr, BlockNumber: 11}})

	cache := pool.NewCache()
	cache.Set(addr, st)
	applier := &fakeReplayApplier{}
	poolRepo := &fakePoolRepo{}
	processor := NewUniswapV3BlockProcessor(
		"test-chain",
		cache,
		fakeBlockLogFetcher{},
		applier,
		poolRepo,
		&fakeSyncRepo{},
		logx.Nop(),
		nil,
	)

	if err := processor.FinishPoolLoading(context.Background(), addr); err != nil {
		t.Fatalf("FinishPoolLoading: %v", err)
	}
	if applier.calls != 1 {
		t.Fatalf("applier calls=%d want 1", applier.calls)
	}
	if st.BlockNumber != 11 {
		t.Fatalf("pool block=%d want 11", st.BlockNumber)
	}
	if st.PendingLen() != 0 {
		t.Fatalf("pending len=%d want 0", st.PendingLen())
	}
	if st.Loading() {
		t.Fatal("state should no longer be loading")
	}
	if len(poolRepo.saveBlocks) != 1 || poolRepo.saveBlocks[0] != 11 {
		t.Fatalf("save blocks=%v want [11]", poolRepo.saveBlocks)
	}
}
