package v4

import (
	"math/big"
	"testing"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

func testPoolID() PoolID {
	key := PoolKey{
		Currency0:   common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
		Currency1:   common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
		Fee:         3000,
		TickSpacing: 60,
		Hooks:       common.Address{},
	}
	id, err := ComputePoolID(key)
	if err != nil {
		panic(err)
	}
	return id
}

func sqrtPriceAtTick0() *big.Int {
	v, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	return v
}

func testMeta(block uint64) EventMeta {
	return EventMeta{
		PoolID:      testPoolID(),
		BlockNumber: block,
	}
}

func TestPoolApplyInitializeAndModifyLiquidity(t *testing.T) {
	key := PoolKey{
		Currency0:   common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
		Currency1:   common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
		Fee:         3000,
		TickSpacing: 60,
	}
	id, err := ComputePoolID(key)
	if err != nil {
		t.Fatalf("compute pool id: %v", err)
	}

	pool := NewPool(id, key)
	sqrtPrice := sqrtPriceAtTick0()
	meta := EventMeta{PoolID: id, BlockNumber: 1}

	if err := pool.Apply(NewInitializeEvent(meta, sqrtPrice, 0)); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if !pool.State.IsInitialized() {
		t.Fatal("pool should be initialized")
	}

	amount := big.NewInt(5_000_000)
	if err := pool.Apply(NewModifyLiquidityEvent(
		EventMeta{PoolID: id, BlockNumber: 2},
		common.Address{},
		-120, 120,
		amount,
		common.Hash{},
	)); err != nil {
		t.Fatalf("modify liquidity: %v", err)
	}

	tick, ok := pool.Ticks.Get(-120)
	if !ok || tick.LiquidityGross.Cmp(amount) != 0 {
		t.Fatalf("expected lower tick liquidity %s, got %#v ok=%v", amount, tick, ok)
	}
	if pool.Status != market.PoolStatusSyncing {
		t.Fatalf("expected syncing status, got %s", pool.Status)
	}
}

func TestPoolApplySkipsStaleEvent(t *testing.T) {
	pool := NewPool(testPoolID(), PoolKey{TickSpacing: 60})
	sqrtPrice := sqrtPriceAtTick0()
	if err := pool.Apply(NewInitializeEvent(testMeta(1), sqrtPrice, 0)); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	pool.LastBlockNumber = 100

	if err := pool.Apply(NewSwapEvent(
		testMeta(50),
		common.Address{},
		big.NewInt(-1), big.NewInt(1),
		sqrtPrice, big.NewInt(1000), 0, 3000,
	)); err != nil {
		t.Fatalf("expected stale swap to be skipped, got %v", err)
	}
	if pool.LastBlockNumber != 100 {
		t.Fatalf("expected last block to remain 100, got %d", pool.LastBlockNumber)
	}
}

func TestPoolRef(t *testing.T) {
	id := testPoolID()
	pool := NewPool(id, PoolKey{TickSpacing: 60})
	ref := pool.Ref()
	if ref.Protocol != market.ProtocolV4 {
		t.Fatalf("expected v4 protocol, got %s", ref.Protocol)
	}
	if ref.PoolID != id.Hash() {
		t.Fatalf("expected pool id %s, got %s", id, ref.PoolID.Hex())
	}
}

func TestSnapshotRestore(t *testing.T) {
	key := PoolKey{TickSpacing: 60}
	id := testPoolID()
	pool := NewPool(id, key)
	sqrtPrice := sqrtPriceAtTick0()
	if err := pool.Apply(NewInitializeEvent(testMeta(1), sqrtPrice, 0)); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	amount := big.NewInt(5_000_000)
	if err := pool.Apply(NewModifyLiquidityEvent(testMeta(2), common.Address{}, -120, 120, amount, common.Hash{})); err != nil {
		t.Fatalf("modify liquidity: %v", err)
	}

	snapshot := NewSnapshot(pool, 2, time.Unix(0, 0).UTC())
	pool.State.Liquidity = big.NewInt(0)

	restored := NewPool(id, key)
	snapshot.RestoreTo(restored)
	if restored.State.Liquidity.Cmp(amount) != 0 {
		t.Fatalf("expected restored liquidity %s, got %s", amount, restored.State.Liquidity)
	}
}
