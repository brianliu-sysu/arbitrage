package combined_test

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	quoteapp "github.com/brianliu-sysu/uniswapv3/internal/application/quote"
	quotecombined "github.com/brianliu-sysu/uniswapv3/internal/application/quote/combined"
	quoteuniv3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ3"
	quoteuniv4domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ4"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

type memoryV3PoolRepo struct {
	pools map[common.Address]*marketv3.Pool
}

func newMemoryV3PoolRepo() *memoryV3PoolRepo {
	return &memoryV3PoolRepo{pools: make(map[common.Address]*marketv3.Pool)}
}

func (r *memoryV3PoolRepo) Save(_ context.Context, pool *marketv3.Pool) error {
	r.pools[pool.Address] = pool.Clone()
	return nil
}

func (r *memoryV3PoolRepo) Get(_ context.Context, address common.Address) (*marketv3.Pool, error) {
	pool, ok := r.pools[address]
	if !ok {
		return nil, nil
	}
	return pool.Clone(), nil
}

func (r *memoryV3PoolRepo) Delete(_ context.Context, address common.Address) error {
	delete(r.pools, address)
	return nil
}

func (r *memoryV3PoolRepo) AdvanceSyncProgress(ctx context.Context, address common.Address, blockNumber uint64) error {
	return r.AdvanceSyncProgressMany(ctx, []common.Address{address}, blockNumber)
}

func (r *memoryV3PoolRepo) AdvanceSyncProgressMany(_ context.Context, addresses []common.Address, blockNumber uint64) error {
	for _, address := range addresses {
		pool, ok := r.pools[address]
		if !ok || pool == nil {
			return fmt.Errorf("pool %s not found", address.Hex())
		}
		if blockNumber > pool.LastBlockNumber {
			pool.LastBlockNumber = blockNumber
		}
	}
	return nil
}

type memoryV4PoolRepo struct {
	pools map[marketv4.PoolID]*marketv4.Pool
}

func newMemoryV4PoolRepo() *memoryV4PoolRepo {
	return &memoryV4PoolRepo{pools: make(map[marketv4.PoolID]*marketv4.Pool)}
}

func (r *memoryV4PoolRepo) Save(_ context.Context, pool *marketv4.Pool) error {
	r.pools[pool.ID] = pool.Clone()
	return nil
}

func (r *memoryV4PoolRepo) Get(_ context.Context, id marketv4.PoolID) (*marketv4.Pool, error) {
	pool, ok := r.pools[id]
	if !ok {
		return nil, nil
	}
	return pool.Clone(), nil
}

func (r *memoryV4PoolRepo) Delete(_ context.Context, id marketv4.PoolID) error {
	delete(r.pools, id)
	return nil
}

func (r *memoryV4PoolRepo) AdvanceSyncProgress(ctx context.Context, id marketv4.PoolID, blockNumber uint64) error {
	return r.AdvanceSyncProgressMany(ctx, []marketv4.PoolID{id}, blockNumber)
}

func (r *memoryV4PoolRepo) AdvanceSyncProgressMany(_ context.Context, ids []marketv4.PoolID, blockNumber uint64) error {
	for _, id := range ids {
		pool, ok := r.pools[id]
		if !ok || pool == nil {
			return fmt.Errorf("pool %s not found", id.String())
		}
		if blockNumber > pool.LastBlockNumber {
			pool.LastBlockNumber = blockNumber
		}
	}
	return nil
}

type staticV3Registry struct {
	addresses []common.Address
}

func (r staticV3Registry) List(_ context.Context) ([]common.Address, error) {
	return append([]common.Address(nil), r.addresses...), nil
}

func (r staticV3Registry) Add(_ context.Context, _ common.Address) error    { return nil }
func (r staticV3Registry) Remove(_ context.Context, _ common.Address) error { return nil }

type staticV4Registry struct {
	entries map[marketv4.PoolID]marketv4.PoolKey
}

func (r staticV4Registry) List(_ context.Context) ([]marketv4.PoolID, error) {
	ids := make([]marketv4.PoolID, 0, len(r.entries))
	for id := range r.entries {
		ids = append(ids, id)
	}
	return ids, nil
}

func (r staticV4Registry) GetKey(_ context.Context, id marketv4.PoolID) (marketv4.PoolKey, error) {
	key, ok := r.entries[id]
	if !ok {
		return marketv4.PoolKey{}, fmt.Errorf("pool %s not found", id.String())
	}
	return key, nil
}

func (r staticV4Registry) Add(_ context.Context, id marketv4.PoolID, key marketv4.PoolKey) error {
	r.entries[id] = key
	return nil
}

func (r staticV4Registry) Remove(_ context.Context, id marketv4.PoolID) error {
	delete(r.entries, id)
	return nil
}

type alwaysReady struct{}

func (alwaysReady) IsSystemReady() bool                         { return true }
func (alwaysReady) IsV3PoolReady(_ common.Address) bool         { return true }
func (alwaysReady) IsV4PoolReady(_ marketv4.PoolID) bool        { return true }

func testToken(index byte) common.Address {
	return common.HexToAddress(fmt.Sprintf("0x000000000000000000000000000000000000000%x", index))
}

func sqrtPriceAtTick0() *big.Int {
	v, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	return v
}

func setupV3Pool(address, token0, token1 common.Address, liquidity int64) *marketv3.Pool {
	pool := marketv3.NewPool(address, token0, token1, 3000, 60)
	meta := marketv3.EventMeta{PoolAddress: address, BlockNumber: 1}
	_ = pool.Apply(marketv3.NewInitializeEvent(meta, sqrtPriceAtTick0(), 0))
	_ = pool.Apply(marketv3.NewMintEvent(meta, common.Address{}, common.Address{}, -120, 120, big.NewInt(liquidity), big.NewInt(1), big.NewInt(1)))
	pool.Status = market.PoolStatusReady
	return pool
}

func setupV4Pool(token0, token1 common.Address, liquidity int64) (*marketv4.Pool, marketv4.PoolID) {
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
	_ = pool.Apply(marketv4.NewModifyLiquidityEvent(meta, common.Address{}, -120, 120, big.NewInt(liquidity), common.Hash{}))
	pool.Status = market.PoolStatusReady
	return pool, id
}

func newCombinedService(v3Repo *memoryV3PoolRepo, v4Repo *memoryV4PoolRepo, v3Reg staticV3Registry, v4Reg staticV4Registry) *quotecombined.AppService {
	return quotecombined.NewAppService(
		v3Repo,
		v4Repo,
		v3Reg,
		v4Reg,
		quoteunified.NewQuoteService(
			quoteuniv3domain.NewQuoteService(),
			quoteuniv4domain.NewQuoteService(),
		),
		alwaysReady{},
		3,
	)
}

func TestAppServiceFindsMixedV3V4Route(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	tokenC := testToken(4)
	poolAB := testToken(10)

	v3Repo := newMemoryV3PoolRepo()
	v4Repo := newMemoryV4PoolRepo()

	poolABPool := setupV3Pool(poolAB, tokenA, tokenB, 10_000_000_000_000)
	if err := v3Repo.Save(context.Background(), poolABPool); err != nil {
		t.Fatalf("save v3 pool: %v", err)
	}

	poolBC, poolBCID := setupV4Pool(tokenB, tokenC, 1_000_000_000_000)
	if err := v4Repo.Save(context.Background(), poolBC); err != nil {
		t.Fatalf("save v4 pool: %v", err)
	}

	service := newCombinedService(
		v3Repo,
		v4Repo,
		staticV3Registry{addresses: []common.Address{poolAB}},
		staticV4Registry{entries: map[marketv4.PoolID]marketv4.PoolKey{poolBCID: poolBC.Key}},
	)

	resp, err := service.Quote(context.Background(), quotecombined.Request{
		TokenIn:  tokenA,
		TokenOut: tokenC,
		Mode:     quoteapp.QuoteModeExactInput,
		AmountIn: big.NewInt(1_000_000),
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if resp.BestRoute.Len() != 2 {
		t.Fatalf("expected 2-hop mixed route, got %d", resp.BestRoute.Len())
	}
	if resp.BestRoute.Hops[0].Version != quoteunified.PoolVersionV3 {
		t.Fatalf("expected first hop on v3, got %s", resp.BestRoute.Hops[0].Version)
	}
	if resp.BestRoute.Hops[1].Version != quoteunified.PoolVersionV4 {
		t.Fatalf("expected second hop on v4, got %s", resp.BestRoute.Hops[1].Version)
	}
	if resp.AmountOut.Sign() <= 0 {
		t.Fatalf("expected positive amountOut, got %s", resp.AmountOut)
	}
}

func TestAppServicePicksBestAcrossProtocols(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)

	v3Repo := newMemoryV3PoolRepo()
	v4Repo := newMemoryV4PoolRepo()

	poolV3Addr := testToken(10)
	poolV3 := setupV3Pool(poolV3Addr, tokenA, tokenB, 1_000_000_000)
	if err := v3Repo.Save(context.Background(), poolV3); err != nil {
		t.Fatalf("save v3 pool: %v", err)
	}

	poolV4, poolV4ID := setupV4Pool(tokenA, tokenB, 100_000_000_000_000)
	if err := v4Repo.Save(context.Background(), poolV4); err != nil {
		t.Fatalf("save v4 pool: %v", err)
	}

	service := newCombinedService(
		v3Repo,
		v4Repo,
		staticV3Registry{addresses: []common.Address{poolV3Addr}},
		staticV4Registry{entries: map[marketv4.PoolID]marketv4.PoolKey{poolV4ID: poolV4.Key}},
	)

	resp, err := service.Quote(context.Background(), quotecombined.Request{
		TokenIn:  tokenA,
		TokenOut: tokenB,
		Mode:     quoteapp.QuoteModeExactInput,
		AmountIn: big.NewInt(1_000_000),
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if resp.BestRoute.Len() != 1 {
		t.Fatalf("expected single-hop route, got %d", resp.BestRoute.Len())
	}
	if resp.BestRoute.Hops[0].Version != quoteunified.PoolVersionV4 {
		t.Fatalf("expected v4 pool to win with higher liquidity, got %s", resp.BestRoute.Hops[0].Version)
	}
}

func TestAppServiceRejectsBothPoolSelectors(t *testing.T) {
	service := newCombinedService(newMemoryV3PoolRepo(), newMemoryV4PoolRepo(), staticV3Registry{}, staticV4Registry{entries: map[marketv4.PoolID]marketv4.PoolKey{}})

	poolAddr := testToken(1)
	poolID := marketv4.PoolID(common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"))

	_, err := service.Quote(context.Background(), quotecombined.Request{
		TokenIn:     testToken(2),
		TokenOut:    testToken(3),
		Mode:        quoteapp.QuoteModeExactInput,
		AmountIn:    big.NewInt(1),
		PoolAddress: &poolAddr,
		PoolID:      &poolID,
	})
	if err == nil {
		t.Fatal("expected error when both pool selectors are provided")
	}
}
