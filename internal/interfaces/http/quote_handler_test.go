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
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	domainquote "github.com/brianliu-sysu/uniswapv3/internal/domain/quote"
	httpapi "github.com/brianliu-sysu/uniswapv3/internal/interfaces/http"
	"github.com/ethereum/go-ethereum/common"
)

type memoryPoolRepo struct {
	pools map[common.Address]*market.Pool
}

func newMemoryPoolRepo() *memoryPoolRepo {
	return &memoryPoolRepo{pools: make(map[common.Address]*market.Pool)}
}

func (r *memoryPoolRepo) Save(_ context.Context, pool *market.Pool) error {
	r.pools[pool.Address] = pool.Clone()
	return nil
}

func (r *memoryPoolRepo) Get(_ context.Context, address common.Address) (*market.Pool, error) {
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

func (r staticRegistry) Add(_ context.Context, _ common.Address) error      { return nil }
func (r staticRegistry) Remove(_ context.Context, _ common.Address) error { return nil }

type alwaysReady struct{}

func (alwaysReady) IsSystemReady() bool              { return true }
func (alwaysReady) IsPoolReady(_ common.Address) bool { return true }

func testToken(index byte) common.Address {
	return common.HexToAddress(fmt.Sprintf("0x000000000000000000000000000000000000000%x", index))
}

func sqrtPriceAtTick0() *big.Int {
	v, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	return v
}

func setupQuotedPool(address, token0, token1 common.Address) *market.Pool {
	pool := market.NewPool(address, token0, token1, 3000, 60)
	meta := market.EventMeta{PoolAddress: address, BlockNumber: 1}
	_ = pool.Apply(market.NewInitializeEvent(meta, sqrtPriceAtTick0(), 0))
	_ = pool.Apply(market.NewMintEvent(meta, common.Address{}, common.Address{}, -120, 120, big.NewInt(10_000_000_000_000), big.NewInt(1), big.NewInt(1)))
	pool.Status = market.PoolStatusReady
	return pool
}

func newQuoteRouter(app *quoteapp.QuoteAppService) http.Handler {
	return httpapi.NewRouter(httpapi.Handlers{
		Quote: httpapi.NewQuoteHandler(app),
	})
}

func TestQuoteHandlerReturnsQuoteJSON(t *testing.T) {
	token0 := testToken(2)
	token1 := testToken(3)
	poolAddr := testToken(1)
	pool := setupQuotedPool(poolAddr, token0, token1)

	repo := newMemoryPoolRepo()
	if err := repo.Save(context.Background(), pool); err != nil {
		t.Fatalf("save pool: %v", err)
	}

	app := quoteapp.NewQuoteAppService(
		repo,
		staticRegistry{addresses: []common.Address{poolAddr}},
		domainquote.NewQuoteService(),
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

	req := httptest.NewRequest(http.MethodPost, "/quote", bytes.NewReader(body))
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

func TestQuoteHandlerRejectsInvalidJSON(t *testing.T) {
	router := newQuoteRouter(quoteapp.NewQuoteAppService(nil, nil, domainquote.NewQuoteService(), nil, 3))

	req := httptest.NewRequest(http.MethodPost, "/quote", bytes.NewReader([]byte("{")))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestQuoteHandlerRejectsNonPost(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Handlers{
		Quote: httpapi.NewQuoteHandler(nil),
	})

	req := httptest.NewRequest(http.MethodGet, "/quote", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}
