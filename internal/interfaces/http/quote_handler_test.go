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

	quoteapp "github.com/brianliu-sysu/uniswapv3/internal/application/quote"
	quoteuniv3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/univ3"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v3"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	quoteuniv3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ3"
	httpapi "github.com/brianliu-sysu/uniswapv3/internal/interfaces/http"
	"github.com/ethereum/go-ethereum/common"
)

type memoryPoolRepo struct {
	pools map[common.Address]*marketv3.Pool
}

func newMemoryPoolRepo() *memoryPoolRepo {
	return &memoryPoolRepo{pools: make(map[common.Address]*marketv3.Pool)}
}

func (r *memoryPoolRepo) Save(_ context.Context, pool *marketv3.Pool) error {
	r.pools[pool.Address] = pool.Clone()
	return nil
}

func (r *memoryPoolRepo) Get(_ context.Context, address common.Address) (*marketv3.Pool, error) {
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

func setupQuotedPool(address, token0, token1 common.Address) *marketv3.Pool {
	pool := marketv3.NewPool(address, token0, token1, 3000, 60)
	meta := marketv3.EventMeta{PoolAddress: address, BlockNumber: 1}
	_ = pool.Apply(marketv3.NewInitializeEvent(meta, sqrtPriceAtTick0(), 0))
	_ = pool.Apply(marketv3.NewMintEvent(meta, common.Address{}, common.Address{}, -120, 120, big.NewInt(10_000_000_000_000), big.NewInt(1), big.NewInt(1)))
	pool.Status = market.PoolStatusReady
	return pool
}

func newQuoteRouter(v3 *quoteuniv3.AppService) http.Handler {
	return httpapi.NewRouter(httpapi.Handlers{
		Health:  httpapi.NewHealthHandler(),
		QuoteV3: httpapi.NewQuoteV3Handler(v3),
	})
}

func TestQuoteV3HandlerReturnsQuoteJSON(t *testing.T) {
	token0 := testToken(2)
	token1 := testToken(3)
	poolAddr := testToken(1)
	pool := setupQuotedPool(poolAddr, token0, token1)

	repo := newMemoryPoolRepo()
	if err := repo.Save(context.Background(), pool); err != nil {
		t.Fatalf("save pool: %v", err)
	}

	app := quoteapp.NewQuoteV3AppService(
		repo,
		staticRegistry{addresses: []common.Address{poolAddr}},
		quoteuniv3domain.NewQuoteService(),
		alwaysReady{},
		3,
	)
	router := newQuoteRouter(app)

	body, err := json.Marshal(map[string]string{
		"tokenIn":     token0.Hex(),
		"tokenOut":    token1.Hex(),
		"amountIn":    "1000000",
		"poolAddress": poolAddr.Hex(),
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/univ3/quote", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["amountOut"] == "" || resp["amountOut"] == "0" {
		t.Fatalf("expected positive amountOut, got %#v", resp["amountOut"])
	}
	if resp["bestRoute"] == nil {
		t.Fatal("expected bestRoute in response")
	}
}

func TestQuoteV3HandlerRejectsInvalidJSON(t *testing.T) {
	router := newQuoteRouter(quoteapp.NewQuoteV3AppService(
		nil, nil, quoteuniv3domain.NewQuoteService(), nil, 3,
	))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/univ3/quote", bytes.NewReader([]byte("{")))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestQuoteV3HandlerRejectsNonPost(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Handlers{
		QuoteV3: httpapi.NewQuoteV3Handler(nil),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/univ3/quote", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}

func TestQuoteV4HandlerReturnsNotConfiguredWhenNil(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Handlers{
		QuoteV4: httpapi.NewQuoteV4Handler(nil),
	})

	body := bytes.NewReader([]byte(`{"tokenIn":"0x0000000000000000000000000000000000000002","tokenOut":"0x0000000000000000000000000000000000000003","amountIn":"1"}`))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/univ4/quote", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d body=%s", rec.Code, rec.Body.String())
	}
}
