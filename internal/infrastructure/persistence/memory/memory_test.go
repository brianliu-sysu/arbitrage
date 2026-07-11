package memory_test

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/memory"
	"github.com/ethereum/go-ethereum/common"
)

func testPool() *marketv3.Pool {
	pool := marketv3.NewPool(
		common.HexToAddress("0x0000000000000000000000000000000000000001"),
		common.HexToAddress("0x0000000000000000000000000000000000000002"),
		common.HexToAddress("0x0000000000000000000000000000000000000003"),
		3000,
		60,
	)
	sqrtPrice, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	pool.State.SqrtPriceX96 = sqrtPrice
	pool.State.Liquidity = big.NewInt(1000)
	pool.Status = market.PoolStatusReady
	pool.LastBlockNumber = 10
	if _, err := pool.Ticks.Update(0, big.NewInt(1000), false); err != nil {
		panic(err)
	}
	_ = pool.Bitmap.FlipTick(0, 60)
	return pool
}

func TestPoolRepositorySaveGet(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewPoolRepository()
	pool := testPool()

	if err := repo.Save(ctx, pool); err != nil {
		t.Fatalf("save pool: %v", err)
	}
	loaded, err := repo.Get(ctx, pool.Address)
	if err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if loaded.State.Liquidity.Cmp(pool.State.Liquidity) != 0 {
		t.Fatalf("liquidity mismatch")
	}
	if _, ok := loaded.Ticks.Get(0); !ok {
		t.Fatal("expected tick 0")
	}
}

func TestPoolRepositoryAdvanceSyncProgress(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewPoolRepository()
	pool := testPool()
	pool.Status = market.PoolStatusCatchingUp
	pool.LastBlockNumber = 10

	if err := repo.Save(ctx, pool); err != nil {
		t.Fatalf("save pool: %v", err)
	}

	if err := repo.AdvanceSyncProgress(ctx, pool.Address, 20); err != nil {
		t.Fatalf("advance sync progress: %v", err)
	}

	loaded, err := repo.Get(ctx, pool.Address)
	if err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if loaded.LastBlockNumber != 20 {
		t.Fatalf("expected last block 20, got %d", loaded.LastBlockNumber)
	}
	if loaded.Status != market.PoolStatusSyncing {
		t.Fatalf("expected syncing status, got %s", loaded.Status)
	}
	if _, ok := loaded.Ticks.Get(0); !ok {
		t.Fatal("expected tick data preserved")
	}
}

func TestSnapshotRepositorySaveGetLatest(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewSnapshotRepository()
	pool := testPool()
	snapshot := marketv3.NewSnapshot(pool, 10, time.Unix(0, 0).UTC())

	if err := repo.Save(ctx, snapshot); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	latest, err := repo.GetLatest(ctx, pool.Address)
	if err != nil || latest == nil || latest.BlockNumber != 10 {
		t.Fatalf("get latest snapshot: %#v err=%v", latest, err)
	}
}

func TestCheckpointRepositorySaveGet(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewCheckpointRepository()
	checkpoint := &blockchain.Checkpoint{
		PoolAddress: common.HexToAddress("0x1"),
		BlockNumber: 42,
		BlockHash:   common.HexToHash("0xabc"),
	}
	if err := repo.Save(ctx, checkpoint); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	loaded, err := repo.Get(ctx, checkpoint.PoolAddress)
	if err != nil || loaded == nil || loaded.BlockNumber != 42 {
		t.Fatalf("get checkpoint: %#v err=%v", loaded, err)
	}
}

func TestCheckpointRepositorySaveMany(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewCheckpointRepository()
	checkpoints := []*blockchain.Checkpoint{
		{
			PoolAddress: common.HexToAddress("0x1"),
			BlockNumber: 10,
			BlockHash:   common.HexToHash("0xa"),
		},
		{
			PoolAddress: common.HexToAddress("0x2"),
			BlockNumber: 20,
			BlockHash:   common.HexToHash("0xb"),
		},
	}
	if err := repo.SaveMany(ctx, checkpoints); err != nil {
		t.Fatalf("save checkpoints: %v", err)
	}
	for _, checkpoint := range checkpoints {
		loaded, err := repo.Get(ctx, checkpoint.PoolAddress)
		if err != nil || loaded == nil || loaded.BlockNumber != checkpoint.BlockNumber {
			t.Fatalf("get checkpoint %s: %#v err=%v", checkpoint.PoolAddress.Hex(), loaded, err)
		}
	}
}

func TestOpportunityRepositorySaveList(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewOpportunityRepository()
	item := &arbitrage.Opportunity{
		ID:          "opp-1",
		PoolAddress: common.HexToAddress("0x1"),
		BlockNumber: 100,
		Payload:     []byte(`{"profit":1}`),
		CreatedAt:   time.Unix(0, 0).UTC(),
	}
	if err := repo.Save(ctx, item); err != nil {
		t.Fatalf("save opportunity: %v", err)
	}
	items, err := repo.List(ctx, 10)
	if err != nil || len(items) != 1 || items[0].ID != "opp-1" {
		t.Fatalf("list opportunities: %#v err=%v", items, err)
	}

	got, err := repo.Get(ctx, "opp-1")
	if err != nil {
		t.Fatalf("get opportunity: %v", err)
	}
	if got.ID != "opp-1" || got.BlockNumber != 100 {
		t.Fatalf("unexpected opportunity: %#v", got)
	}
	if _, err := repo.Get(ctx, "missing"); !errors.Is(err, arbitrage.ErrOpportunityNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestExportImportTicks(t *testing.T) {
	pool := testPool()
	exported := pool.Ticks.ExportTicks()
	imported := market.ImportTickTable(exported)
	tick, ok := imported.Get(0)
	if !ok || tick.LiquidityGross.Sign() == 0 {
		t.Fatal("expected imported tick liquidity")
	}
}
