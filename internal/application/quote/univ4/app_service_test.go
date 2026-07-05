package quoteuniv4_test

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	quoteapp "github.com/brianliu-sysu/uniswapv3/internal/application/quote"
	quoteuniv4 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/univ4"
	quoteuniv4domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ4"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

type memoryPoolRepo struct {
	pools map[marketv4.PoolID]*marketv4.Pool
}

func newMemoryPoolRepo() *memoryPoolRepo {
	return &memoryPoolRepo{pools: make(map[marketv4.PoolID]*marketv4.Pool)}
}

func (r *memoryPoolRepo) Save(_ context.Context, pool *marketv4.Pool) error {
	r.pools[pool.ID] = pool.Clone()
	return nil
}

func (r *memoryPoolRepo) Get(_ context.Context, id marketv4.PoolID) (*marketv4.Pool, error) {
	pool, ok := r.pools[id]
	if !ok {
		return nil, nil
	}
	return pool.Clone(), nil
}

func (r *memoryPoolRepo) Delete(_ context.Context, id marketv4.PoolID) error {
	delete(r.pools, id)
	return nil
}

func (r *memoryPoolRepo) AdvanceSyncProgress(ctx context.Context, id marketv4.PoolID, blockNumber uint64) error {
	return r.AdvanceSyncProgressMany(ctx, []marketv4.PoolID{id}, blockNumber)
}

func (r *memoryPoolRepo) AdvanceSyncProgressMany(_ context.Context, ids []marketv4.PoolID, blockNumber uint64) error {
	for _, id := range ids {
		pool, ok := r.pools[id]
		if !ok || pool == nil {
			return fmt.Errorf("pool %s not found", id.String())
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

type staticRegistry struct {
	entries map[marketv4.PoolID]marketv4.PoolKey
}

func (r staticRegistry) List(_ context.Context) ([]marketv4.PoolID, error) {
	ids := make([]marketv4.PoolID, 0, len(r.entries))
	for id := range r.entries {
		ids = append(ids, id)
	}
	return ids, nil
}

func (r staticRegistry) GetKey(_ context.Context, id marketv4.PoolID) (marketv4.PoolKey, error) {
	key, ok := r.entries[id]
	if !ok {
		return marketv4.PoolKey{}, fmt.Errorf("pool %s not found", id.String())
	}
	return key, nil
}

func (r staticRegistry) Add(_ context.Context, id marketv4.PoolID, key marketv4.PoolKey) error {
	r.entries[id] = key
	return nil
}

func (r staticRegistry) Remove(_ context.Context, id marketv4.PoolID) error {
	delete(r.entries, id)
	return nil
}

type alwaysReady struct{}

func (alwaysReady) IsSystemReady() bool                  { return true }
func (alwaysReady) IsPoolReady(_ marketv4.PoolID) bool   { return true }

func testToken(index byte) common.Address {
	return common.HexToAddress(fmt.Sprintf("0x000000000000000000000000000000000000000%x", index))
}

func sqrtPriceAtTick0() *big.Int {
	v, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	return v
}

func setupQuotedPool(token0, token1 common.Address) (*marketv4.Pool, marketv4.PoolID) {
	key := marketv4.PoolKey{
		Currency0:   token0,
		Currency1:   token1,
		Fee:         3000,
		TickSpacing: 60,
	}
	id, err := marketv4.ComputePoolID(key)
	if err != nil {
		panic(err)
	}

	pool := marketv4.NewPool(id, key)
	meta := marketv4.EventMeta{PoolID: id, BlockNumber: 1}
	_ = pool.Apply(marketv4.NewInitializeEvent(meta, sqrtPriceAtTick0(), 0))
	_ = pool.Apply(marketv4.NewModifyLiquidityEvent(
		meta,
		common.Address{},
		-120, 120,
		big.NewInt(10_000_000_000_000),
		common.Hash{},
	))
	pool.Status = market.PoolStatusReady
	return pool, id
}

func TestAppServiceSinglePoolExactInput(t *testing.T) {
	token0 := testToken(2)
	token1 := testToken(3)
	pool, poolID := setupQuotedPool(token0, token1)

	repo := newMemoryPoolRepo()
	if err := repo.Save(context.Background(), pool); err != nil {
		t.Fatalf("save pool: %v", err)
	}

	service := quoteuniv4.NewAppService(
		repo,
		staticRegistry{entries: map[marketv4.PoolID]marketv4.PoolKey{poolID: pool.Key}},
		quoteuniv4domain.NewQuoteService(),
		alwaysReady{},
		3,
	)

	resp, err := service.Quote(context.Background(), quoteuniv4.Request{
		TokenIn:  token0,
		TokenOut: token1,
		Mode:     quoteapp.QuoteModeExactInput,
		AmountIn: big.NewInt(1_000_000),
		PoolID:   &poolID,
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if resp.AmountOut.Sign() <= 0 {
		t.Fatalf("expected positive amountOut, got %s", resp.AmountOut)
	}
	if resp.BestRoute.Len() != 1 {
		t.Fatalf("expected single-hop route, got %d hops", resp.BestRoute.Len())
	}
}

func TestAppServiceFindsBestMultiHopRoute(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	tokenC := testToken(4)

	repo := newMemoryPoolRepo()
	registryEntries := make(map[marketv4.PoolID]marketv4.PoolKey)
	var poolABID, poolBCID marketv4.PoolID

	for _, item := range []struct {
		token0, token1 common.Address
		liquidity      int64
		targetID       *marketv4.PoolID
	}{
		{tokenA, tokenB, 10_000_000_000_000, &poolABID},
		{tokenB, tokenC, 1_000_000_000_000, &poolBCID},
	} {
		pool, id := setupQuotedPool(item.token0, item.token1)
		pool.State.Liquidity = big.NewInt(item.liquidity)
		if err := repo.Save(context.Background(), pool); err != nil {
			t.Fatalf("save pool: %v", err)
		}
		registryEntries[id] = pool.Key
		*item.targetID = id
	}

	service := quoteuniv4.NewAppService(
		repo,
		staticRegistry{entries: registryEntries},
		quoteuniv4domain.NewQuoteService(),
		alwaysReady{},
		3,
	)

	resp, err := service.Quote(context.Background(), quoteuniv4.Request{
		TokenIn:  tokenA,
		TokenOut: tokenC,
		Mode:     quoteapp.QuoteModeExactInput,
		AmountIn: big.NewInt(1_000_000),
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if resp.BestRoute.Len() != 2 {
		t.Fatalf("expected 2-hop best route, got %d", resp.BestRoute.Len())
	}
	if len(resp.RouteQuotes) != 1 {
		t.Fatalf("expected 1 route quote, got %d", len(resp.RouteQuotes))
	}
}

func TestAppServiceRejectsWhenSystemNotReady(t *testing.T) {
	service := quoteuniv4.NewAppService(
		newMemoryPoolRepo(),
		staticRegistry{entries: map[marketv4.PoolID]marketv4.PoolKey{}},
		quoteuniv4domain.NewQuoteService(),
		notReadyChecker{},
		3,
	)

	_, err := service.Quote(context.Background(), quoteuniv4.Request{
		TokenIn:  testToken(2),
		TokenOut: testToken(3),
		Mode:     quoteapp.QuoteModeExactInput,
		AmountIn: big.NewInt(1),
	})
	if err == nil {
		t.Fatal("expected readiness error")
	}
}

type notReadyChecker struct{}

func (notReadyChecker) IsSystemReady() bool                { return false }
func (notReadyChecker) IsPoolReady(_ marketv4.PoolID) bool { return false }
