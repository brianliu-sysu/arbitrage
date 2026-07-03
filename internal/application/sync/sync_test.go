package syncapp_test

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

type memoryPoolRepo struct {
	pools map[common.Address]*market.Pool
}

func newMemoryPoolRepo() *memoryPoolRepo {
	return &memoryPoolRepo{pools: make(map[common.Address]*market.Pool)}
}

func (r *memoryPoolRepo) Save(_ context.Context, pool *market.Pool) error {
	r.pools[pool.Address] = pool.Clone()
	return nil
}

func (r *memoryPoolRepo) Get(_ context.Context, address common.Address) (*market.Pool, error) {
	pool, ok := r.pools[address]
	if !ok {
		return nil, nil
	}
	return pool.Clone(), nil
}

func (r *memoryPoolRepo) Delete(_ context.Context, address common.Address) error {
	delete(r.pools, address)
	return nil
}

func (r *memoryPoolRepo) AdvanceSyncProgress(ctx context.Context, address common.Address, blockNumber uint64) error {
	return r.AdvanceSyncProgressMany(ctx, []common.Address{address}, blockNumber)
}

func (r *memoryPoolRepo) AdvanceSyncProgressMany(_ context.Context, addresses []common.Address, blockNumber uint64) error {
	for _, address := range addresses {
		pool, ok := r.pools[address]
		if !ok || pool == nil {
			return fmt.Errorf("pool %s not found", address.Hex())
		}
		if blockNumber > pool.LastBlockNumber {
			pool.LastBlockNumber = blockNumber
		}
		if pool.Status == market.PoolStatusCatchingUp {
			pool.Status = market.PoolStatusSyncing
		}
	}
	return nil
}

type memoryCheckpointRepo struct {
	checkpoints map[common.Address]*blockchain.Checkpoint
}

func newMemoryCheckpointRepo() *memoryCheckpointRepo {
	return &memoryCheckpointRepo{checkpoints: make(map[common.Address]*blockchain.Checkpoint)}
}

func (r *memoryCheckpointRepo) Save(ctx context.Context, checkpoint *blockchain.Checkpoint) error {
	return r.SaveMany(ctx, []*blockchain.Checkpoint{checkpoint})
}

func (r *memoryCheckpointRepo) SaveMany(_ context.Context, checkpoints []*blockchain.Checkpoint) error {
	for _, checkpoint := range checkpoints {
		if checkpoint == nil {
			continue
		}
		copyCheckpoint := *checkpoint
		r.checkpoints[checkpoint.PoolAddress] = &copyCheckpoint
	}
	return nil
}

func (r *memoryCheckpointRepo) Get(_ context.Context, poolAddress common.Address) (*blockchain.Checkpoint, error) {
	checkpoint, ok := r.checkpoints[poolAddress]
	if !ok {
		return nil, nil
	}
	copyCheckpoint := *checkpoint
	return &copyCheckpoint, nil
}

func (r *memoryCheckpointRepo) Delete(_ context.Context, poolAddress common.Address) error {
	delete(r.checkpoints, poolAddress)
	return nil
}

type memorySnapshotRepo struct {
	snapshots map[common.Address][]*market.Snapshot
}

func newMemorySnapshotRepo() *memorySnapshotRepo {
	return &memorySnapshotRepo{snapshots: make(map[common.Address][]*market.Snapshot)}
}

func (r *memorySnapshotRepo) Save(_ context.Context, snapshot *market.Snapshot) error {
	r.snapshots[snapshot.PoolAddress] = append(r.snapshots[snapshot.PoolAddress], snapshot)
	return nil
}

func (r *memorySnapshotRepo) GetLatest(_ context.Context, poolAddress common.Address) (*market.Snapshot, error) {
	items := r.snapshots[poolAddress]
	if len(items) == 0 {
		return nil, nil
	}
	return items[len(items)-1], nil
}

func (r *memorySnapshotRepo) GetAtBlock(_ context.Context, poolAddress common.Address, blockNumber uint64) (*market.Snapshot, error) {
	for i := len(r.snapshots[poolAddress]) - 1; i >= 0; i-- {
		if r.snapshots[poolAddress][i].BlockNumber == blockNumber {
			return r.snapshots[poolAddress][i], nil
		}
	}
	return nil, nil
}

func (r *memorySnapshotRepo) DeleteAfterBlock(_ context.Context, poolAddress common.Address, blockNumber uint64) error {
	items := r.snapshots[poolAddress]
	kept := make([]*market.Snapshot, 0, len(items))
	for _, snapshot := range items {
		if snapshot.BlockNumber <= blockNumber {
			kept = append(kept, snapshot)
		}
	}
	r.snapshots[poolAddress] = kept
	return nil
}

type memoryRegistry struct {
	pools []common.Address
}

func newMemoryRegistry(addresses ...common.Address) *memoryRegistry {
	return &memoryRegistry{pools: append([]common.Address(nil), addresses...)}
}

func (r *memoryRegistry) List(context.Context) ([]common.Address, error) {
	return append([]common.Address(nil), r.pools...), nil
}

func (r *memoryRegistry) Add(_ context.Context, address common.Address) error {
	r.pools = append(r.pools, address)
	return nil
}

func (r *memoryRegistry) Remove(_ context.Context, address common.Address) error {
	filtered := r.pools[:0]
	for _, item := range r.pools {
		if item != address {
			filtered = append(filtered, item)
		}
	}
	r.pools = filtered
	return nil
}

type stubBootstrapReader struct{}

func (stubBootstrapReader) ReadBootstrapData(_ context.Context, poolAddress common.Address, _ uint64) (*syncapp.BootstrapData, error) {
	sqrtPrice, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	return &syncapp.BootstrapData{
		TickSpacing: 60,
		Fee:         3000,
		State: market.PoolState{
			SqrtPriceX96: sqrtPrice,
			Tick:         0,
			Liquidity:    big.NewInt(0),
		},
		Ticks:  market.NewTickTable(),
		Bitmap: market.NewTickBitmap(),
	}, nil
}

type stubLogFetcher struct {
	events []market.PoolEvent
}

func (f *stubLogFetcher) FetchLogs(_ context.Context, filter syncapp.LogFilter) ([]syncapp.RawLog, error) {
	_ = filter
	return nil, nil
}

type countingLogFetcher struct {
	calls      int
	lastFilter syncapp.LogFilter
}

func (f *countingLogFetcher) FetchLogs(_ context.Context, filter syncapp.LogFilter) ([]syncapp.RawLog, error) {
	f.calls++
	f.lastFilter = filter
	return nil, nil
}

type stubParser struct {
	events []market.PoolEvent
}

func (p *stubParser) ParsePoolEvents(_ []syncapp.RawLog) ([]market.PoolEvent, error) {
	return p.events, nil
}

type stubBlockReader struct {
	headers map[uint64]blockchain.BlockHeader
}

func newStubBlockReader(headers ...blockchain.BlockHeader) *stubBlockReader {
	reader := &stubBlockReader{headers: make(map[uint64]blockchain.BlockHeader)}
	for _, header := range headers {
		reader.headers[header.Number] = header
	}
	return reader
}

func (r *stubBlockReader) GetBlockHeader(_ context.Context, blockNumber uint64) (blockchain.BlockHeader, error) {
	return r.headers[blockNumber], nil
}

func (r *stubBlockReader) GetLatestBlockHeader(_ context.Context) (blockchain.BlockHeader, error) {
	var latest blockchain.BlockHeader
	for _, header := range r.headers {
		if header.Number >= latest.Number {
			latest = header
		}
	}
	return latest, nil
}

func testPoolAddress() common.Address {
	return common.HexToAddress("0x0000000000000000000000000000000000000001")
}

func TestBlockApplyServiceApplyBlock(t *testing.T) {
	ctx := context.Background()
	poolRepo := newMemoryPoolRepo()
	checkpointRepo := newMemoryCheckpointRepo()
	snapshotRepo := newMemorySnapshotRepo()
	readiness := syncapp.NewReadinessService()

	pool := market.NewPool(testPoolAddress(), common.Address{}, common.Address{}, 3000, 60)
	sqrtPrice, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	if err := pool.Apply(market.NewInitializeEvent(market.EventMeta{
		PoolAddress: testPoolAddress(),
		BlockNumber: 1,
	}, sqrtPrice, 0)); err != nil {
		t.Fatalf("initialize pool: %v", err)
	}
	pool.LastBlockNumber = 1
	if err := poolRepo.Save(ctx, pool); err != nil {
		t.Fatalf("save pool: %v", err)
	}

	snapshots := syncapp.NewSnapshotService(snapshotRepo, syncapp.SnapshotPolicy{BlockInterval: 1})
	blockApply := syncapp.NewBlockApplyService(poolRepo, checkpointRepo, snapshots, readiness, nil)

	swapEvent := market.NewSwapEvent(
		market.EventMeta{PoolAddress: testPoolAddress(), BlockNumber: 2},
		common.Address{}, common.Address{},
		big.NewInt(-1), big.NewInt(1),
		sqrtPrice, big.NewInt(1000), 0,
	)

	result, err := blockApply.ApplyBlock(ctx, syncapp.ApplyBlockRequest{
		BlockNumber: 2,
		BlockHash:   common.HexToHash("0x2"),
		Events:      []market.PoolEvent{swapEvent},
	})
	if err != nil {
		t.Fatalf("apply block: %v", err)
	}
	if len(result.ChangedPools) != 1 {
		t.Fatalf("expected 1 changed pool, got %d", len(result.ChangedPools))
	}

	checkpoint, err := checkpointRepo.Get(ctx, testPoolAddress())
	if err != nil || checkpoint == nil || checkpoint.BlockNumber != 2 {
		t.Fatalf("expected checkpoint at block 2, got %#v err=%v", checkpoint, err)
	}

	latestSnapshot, err := snapshotRepo.GetLatest(ctx, testPoolAddress())
	if err != nil || latestSnapshot == nil || latestSnapshot.BlockNumber != 2 {
		t.Fatalf("expected snapshot at block 2, got %#v err=%v", latestSnapshot, err)
	}
}

type recordingListener struct {
	calls int
}

func (l *recordingListener) OnPoolsChanged(_ context.Context, _ uint64, _ []common.Address) error {
	l.calls++
	return nil
}

func TestBlockApplyServiceSuppressListener(t *testing.T) {
	ctx := context.Background()
	poolRepo := newMemoryPoolRepo()
	checkpointRepo := newMemoryCheckpointRepo()
	readiness := syncapp.NewReadinessService()
	listener := &recordingListener{}

	pool := market.NewPool(testPoolAddress(), common.Address{}, common.Address{}, 3000, 60)
	pool.LastBlockNumber = 1
	if err := poolRepo.Save(ctx, pool); err != nil {
		t.Fatalf("save pool: %v", err)
	}

	blockApply := syncapp.NewBlockApplyService(poolRepo, checkpointRepo, nil, readiness, listener)
	_, err := blockApply.ApplyBlock(ctx, syncapp.ApplyBlockRequest{
		BlockNumber:      2,
		BlockHash:        common.HexToHash("0x2"),
		TrackedPools:     []common.Address{testPoolAddress()},
		SuppressListener: true,
	})
	if err != nil {
		t.Fatalf("apply block: %v", err)
	}
	if listener.calls != 0 {
		t.Fatalf("expected listener suppressed, got %d calls", listener.calls)
	}

	_, err = blockApply.ApplyBlock(ctx, syncapp.ApplyBlockRequest{
		BlockNumber:  3,
		BlockHash:    common.HexToHash("0x3"),
		TrackedPools: []common.Address{testPoolAddress()},
	})
	if err != nil {
		t.Fatalf("apply block with listener: %v", err)
	}
	if listener.calls != 1 {
		t.Fatalf("expected listener called once, got %d calls", listener.calls)
	}
}

func TestBlockApplyServiceMarkPoolsReady(t *testing.T) {
	ctx := context.Background()
	poolRepo := newMemoryPoolRepo()
	readiness := syncapp.NewReadinessService()
	blockApply := syncapp.NewBlockApplyService(poolRepo, newMemoryCheckpointRepo(), nil, readiness, nil)

	pool := market.NewPool(testPoolAddress(), common.Address{}, common.Address{}, 3000, 60)
	pool.Status = market.PoolStatusCatchingUp
	if err := poolRepo.Save(ctx, pool); err != nil {
		t.Fatalf("save pool: %v", err)
	}

	if err := blockApply.MarkPoolsReady(ctx, []common.Address{testPoolAddress()}); err != nil {
		t.Fatalf("mark pools ready: %v", err)
	}

	loaded, err := poolRepo.Get(ctx, testPoolAddress())
	if err != nil {
		t.Fatalf("load pool: %v", err)
	}
	if loaded.Status != market.PoolStatusReady {
		t.Fatalf("expected ready status, got %s", loaded.Status)
	}
	if !readiness.IsPoolReady(testPoolAddress()) {
		t.Fatal("expected pool ready in readiness service")
	}
}

func TestCatchupServiceSkipsWhenPoolAheadOfCheckpoint(t *testing.T) {
	ctx := context.Background()
	poolRepo := newMemoryPoolRepo()
	checkpointRepo := newMemoryCheckpointRepo()
	registry := newMemoryRegistry(testPoolAddress())

	services := syncapp.NewServices(syncapp.ServiceDeps{
		Config:      syncapp.DefaultConfig(),
		Pools:       poolRepo,
		Checkpoints: checkpointRepo,
		Registry:    registry,
		Fetcher:     &stubLogFetcher{},
		Parser:      &stubParser{},
		Blocks:      newStubBlockReader(blockchain.BlockHeader{Number: 200, Hash: common.HexToHash("0x200")}),
		Bootstrap:   stubBootstrapReader{},
	})

	pool := market.NewPool(testPoolAddress(), common.Address{}, common.Address{}, 3000, 60)
	sqrtPrice, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	pool.State.SqrtPriceX96 = sqrtPrice
	pool.State.Tick = 0
	pool.LastBlockNumber = 200
	if err := poolRepo.Save(ctx, pool); err != nil {
		t.Fatalf("save pool: %v", err)
	}
	if err := checkpointRepo.Save(ctx, &blockchain.Checkpoint{
		PoolAddress: testPoolAddress(),
		BlockNumber: 150,
		BlockHash:   common.HexToHash("0x150"),
	}); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	if err := services.Catchup.CatchUpPool(ctx, testPoolAddress(), 200); err != nil {
		t.Fatalf("catch up pool: %v", err)
	}

	checkpoint, err := checkpointRepo.Get(ctx, testPoolAddress())
	if err != nil || checkpoint == nil || checkpoint.BlockNumber != 150 {
		t.Fatalf("expected checkpoint to remain at 150, got %#v err=%v", checkpoint, err)
	}
}

func TestCatchupServiceCatchUpPool(t *testing.T) {
	ctx := context.Background()
	poolRepo := newMemoryPoolRepo()
	checkpointRepo := newMemoryCheckpointRepo()
	snapshotRepo := newMemorySnapshotRepo()
	readiness := syncapp.NewReadinessService()
	registry := newMemoryRegistry(testPoolAddress())

	services := syncapp.NewServices(syncapp.ServiceDeps{
		Config:      syncapp.DefaultConfig(),
		Pools:       poolRepo,
		Checkpoints: checkpointRepo,
		Snapshots:   snapshotRepo,
		Registry:    registry,
		Fetcher:     &stubLogFetcher{},
		Parser:      &stubParser{},
		Blocks: newStubBlockReader(
			blockchain.BlockHeader{Number: 1, Hash: common.HexToHash("0x1")},
			blockchain.BlockHeader{Number: 2, Hash: common.HexToHash("0x2")},
		),
		Bootstrap: stubBootstrapReader{},
	})

	if err := services.Lifecycle.StartAll(ctx, 1); err != nil {
		t.Fatalf("start pools: %v", err)
	}

	if err := services.Catchup.CatchUpPool(ctx, testPoolAddress(), 2); err != nil {
		t.Fatalf("catch up pool: %v", err)
	}

	checkpoint, err := checkpointRepo.Get(ctx, testPoolAddress())
	if err != nil || checkpoint == nil || checkpoint.BlockNumber != 2 {
		t.Fatalf("expected checkpoint at block 2 after empty catchup, got %#v err=%v", checkpoint, err)
	}
	_ = readiness
}

func TestCatchupServiceCatchUpAllBatchesPools(t *testing.T) {
	ctx := context.Background()
	poolRepo := newMemoryPoolRepo()
	checkpointRepo := newMemoryCheckpointRepo()

	poolA := common.HexToAddress("0x1")
	poolB := common.HexToAddress("0x2")
	poolC := common.HexToAddress("0x3")
	registry := newMemoryRegistry(poolA, poolB, poolC)
	fetcher := &countingLogFetcher{}

	services := syncapp.NewServices(syncapp.ServiceDeps{
		Config:      syncapp.DefaultConfig(),
		Pools:       poolRepo,
		Checkpoints: checkpointRepo,
		Snapshots:   newMemorySnapshotRepo(),
		Registry:    registry,
		Fetcher:     fetcher,
		Parser:      &stubParser{},
		Blocks: newStubBlockReader(
			blockchain.BlockHeader{Number: 1, Hash: common.HexToHash("0x1")},
			blockchain.BlockHeader{Number: 2, Hash: common.HexToHash("0x2")},
			blockchain.BlockHeader{Number: 3, Hash: common.HexToHash("0x3")},
		),
		Bootstrap: stubBootstrapReader{},
	})

	if err := services.Lifecycle.StartAll(ctx, 1); err != nil {
		t.Fatalf("start pools: %v", err)
	}

	if err := services.Catchup.CatchUpAll(ctx, 3); err != nil {
		t.Fatalf("catch up all: %v", err)
	}

	if fetcher.calls != 1 {
		t.Fatalf("expected 1 batched log fetch for 3 pools, got %d", fetcher.calls)
	}
	if len(fetcher.lastFilter.PoolAddresses) != 3 {
		t.Fatalf("expected 3 pools in filter, got %d", len(fetcher.lastFilter.PoolAddresses))
	}

	for _, poolAddress := range []common.Address{poolA, poolB, poolC} {
		checkpoint, err := checkpointRepo.Get(ctx, poolAddress)
		if err != nil || checkpoint == nil || checkpoint.BlockNumber != 3 {
			t.Fatalf("expected checkpoint at block 3 for %s, got %#v err=%v", poolAddress.Hex(), checkpoint, err)
		}
	}
}

func TestReadinessServicePoolLevel(t *testing.T) {
	readiness := syncapp.NewReadinessService()
	pool := testPoolAddress()

	readiness.SetSystemReady(true)
	readiness.SetPoolReady(pool, true)
	if !readiness.IsPoolReady(pool) {
		t.Fatal("expected pool ready")
	}
	if !readiness.IsSystemReady() {
		t.Fatal("expected system ready")
	}

	readiness.SetPoolReady(pool, false)
	if readiness.IsSystemReady() {
		t.Fatal("expected system not ready when a pool is not ready")
	}
}

func TestSnapshotSchedulerRunOnce(t *testing.T) {
	ctx := context.Background()
	poolRepo := newMemoryPoolRepo()
	snapshotRepo := newMemorySnapshotRepo()
	readiness := syncapp.NewReadinessService()
	registry := newMemoryRegistry(testPoolAddress())

	services := syncapp.NewServices(syncapp.ServiceDeps{
		Pools:       poolRepo,
		Checkpoints: newMemoryCheckpointRepo(),
		Snapshots:   snapshotRepo,
		Registry:    registry,
		Bootstrap:   stubBootstrapReader{},
	})

	if err := services.Lifecycle.Start(ctx, testPoolAddress(), 5); err != nil {
		t.Fatalf("start pool: %v", err)
	}

	scheduler := syncapp.NewSnapshotScheduler(syncapp.Config{SnapshotFallback: time.Minute}, poolRepo, services.Snapshot, services.Lifecycle)
	if err := scheduler.RunOnce(ctx); err != nil {
		t.Fatalf("run snapshot scheduler: %v", err)
	}

	snapshot, err := snapshotRepo.GetLatest(ctx, testPoolAddress())
	if err != nil || snapshot == nil {
		t.Fatalf("expected fallback snapshot, got %#v err=%v", snapshot, err)
	}
	_ = readiness
}

func TestHeadSyncServiceHandleHead(t *testing.T) {
	ctx := context.Background()
	poolRepo := newMemoryPoolRepo()
	checkpointRepo := newMemoryCheckpointRepo()
	snapshotRepo := newMemorySnapshotRepo()
	registry := newMemoryRegistry(testPoolAddress())

	services := syncapp.NewServices(syncapp.ServiceDeps{
		Pools:       poolRepo,
		Checkpoints: checkpointRepo,
		Snapshots:   snapshotRepo,
		Registry:    registry,
		Fetcher:     &stubLogFetcher{},
		Parser:      &stubParser{},
		Bootstrap:   stubBootstrapReader{},
	})

	if err := services.Lifecycle.StartAll(ctx, 1); err != nil {
		t.Fatalf("start pools: %v", err)
	}
	services.Readiness.SetSystemReady(true)

	head := blockchain.BlockHeader{Number: 2, Hash: common.HexToHash("0x2"), ParentHash: common.HexToHash("0x1")}
	services.HeadSync.SetLocalHead(blockchain.BlockHeader{Number: 1, Hash: common.HexToHash("0x1")})

	if err := services.HeadSync.HandleHead(ctx, head); err != nil {
		t.Fatalf("handle head: %v", err)
	}
	if !services.Readiness.IsPoolReady(testPoolAddress()) {
		t.Fatal("expected pool ready after head sync")
	}
	loaded, err := poolRepo.Get(ctx, testPoolAddress())
	if err != nil {
		t.Fatalf("load pool: %v", err)
	}
	if loaded.Status != market.PoolStatusReady {
		t.Fatalf("expected ready status after head sync, got %s", loaded.Status)
	}
}
