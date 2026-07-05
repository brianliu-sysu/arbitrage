package quotepancakev3_test

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	quoteapp "github.com/brianliu-sysu/uniswapv3/internal/application/quote"
	quotepancakev3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/pancakev3"
	quotepancakev3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/pancakev3"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

type memoryPoolRepo struct {
	pools map[common.Address]*marketpancake.Pool
}

func newMemoryPoolRepo() *memoryPoolRepo {
	return &memoryPoolRepo{pools: make(map[common.Address]*marketpancake.Pool)}
}

func (r *memoryPoolRepo) Save(_ context.Context, pool *marketpancake.Pool) error {
	r.pools[pool.Address] = pool.Clone()
	return nil
}

func (r *memoryPoolRepo) Get(_ context.Context, address common.Address) (*marketpancake.Pool, error) {
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

type staticRegistry struct {
	addresses []common.Address
}

func (r staticRegistry) List(_ context.Context) ([]common.Address, error) {
	return append([]common.Address(nil), r.addresses...), nil
}

func (r staticRegistry) Add(_ context.Context, _ common.Address) error    { return nil }
func (r staticRegistry) Remove(_ context.Context, _ common.Address) error { return nil }

type alwaysReady struct{}

func (alwaysReady) IsSystemReady() bool                   { return true }
func (alwaysReady) IsPoolReady(_ common.Address) bool     { return true }

func testToken(index byte) common.Address {
	return common.HexToAddress(fmt.Sprintf("0x000000000000000000000000000000000000000%x", index))
}

func sqrtPriceAtTick0() *big.Int {
	v, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	return v
}

func setupQuotedPool(address, token0, token1 common.Address) *marketpancake.Pool {
	pool := marketpancake.NewPool(address, token0, token1, 3000, 60)
	meta := marketpancake.EventMeta{PoolAddress: address, BlockNumber: 1}
	_ = pool.Apply(marketpancake.NewInitializeEvent(meta, sqrtPriceAtTick0(), 0))
	_ = pool.Apply(marketpancake.NewMintEvent(meta, common.Address{}, common.Address{}, -120, 120, big.NewInt(10_000_000_000_000), big.NewInt(1), big.NewInt(1)))
	pool.Status = market.PoolStatusReady
	return pool
}

func TestAppServiceSinglePoolExactInput(t *testing.T) {
	token0 := testToken(2)
	token1 := testToken(3)
	poolAddr := testToken(1)
	pool := setupQuotedPool(poolAddr, token0, token1)

	repo := newMemoryPoolRepo()
	if err := repo.Save(context.Background(), pool); err != nil {
		t.Fatalf("save pool: %v", err)
	}

	service := quotepancakev3.NewAppService(
		repo,
		staticRegistry{addresses: []common.Address{poolAddr}},
		quotepancakev3domain.NewQuoteService(),
		alwaysReady{},
		3,
	)

	resp, err := service.Quote(context.Background(), quotepancakev3.Request{
		TokenIn:     token0,
		TokenOut:    token1,
		Mode:        quoteapp.QuoteModeExactInput,
		AmountIn:    big.NewInt(1_000_000),
		PoolAddress: &poolAddr,
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
	poolAB := testToken(10)
	poolBC := testToken(11)

	repo := newMemoryPoolRepo()
	for _, item := range []struct {
		addr           common.Address
		token0, token1 common.Address
		liquidity      int64
	}{
		{poolAB, tokenA, tokenB, 10_000_000_000_000},
		{poolBC, tokenB, tokenC, 1_000_000_000_000},
	} {
		pool := setupQuotedPool(item.addr, item.token0, item.token1)
		pool.State.Liquidity = big.NewInt(item.liquidity)
		if err := repo.Save(context.Background(), pool); err != nil {
			t.Fatalf("save pool: %v", err)
		}
	}

	service := quotepancakev3.NewAppService(
		repo,
		staticRegistry{addresses: []common.Address{poolAB, poolBC}},
		quotepancakev3domain.NewQuoteService(),
		alwaysReady{},
		3,
	)

	resp, err := service.Quote(context.Background(), quotepancakev3.Request{
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
	service := quotepancakev3.NewAppService(
		newMemoryPoolRepo(),
		staticRegistry{},
		quotepancakev3domain.NewQuoteService(),
		notReadyChecker{},
		3,
	)

	_, err := service.Quote(context.Background(), quotepancakev3.Request{
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

func (notReadyChecker) IsSystemReady() bool               { return false }
func (notReadyChecker) IsPoolReady(_ common.Address) bool { return false }
