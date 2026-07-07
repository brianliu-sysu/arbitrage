package balancer

import (
	"math/big"
	"testing"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

func testBalancerPoolID() PoolID {
	return PoolID(common.HexToHash("0x1000000000000000000000000000000000000000000000000000000000000000"))
}

func testBalancerTokens() []common.Address {
	return []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000000001"),
		common.HexToAddress("0x0000000000000000000000000000000000000002"),
	}
}

func testBalancerMeta(block uint64) EventMeta {
	return EventMeta{
		PoolID:      testBalancerPoolID(),
		BlockNumber: block,
	}
}

func newWeightedTestPool(t *testing.T) *Pool {
	t.Helper()
	tokens := testBalancerTokens()
	pool, err := NewPool(testBalancerPoolID(), common.HexToAddress("0x00000000000000000000000000000000000000aa"), common.HexToAddress("0x00000000000000000000000000000000000000bb"), PoolTypeWeighted, tokens)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	pool.Balances[tokens[0]] = big.NewInt(1000)
	pool.Balances[tokens[1]] = big.NewInt(2000)
	pool.Weights[tokens[0]] = big.NewInt(500000000000000000)
	pool.Weights[tokens[1]] = big.NewInt(500000000000000000)
	return pool
}

func TestPoolApplySwapUpdatesBalances(t *testing.T) {
	pool := newWeightedTestPool(t)
	tokens := testBalancerTokens()

	err := pool.Apply(NewSwapEvent(testBalancerMeta(10), tokens[0], tokens[1], big.NewInt(100), big.NewInt(90)))
	if err != nil {
		t.Fatalf("apply swap: %v", err)
	}
	if got := pool.Balances[tokens[0]]; got.Cmp(big.NewInt(1100)) != 0 {
		t.Fatalf("expected token in balance 1100, got %s", got)
	}
	if got := pool.Balances[tokens[1]]; got.Cmp(big.NewInt(1910)) != 0 {
		t.Fatalf("expected token out balance 1910, got %s", got)
	}
	if pool.Status != market.PoolStatusSyncing {
		t.Fatalf("expected syncing status, got %s", pool.Status)
	}
	if pool.LastBlockNumber != 10 {
		t.Fatalf("expected last block 10, got %d", pool.LastBlockNumber)
	}
}

func TestPoolApplyPoolBalanceChanged(t *testing.T) {
	pool := newWeightedTestPool(t)
	tokens := testBalancerTokens()

	err := pool.Apply(NewPoolBalanceChangedEvent(
		testBalancerMeta(11),
		tokens,
		[]*big.Int{big.NewInt(-100), big.NewInt(250)},
	))
	if err != nil {
		t.Fatalf("apply balance changed: %v", err)
	}
	if got := pool.Balances[tokens[0]]; got.Cmp(big.NewInt(900)) != 0 {
		t.Fatalf("expected first balance 900, got %s", got)
	}
	if got := pool.Balances[tokens[1]]; got.Cmp(big.NewInt(2250)) != 0 {
		t.Fatalf("expected second balance 2250, got %s", got)
	}
}

func TestPoolRejectsNegativeBalance(t *testing.T) {
	pool := newWeightedTestPool(t)
	tokens := testBalancerTokens()

	err := pool.Apply(NewSwapEvent(testBalancerMeta(12), tokens[0], tokens[1], big.NewInt(1), big.NewInt(3000)))
	if err == nil {
		t.Fatal("expected negative balance error")
	}
}

func TestStablePoolApplyAmplificationUpdated(t *testing.T) {
	tokens := testBalancerTokens()
	pool, err := NewPool(testBalancerPoolID(), common.Address{}, common.Address{}, PoolTypeStable, tokens)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}

	err = pool.Apply(NewAmplificationUpdatedEvent(testBalancerMeta(13), big.NewInt(1500)))
	if err != nil {
		t.Fatalf("apply amplification: %v", err)
	}
	if pool.Amplification.Cmp(big.NewInt(1500)) != 0 {
		t.Fatalf("expected amplification 1500, got %s", pool.Amplification)
	}
}

func TestWeightedPoolRejectsAmplificationUpdated(t *testing.T) {
	pool := newWeightedTestPool(t)
	err := pool.Apply(NewAmplificationUpdatedEvent(testBalancerMeta(14), big.NewInt(1500)))
	if err == nil {
		t.Fatal("expected weighted pool amplification error")
	}
}

func TestSnapshotRestore(t *testing.T) {
	pool := newWeightedTestPool(t)
	tokens := testBalancerTokens()
	snapshot := NewSnapshot(pool, 20, time.Unix(0, 0).UTC())

	pool.Balances[tokens[0]] = big.NewInt(1)
	restored := newWeightedTestPool(t)
	snapshot.RestoreTo(restored)

	if got := restored.Balances[tokens[0]]; got.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("expected restored balance 1000, got %s", got)
	}
	if restored.LastBlockNumber != 20 {
		t.Fatalf("expected restored block 20, got %d", restored.LastBlockNumber)
	}
}
