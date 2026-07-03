package syncapp

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

type memorySnapshotRepo struct {
	latest map[common.Address]*market.Snapshot
}

func (r *memorySnapshotRepo) Save(_ context.Context, snapshot *market.Snapshot) error {
	r.latest[snapshot.PoolAddress] = snapshot
	return nil
}

func (r *memorySnapshotRepo) GetLatest(_ context.Context, poolAddress common.Address) (*market.Snapshot, error) {
	return r.latest[poolAddress], nil
}

func (r *memorySnapshotRepo) GetAtBlock(_ context.Context, poolAddress common.Address, blockNumber uint64) (*market.Snapshot, error) {
	snapshot := r.latest[poolAddress]
	if snapshot == nil || snapshot.BlockNumber != blockNumber {
		return nil, nil
	}
	return snapshot, nil
}

func (r *memorySnapshotRepo) DeleteAfterBlock(_ context.Context, poolAddress common.Address, blockNumber uint64) error {
	snapshot := r.latest[poolAddress]
	if snapshot != nil && snapshot.BlockNumber > blockNumber {
		delete(r.latest, poolAddress)
	}
	return nil
}

func TestRestorePoolSkipsOlderSnapshot(t *testing.T) {
	ctx := context.Background()
	address := common.HexToAddress("0x0000000000000000000000000000000000000001")

	pool := market.NewPool(address, common.Address{}, common.Address{}, 3000, 60)
	sqrtPrice, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	pool.State.SqrtPriceX96 = sqrtPrice
	pool.State.Tick = 0
	pool.LastBlockNumber = 200
	pool.Ticks.GetOrCreate(-120).LiquidityGross = big.NewInt(999)

	repo := &memorySnapshotRepo{latest: make(map[common.Address]*market.Snapshot)}
	olderPool := market.NewPool(address, common.Address{}, common.Address{}, 3000, 60)
	olderPool.State.SqrtPriceX96 = sqrtPrice
	olderPool.State.Tick = 0
	olderPool.Ticks.GetOrCreate(-120).LiquidityGross = big.NewInt(1)
	repo.latest[address] = market.NewSnapshot(olderPool, 100, time.Unix(0, 0).UTC())

	service := NewSnapshotService(repo, SnapshotPolicy{BlockInterval: 1000})
	if _, err := service.RestorePool(ctx, pool); err != nil {
		t.Fatalf("restore pool: %v", err)
	}
	if pool.LastBlockNumber != 200 {
		t.Fatalf("expected pool last block to remain 200, got %d", pool.LastBlockNumber)
	}
	tick, ok := pool.Ticks.Get(-120)
	if !ok || tick.LiquidityGross.Cmp(big.NewInt(999)) != 0 {
		t.Fatalf("expected newer pool tick state to be preserved, got %#v ok=%v", tick, ok)
	}
}

func TestRestorePoolAppliesNewerSnapshot(t *testing.T) {
	ctx := context.Background()
	address := common.HexToAddress("0x0000000000000000000000000000000000000001")

	pool := market.NewPool(address, common.Address{}, common.Address{}, 3000, 60)
	sqrtPrice, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	pool.State.SqrtPriceX96 = sqrtPrice
	pool.State.Tick = 0
	pool.LastBlockNumber = 100
	pool.Ticks.GetOrCreate(-120).LiquidityGross = big.NewInt(1)

	repo := &memorySnapshotRepo{latest: make(map[common.Address]*market.Snapshot)}
	newerPool := market.NewPool(address, common.Address{}, common.Address{}, 3000, 60)
	newerPool.State.SqrtPriceX96 = sqrtPrice
	newerPool.State.Tick = 0
	newerPool.Ticks.GetOrCreate(-120).LiquidityGross = big.NewInt(500)
	repo.latest[address] = market.NewSnapshot(newerPool, 200, time.Unix(0, 0).UTC())

	service := NewSnapshotService(repo, SnapshotPolicy{BlockInterval: 1000})
	if _, err := service.RestorePool(ctx, pool); err != nil {
		t.Fatalf("restore pool: %v", err)
	}
	if pool.LastBlockNumber != 200 {
		t.Fatalf("expected pool last block to become 200, got %d", pool.LastBlockNumber)
	}
	tick, ok := pool.Ticks.Get(-120)
	if !ok || tick.LiquidityGross.Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("expected newer snapshot tick state, got %#v ok=%v", tick, ok)
	}
}
