package univ3

import (
	"math/big"
	"testing"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

func testPoolAddress() common.Address {
	return common.HexToAddress("0x0000000000000000000000000000000000000001")
}

func sqrtPriceAtTick0() *big.Int {
	v, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	return v
}

func sqrtPriceAfterSwap() *big.Int {
	v, _ := new(big.Int).SetString("79324200000000000000000000000", 10)
	return v
}

func testMeta(block uint64) EventMeta {
	return EventMeta{
		PoolAddress: testPoolAddress(),
		BlockNumber: block,
	}
}

func TestPoolApplyInitialize(t *testing.T) {
	pool := NewPool(testPoolAddress(), common.Address{}, common.Address{}, 3000, 60)
	sqrtPrice := sqrtPriceAtTick0()

	err := pool.Apply(NewInitializeEvent(testMeta(1), sqrtPrice, 0))
	if err != nil {
		t.Fatalf("apply initialize: %v", err)
	}
	if !pool.State.IsInitialized() {
		t.Fatal("pool should be initialized")
	}
	if pool.State.Tick != 0 {
		t.Fatalf("expected tick 0, got %d", pool.State.Tick)
	}
	if pool.Status != market.PoolStatusSyncing {
		t.Fatalf("expected syncing status, got %s", pool.Status)
	}

	err = pool.Apply(NewInitializeEvent(testMeta(2), sqrtPrice, 0))
	if err == nil {
		t.Fatal("expected error on double initialize")
	}
}

func TestPoolApplySwap(t *testing.T) {
	pool := NewPool(testPoolAddress(), common.Address{}, common.Address{}, 3000, 60)
	sqrtPrice := sqrtPriceAtTick0()
	if err := pool.Apply(NewInitializeEvent(testMeta(1), sqrtPrice, 0)); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	newPrice := sqrtPriceAfterSwap()
	liquidity := big.NewInt(1_000_000)
	err := pool.Apply(NewSwapEvent(
		testMeta(2),
		common.Address{}, common.Address{},
		big.NewInt(-100), big.NewInt(200),
		newPrice, liquidity, 60,
	))
	if err != nil {
		t.Fatalf("apply swap: %v", err)
	}
	if pool.State.Tick != 60 {
		t.Fatalf("expected tick 60, got %d", pool.State.Tick)
	}
	if pool.State.Liquidity.Cmp(liquidity) != 0 {
		t.Fatalf("expected liquidity %s, got %s", liquidity, pool.State.Liquidity)
	}
}

func TestPoolApplyMintUpdatesTicksAndLiquidity(t *testing.T) {
	pool := NewPool(testPoolAddress(), common.Address{}, common.Address{}, 3000, 60)
	sqrtPrice := sqrtPriceAtTick0()
	if err := pool.Apply(NewInitializeEvent(testMeta(1), sqrtPrice, 0)); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	amount := big.NewInt(5_000_000)
	if err := pool.Apply(NewMintEvent(testMeta(2), common.Address{}, common.Address{}, -120, 120, amount, big.NewInt(1), big.NewInt(1))); err != nil {
		t.Fatalf("apply mint: %v", err)
	}

	lower, ok := pool.Ticks.Get(-120)
	if !ok || lower.LiquidityGross.Cmp(amount) != 0 {
		t.Fatalf("lower tick liquidity gross not updated")
	}
	upper, ok := pool.Ticks.Get(120)
	if !ok || upper.LiquidityGross.Cmp(amount) != 0 {
		t.Fatalf("upper tick liquidity gross not updated")
	}
	if pool.State.Liquidity.Cmp(amount) != 0 {
		t.Fatalf("active liquidity should be %s, got %s", amount, pool.State.Liquidity)
	}

	initialized, err := pool.Bitmap.IsInitialized(-120, 60)
	if err != nil || !initialized {
		t.Fatal("lower tick should be marked initialized in bitmap")
	}
}

func TestPoolApplyBurnRemovesLiquidity(t *testing.T) {
	pool := NewPool(testPoolAddress(), common.Address{}, common.Address{}, 3000, 60)
	sqrtPrice := sqrtPriceAtTick0()
	if err := pool.Apply(NewInitializeEvent(testMeta(1), sqrtPrice, 0)); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	amount := big.NewInt(5_000_000)
	if err := pool.Apply(NewMintEvent(testMeta(2), common.Address{}, common.Address{}, -120, 120, amount, big.NewInt(1), big.NewInt(1))); err != nil {
		t.Fatalf("apply mint: %v", err)
	}
	if err := pool.Apply(NewBurnEvent(testMeta(3), common.Address{}, -120, 120, amount, big.NewInt(1), big.NewInt(1))); err != nil {
		t.Fatalf("apply burn: %v", err)
	}

	if _, ok := pool.Ticks.Get(-120); ok {
		t.Fatal("lower tick should be removed after full burn")
	}
	if pool.State.Liquidity.Sign() != 0 {
		t.Fatalf("active liquidity should be zero, got %s", pool.State.Liquidity)
	}
}

func TestPoolApplySkipsStaleEvent(t *testing.T) {
	pool := NewPool(testPoolAddress(), common.Address{}, common.Address{}, 3000, 60)
	sqrtPrice := sqrtPriceAtTick0()
	if err := pool.Apply(NewInitializeEvent(testMeta(1), sqrtPrice, 0)); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	pool.LastBlockNumber = 100

	err := pool.Apply(NewSwapEvent(
		testMeta(50),
		common.Address{}, common.Address{},
		big.NewInt(-1), big.NewInt(1),
		sqrtPriceAfterSwap(), big.NewInt(1000), 0,
	))
	if err != nil {
		t.Fatalf("expected stale swap to be skipped, got %v", err)
	}
	if pool.LastBlockNumber != 100 {
		t.Fatalf("expected last block to remain 100, got %d", pool.LastBlockNumber)
	}
}

func TestSnapshotRestore(t *testing.T) {
	pool := NewPool(testPoolAddress(), common.Address{}, common.Address{}, 3000, 60)
	sqrtPrice := sqrtPriceAtTick0()
	if err := pool.Apply(NewInitializeEvent(testMeta(1), sqrtPrice, 0)); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	amount := big.NewInt(5_000_000)
	if err := pool.Apply(NewMintEvent(testMeta(2), common.Address{}, common.Address{}, -120, 120, amount, big.NewInt(1), big.NewInt(1))); err != nil {
		t.Fatalf("apply mint: %v", err)
	}

	snapshot := NewSnapshot(pool, 2, time.Unix(0, 0).UTC())
	pool.State.Liquidity = big.NewInt(0)

	restored := NewPool(testPoolAddress(), common.Address{}, common.Address{}, 3000, 60)
	snapshot.RestoreTo(restored)
	if restored.State.Liquidity.Cmp(amount) != 0 {
		t.Fatalf("expected restored liquidity %s, got %s", amount, restored.State.Liquidity)
	}
}

func TestPoolSkipsOutOfOrderBlock(t *testing.T) {
	pool := NewPool(testPoolAddress(), common.Address{}, common.Address{}, 3000, 60)
	sqrtPrice := sqrtPriceAtTick0()
	meta := testMeta(10)
	if err := pool.Apply(NewInitializeEvent(meta, sqrtPrice, 0)); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	oldMeta := testMeta(5)
	err := pool.Apply(NewSwapEvent(oldMeta, common.Address{}, common.Address{}, big.NewInt(1), big.NewInt(1), sqrtPrice, big.NewInt(1), 0))
	if err != nil {
		t.Fatalf("expected stale swap to be skipped, got %v", err)
	}
	if pool.LastBlockNumber != 10 {
		t.Fatalf("expected last block to remain 10, got %d", pool.LastBlockNumber)
	}
}

func TestPoolRef(t *testing.T) {
	address := testPoolAddress()
	pool := NewPool(address, common.Address{}, common.Address{}, 3000, 60)
	ref := pool.Ref()
	if ref.Protocol != market.ProtocolV3 {
		t.Fatalf("expected v3 protocol, got %s", ref.Protocol)
	}
	if ref.Address != address {
		t.Fatalf("expected address %s, got %s", address, ref.Address)
	}
}
