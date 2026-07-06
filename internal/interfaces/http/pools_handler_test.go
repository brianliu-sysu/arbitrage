package httpapi_test

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	poolsapp "github.com/brianliu-sysu/uniswapv3/internal/application/pools"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	httpapi "github.com/brianliu-sysu/uniswapv3/internal/interfaces/http"
	"github.com/ethereum/go-ethereum/common"
)

type poolsMemoryRepo struct {
	pool *marketuniv3.Pool
}

func (r *poolsMemoryRepo) Save(context.Context, *marketuniv3.Pool) error { return nil }
func (r *poolsMemoryRepo) Delete(context.Context, common.Address) error  { return nil }
func (r *poolsMemoryRepo) AdvanceSyncProgress(context.Context, common.Address, uint64) error {
	return nil
}
func (r *poolsMemoryRepo) AdvanceSyncProgressMany(context.Context, []common.Address, uint64) error {
	return nil
}
func (r *poolsMemoryRepo) Get(context.Context, common.Address) (*marketuniv3.Pool, error) {
	if r.pool == nil {
		return nil, nil
	}
	return r.pool.Clone(), nil
}

type poolsRegistry struct {
	addresses []common.Address
}

func (r *poolsRegistry) List(context.Context) ([]common.Address, error) {
	return append([]common.Address(nil), r.addresses...), nil
}
func (r *poolsRegistry) Add(context.Context, common.Address) error   { return nil }
func (r *poolsRegistry) Remove(context.Context, common.Address) error { return nil }

func TestPoolsHandlerList(t *testing.T) {
	token0 := common.HexToAddress("0x0000000000000000000000000000000000000001")
	token1 := common.HexToAddress("0x0000000000000000000000000000000000000002")
	poolAddr := common.HexToAddress("0x000000000000000000000000000000000000000a")

	pool := marketuniv3.NewPool(poolAddr, token0, token1, 500, 10)
	pool.Status = market.PoolStatusReady

	service := poolsapp.NewAppService(
		&poolsMemoryRepo{pool: pool},
		nil,
		nil,
		&poolsRegistry{addresses: []common.Address{poolAddr}},
		nil,
		nil,
		nil,
		nil,
	)

	router := httpapi.NewRouter(httpapi.Handlers{
		Pools: httpapi.NewPoolsHandler(service),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp poolsapp.ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("expected 1 pool, got %#v", resp)
	}
	if resp.Items[0].PoolType != poolsapp.PoolTypeUniv3 {
		t.Fatalf("unexpected pool type: %s", resp.Items[0].PoolType)
	}
	if resp.Items[0].Token0.Address != token0.Hex() {
		t.Fatalf("unexpected token0: %s", resp.Items[0].Token0.Address)
	}
}

func TestPoolsHandlerDiagnosticsAll(t *testing.T) {
	poolID := marketuniv4.PoolID(common.HexToHash("0x2"))
	pool := marketuniv4.NewPool(poolID, marketuniv4.PoolKey{
		Currency0: common.HexToAddress("0x3"),
		Currency1: common.HexToAddress("0x4"),
		Fee:       3000,
	})
	pool.State.SqrtPriceX96 = big.NewInt(1)
	pool.State.Tick = 10
	pool.State.Liquidity = big.NewInt(1000)
	pool.LastBlockNumber = 200
	pool.Status = market.PoolStatusReady

	chainSqrt, _ := new(big.Int).SetString("1182815765319608250048300092661", 10)
	service := poolsapp.NewAppService(nil, nil, &diagHTTPV4PoolRepo{pool: pool}, nil, nil, &diagHTTPV4Registry{
		poolIDs: []marketuniv4.PoolID{poolID},
	}, nil, &poolsapp.ChainReaders{
		Head: diagHTTPHead{head: 200},
		V4: diagHTTPV4Chain{
			state: &poolsapp.BaseState{
				SqrtPriceX96: chainSqrt,
				Tick:         100,
				Liquidity:    big.NewInt(1000),
			},
		},
	})

	router := httpapi.NewRouter(httpapi.Handlers{
		Pools: httpapi.NewPoolsHandler(service),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools/diagnostics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp poolsapp.DiagnosticsListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Count != 1 || len(resp.Items) != 1 {
		t.Fatalf("expected 1 mismatching pool, got %#v", resp)
	}
}

type diagHTTPHead struct{ head uint64 }

func (h diagHTTPHead) LatestBlockNumber(context.Context) (uint64, error) { return h.head, nil }

type diagHTTPV4Chain struct{ state *poolsapp.BaseState }

func (r diagHTTPV4Chain) ReadV4BaseState(context.Context, marketuniv4.PoolID, uint64) (*poolsapp.BaseState, error) {
	return r.state, nil
}

type diagHTTPV4Registry struct {
	poolIDs []marketuniv4.PoolID
}

func (r *diagHTTPV4Registry) List(context.Context) ([]marketuniv4.PoolID, error) {
	return append([]marketuniv4.PoolID(nil), r.poolIDs...), nil
}
func (r *diagHTTPV4Registry) GetKey(context.Context, marketuniv4.PoolID) (marketuniv4.PoolKey, error) {
	return marketuniv4.PoolKey{}, nil
}
func (r *diagHTTPV4Registry) Add(context.Context, marketuniv4.PoolID, marketuniv4.PoolKey) error { return nil }
func (r *diagHTTPV4Registry) Remove(context.Context, marketuniv4.PoolID) error                  { return nil }

type diagHTTPV4PoolRepo struct{ pool *marketuniv4.Pool }

func (r *diagHTTPV4PoolRepo) Save(context.Context, *marketuniv4.Pool) error { return nil }
func (r *diagHTTPV4PoolRepo) Delete(context.Context, marketuniv4.PoolID) error { return nil }
func (r *diagHTTPV4PoolRepo) AdvanceSyncProgress(context.Context, marketuniv4.PoolID, uint64) error {
	return nil
}
func (r *diagHTTPV4PoolRepo) AdvanceSyncProgressMany(context.Context, []marketuniv4.PoolID, uint64) error {
	return nil
}
func (r *diagHTTPV4PoolRepo) Get(context.Context, marketuniv4.PoolID) (*marketuniv4.Pool, error) {
	if r.pool == nil {
		return nil, nil
	}
	return r.pool.Clone(), nil
}
