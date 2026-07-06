package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	poolsapp "github.com/brianliu-sysu/uniswapv3/internal/application/pools"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
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
