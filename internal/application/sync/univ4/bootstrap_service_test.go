package syncv4

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

type stubV4BootstrapReader struct {
	sqrtPrice   *big.Int
	tick        int32
	liquidity   *big.Int
	blockNumber uint64
	requested   []uint64
}

func (r *stubV4BootstrapReader) ReadBootstrapData(
	_ context.Context,
	_ marketv4.PoolID,
	_ marketv4.PoolKey,
	blockNumber uint64,
) (*BootstrapData, error) {
	r.requested = append(r.requested, blockNumber)
	if blockNumber == 0 {
		blockNumber = r.blockNumber
	}
	return &BootstrapData{
		State: market.PoolState{
			SqrtPriceX96: new(big.Int).Set(r.sqrtPrice),
			Tick:         r.tick,
			Liquidity:    new(big.Int).Set(r.liquidity),
		},
		Ticks:       market.NewTickTable(),
		Bitmap:      market.NewTickBitmap(),
		BlockNumber: blockNumber,
	}, nil
}

type memoryV4SnapshotRepo struct {
	latest map[marketv4.PoolID]*marketv4.Snapshot
}

func (r *memoryV4SnapshotRepo) Save(_ context.Context, snapshot *marketv4.Snapshot) error {
	r.latest[snapshot.PoolID] = snapshot
	return nil
}

func (r *memoryV4SnapshotRepo) GetLatest(_ context.Context, poolID marketv4.PoolID) (*marketv4.Snapshot, error) {
	return r.latest[poolID], nil
}

func (r *memoryV4SnapshotRepo) GetAtBlock(_ context.Context, poolID marketv4.PoolID, blockNumber uint64) (*marketv4.Snapshot, error) {
	snapshot := r.latest[poolID]
	if snapshot == nil || snapshot.BlockNumber != blockNumber {
		return nil, nil
	}
	return snapshot, nil
}

func (r *memoryV4SnapshotRepo) DeleteAfterBlock(_ context.Context, poolID marketv4.PoolID, blockNumber uint64) error {
	snapshot := r.latest[poolID]
	if snapshot != nil && snapshot.BlockNumber > blockNumber {
		delete(r.latest, poolID)
	}
	return nil
}

type bootstrapV4PoolRepo struct {
	pool *marketv4.Pool
}

func (r *bootstrapV4PoolRepo) Save(_ context.Context, pool *marketv4.Pool) error {
	r.pool = pool.Clone()
	return nil
}

func (r *bootstrapV4PoolRepo) Get(_ context.Context, _ marketv4.PoolID) (*marketv4.Pool, error) {
	if r.pool == nil {
		return nil, nil
	}
	return r.pool.Clone(), nil
}

func (r *bootstrapV4PoolRepo) Delete(_ context.Context, _ marketv4.PoolID) error {
	r.pool = nil
	return nil
}

func (r *bootstrapV4PoolRepo) AdvanceSyncProgress(_ context.Context, _ marketv4.PoolID, blockNumber uint64) error {
	if r.pool != nil {
		r.pool.LastBlockNumber = blockNumber
	}
	return nil
}

func (r *bootstrapV4PoolRepo) AdvanceSyncProgressMany(_ context.Context, _ []marketv4.PoolID, blockNumber uint64) error {
	return r.AdvanceSyncProgress(context.Background(), marketv4.PoolID{}, blockNumber)
}

type stubV4Registry struct {
	key marketv4.PoolKey
}

func (r *stubV4Registry) List(context.Context) ([]marketv4.PoolID, error) { return nil, nil }
func (r *stubV4Registry) GetKey(_ context.Context, _ marketv4.PoolID) (marketv4.PoolKey, error) {
	return r.key, nil
}
func (r *stubV4Registry) Add(context.Context, marketv4.PoolID, marketv4.PoolKey) error { return nil }
func (r *stubV4Registry) Remove(context.Context, marketv4.PoolID) error                { return nil }

func TestBootstrapKeepsFreshChainStateForNewPool(t *testing.T) {
	ctx := context.Background()
	key := marketv4.PoolKey{
		Currency0:   common.Address{},
		Currency1:   common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"),
		Fee:         500,
		TickSpacing: 10,
		Hooks:       common.Address{},
	}
	poolID, err := marketv4.ComputePoolID(key)
	if err != nil {
		t.Fatalf("compute pool id: %v", err)
	}

	chainSqrt, _ := new(big.Int).SetString("3309125991494717474782430", 10)
	staleSqrt, _ := new(big.Int).SetString("3305675705130113121188626", 10)
	liquidity := big.NewInt(1162219549520577934)

	reader := &stubV4BootstrapReader{
		sqrtPrice:   chainSqrt,
		tick:        -201679,
		liquidity:   liquidity,
		blockNumber: 25_473_641,
	}

	snapRepo := &memoryV4SnapshotRepo{latest: make(map[marketv4.PoolID]*marketv4.Snapshot)}
	stalePool := marketv4.NewPool(poolID, key)
	stalePool.State.SqrtPriceX96 = staleSqrt
	stalePool.State.Tick = -201700
	stalePool.State.Liquidity = new(big.Int).Set(liquidity)
	snapRepo.latest[poolID] = marketv4.NewSnapshot(stalePool, 25_473_600, time.Unix(0, 0).UTC())

	service := NewBootstrapService(
		&bootstrapV4PoolRepo{},
		&stubV4Registry{key: key},
		reader,
		NewSnapshotService(snapRepo, SnapshotPolicy{BlockInterval: 1000}),
		1000,
	)

	pool, err := service.Bootstrap(ctx, poolID, 25_473_641)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if pool.State.Tick != -201679 {
		t.Fatalf("expected chain tick -201679, got %d", pool.State.Tick)
	}
	if pool.State.SqrtPriceX96.Cmp(chainSqrt) != 0 {
		t.Fatalf("expected chain sqrt price %s, got %s", chainSqrt, pool.State.SqrtPriceX96)
	}
	if pool.LastBlockNumber != 25_473_641 {
		t.Fatalf("expected last block 25473641, got %d", pool.LastBlockNumber)
	}
}

func TestBootstrapSkipsChainRefreshWhenSlightlyBehindHead(t *testing.T) {
	ctx := context.Background()
	key := marketv4.PoolKey{
		Currency0:   common.Address{},
		Currency1:   common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"),
		Fee:         500,
		TickSpacing: 10,
		Hooks:       common.Address{},
	}
	poolID, err := marketv4.ComputePoolID(key)
	if err != nil {
		t.Fatalf("compute pool id: %v", err)
	}

	chainSqrt, _ := new(big.Int).SetString("3309125991494717474782430", 10)
	reader := &stubV4BootstrapReader{
		sqrtPrice:   chainSqrt,
		tick:        -201679,
		liquidity:   big.NewInt(1162219549520577934),
		blockNumber: 25_473_776,
	}

	localSqrt, _ := new(big.Int).SetString("3305675705130113121188626", 10)
	localPool := marketv4.NewPool(poolID, key)
	localPool.State.SqrtPriceX96 = localSqrt
	localPool.State.Tick = -201700
	localPool.State.Liquidity = big.NewInt(1)
	localPool.LastBlockNumber = 25_473_700

	service := NewBootstrapService(
		&bootstrapV4PoolRepo{pool: localPool},
		&stubV4Registry{key: key},
		reader,
		nil,
		1000,
	)

	pool, err := service.Bootstrap(ctx, poolID, 25_473_776)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if pool.State.Tick != -201700 {
		t.Fatalf("expected local tick retained for catchup, got %d", pool.State.Tick)
	}
	if pool.LastBlockNumber != 25_473_700 {
		t.Fatalf("expected last block to remain 25473700, got %d", pool.LastBlockNumber)
	}
	if len(reader.requested) != 0 {
		t.Fatalf("expected no chain rebootstrap for small head lag, got %v", reader.requested)
	}
}
