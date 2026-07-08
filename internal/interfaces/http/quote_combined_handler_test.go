package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	quotecombined "github.com/brianliu-sysu/uniswapv3/internal/application/quote/combined"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	quoteuniv3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ3"
	quoteuniv4domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ4"
	httpapi "github.com/brianliu-sysu/uniswapv3/internal/interfaces/http"
	"github.com/ethereum/go-ethereum/common"
)

func TestQuoteCombinedHandlerReturnsMixedRouteJSON(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	tokenC := testToken(4)
	poolAB := testToken(10)

	v3Repo := newMemoryPoolRepo()
	v4Repo := newMemoryV4PoolRepo()

	poolABPool := setupQuotedPool(poolAB, tokenA, tokenB)
	if err := v3Repo.Save(context.Background(), poolABPool); err != nil {
		t.Fatalf("save v3 pool: %v", err)
	}

	poolBC, poolBCID := setupV4Pool(tokenB, tokenC, 1_000_000_000_000)
	if err := v4Repo.Save(context.Background(), poolBC); err != nil {
		t.Fatalf("save v4 pool: %v", err)
	}

	combined := quotecombined.NewAppService(
		v3Repo,
		nil,
		nil,
		v4Repo,
		nil,
		staticRegistry{addresses: []common.Address{poolAB}},
		nil,
		nil,
		staticV4Registry{entries: map[marketv4.PoolID]marketv4.PoolKey{poolBCID: poolBC.Key}},
		nil,
		quoteunified.NewQuoteService(
			quoteuniv3domain.NewQuoteService(),
			nil,
			quoteuniv4domain.NewQuoteService(),
		),
		combinedAlwaysReady{},
		3,
	)

	router := httpapi.NewRouter(httpapi.Handlers{
		QuoteCombined: httpapi.NewQuoteCombinedHandler(combined),
	})

	body, err := json.Marshal(map[string]string{
		"tokenIn":  tokenA.Hex(),
		"tokenOut": tokenC.Hex(),
		"amountIn": "1000000",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quote", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	bestRoute, ok := resp["bestRoute"].(map[string]any)
	if !ok {
		t.Fatal("expected bestRoute object")
	}
	hops, ok := bestRoute["hops"].([]any)
	if !ok || len(hops) != 2 {
		t.Fatalf("expected 2-hop mixed route, got %#v", bestRoute["hops"])
	}
}

type combinedAlwaysReady struct{}

func (combinedAlwaysReady) IsSystemReady() bool                          { return true }
func (combinedAlwaysReady) IsV3PoolReady(_ common.Address) bool          { return true }
func (combinedAlwaysReady) IsPancakeV3PoolReady(_ common.Address) bool   { return true }
func (combinedAlwaysReady) IsQuickSwapV3PoolReady(_ common.Address) bool { return true }
func (combinedAlwaysReady) IsV4PoolReady(_ marketv4.PoolID) bool         { return true }
func (combinedAlwaysReady) IsBalancerPoolReady(_ marketbalancer.PoolID) bool {
	return true
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
