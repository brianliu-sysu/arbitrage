package pancakev3

import (
	"math/big"
	"testing"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

func testPoolAddress() common.Address {
	return common.HexToAddress("0x00000000000000000000000000000000000000bb")
}

func sqrtPriceAtTick0() *big.Int {
	v, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	return v
}

func testMeta(block uint64) EventMeta {
	return EventMeta{
		PoolAddress: testPoolAddress(),
		BlockNumber: block,
	}
}

func TestPoolRef(t *testing.T) {
	address := testPoolAddress()
	pool := NewPool(address, common.Address{}, common.Address{}, 2500, 60)
	ref := pool.Ref()
	if ref.Protocol != market.ProtocolPancakeV3 {
		t.Fatalf("expected pancakev3 protocol, got %s", ref.Protocol)
	}
	if ref.Address != address {
		t.Fatalf("expected address %s, got %s", address, ref.Address)
	}
}

func TestPoolApplyInitialize(t *testing.T) {
	pool := NewPool(testPoolAddress(), common.Address{}, common.Address{}, 2500, 60)
	if err := pool.Apply(NewInitializeEvent(testMeta(1), sqrtPriceAtTick0(), 0)); err != nil {
		t.Fatalf("apply initialize: %v", err)
	}
	if !pool.State.IsInitialized() {
		t.Fatal("pool should be initialized")
	}
	if pool.Status != market.PoolStatusSyncing {
		t.Fatalf("expected syncing status, got %s", pool.Status)
	}
}

func TestSnapshotRestore(t *testing.T) {
	pool := NewPool(testPoolAddress(), common.Address{}, common.Address{}, 2500, 60)
	if err := pool.Apply(NewInitializeEvent(testMeta(1), sqrtPriceAtTick0(), 0)); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	amount := big.NewInt(5_000_000)
	if err := pool.Apply(NewMintEvent(testMeta(2), common.Address{}, common.Address{}, -120, 120, amount, big.NewInt(1), big.NewInt(1))); err != nil {
		t.Fatalf("apply mint: %v", err)
	}

	snapshot := NewSnapshot(pool, 2, time.Unix(0, 0).UTC())
	pool.State.Liquidity = big.NewInt(0)

	restored := NewPool(testPoolAddress(), common.Address{}, common.Address{}, 2500, 60)
	RestoreSnapshot(snapshot, restored)
	if restored.State.Liquidity.Cmp(amount) != 0 {
		t.Fatalf("expected restored liquidity %s, got %s", amount, restored.State.Liquidity)
	}
}
