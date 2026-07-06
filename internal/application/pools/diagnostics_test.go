package poolsapp

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

type diagHeadReader struct{ head uint64 }

func (r diagHeadReader) LatestBlockNumber(context.Context) (uint64, error) { return r.head, nil }

type diagV4Reader struct {
	state *BaseState
}

func (r diagV4Reader) ReadV4BaseState(context.Context, marketuniv4.PoolID, uint64) (*BaseState, error) {
	return r.state, nil
}

type diagV4PoolRepo struct {
	pool *marketuniv4.Pool
}

func (r *diagV4PoolRepo) Save(context.Context, *marketuniv4.Pool) error { return nil }
func (r *diagV4PoolRepo) Delete(context.Context, marketuniv4.PoolID) error { return nil }
func (r *diagV4PoolRepo) AdvanceSyncProgress(context.Context, marketuniv4.PoolID, uint64) error {
	return nil
}
func (r *diagV4PoolRepo) AdvanceSyncProgressMany(context.Context, []marketuniv4.PoolID, uint64) error {
	return nil
}
func (r *diagV4PoolRepo) Get(context.Context, marketuniv4.PoolID) (*marketuniv4.Pool, error) {
	if r.pool == nil {
		return nil, nil
	}
	return r.pool.Clone(), nil
}

func TestDiagnosticsV4MatchesChainState(t *testing.T) {
	poolID := marketuniv4.PoolID(common.HexToHash("0xb2b5618903d74bbac9e9049a035c3827afc4487cde3b994a1568b050f4c8e2e4"))
	sqrtPrice, _ := new(big.Int).SetString("1182815765319608250048300092661", 10)
	liquidity, _ := new(big.Int).SetString("1926769575501361337071845", 10)

	pool := marketuniv4.NewPool(poolID, marketuniv4.PoolKey{
		Currency0:   common.Address{},
		Currency1:   common.HexToAddress("0x514910771AF9Ca656af840dff83E8264EcF986CA"),
		Fee:         3000,
		TickSpacing: 60,
	})
	pool.State.SqrtPriceX96 = sqrtPrice
	pool.State.Tick = 54069
	pool.State.Liquidity = liquidity
	pool.LastBlockNumber = 100
	pool.Status = market.PoolStatusReady

	chainState := &BaseState{
		SqrtPriceX96: new(big.Int).Set(sqrtPrice),
		Tick:         54069,
		Liquidity:    new(big.Int).Set(liquidity),
	}

	service := NewAppService(nil, nil, &diagV4PoolRepo{pool: pool}, nil, nil, nil, nil, &ChainReaders{
		Head: diagHeadReader{head: 105},
		V4:   diagV4Reader{state: chainState},
	})

	resp, err := service.Diagnostics(context.Background(), DiagnosticsRequest{
		PoolType: PoolTypeUniv4,
		PoolID:   poolID,
	})
	if err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	if !resp.Diff.SqrtPriceX96Match || !resp.Diff.TickMatch || !resp.Diff.LiquidityMatch {
		t.Fatalf("expected matching state, got %#v", resp.Diff)
	}
	if resp.Local.BlockLag != 5 {
		t.Fatalf("expected block lag 5, got %d", resp.Local.BlockLag)
	}
	if resp.Local.Price.Token1PerToken0 == "" {
		t.Fatal("expected implied price on local snapshot")
	}
}

type diagV4Registry struct {
	poolIDs []marketuniv4.PoolID
}

func (r *diagV4Registry) List(context.Context) ([]marketuniv4.PoolID, error) {
	return append([]marketuniv4.PoolID(nil), r.poolIDs...), nil
}
func (r *diagV4Registry) GetKey(context.Context, marketuniv4.PoolID) (marketuniv4.PoolKey, error) {
	return marketuniv4.PoolKey{}, nil
}
func (r *diagV4Registry) Add(context.Context, marketuniv4.PoolID, marketuniv4.PoolKey) error   { return nil }
func (r *diagV4Registry) Remove(context.Context, marketuniv4.PoolID) error { return nil }

type diagV4ReaderByPool map[marketuniv4.PoolID]*BaseState

func (r diagV4ReaderByPool) ReadV4BaseState(_ context.Context, poolID marketuniv4.PoolID, _ uint64) (*BaseState, error) {
	state, ok := r[poolID]
	if !ok {
		return nil, fmt.Errorf("pool not found")
	}
	return state, nil
}

func TestDiagnosticsAllReturnsMismatchingPoolsAtHead(t *testing.T) {
	matchedID := marketuniv4.PoolID(common.HexToHash("0x1"))
	mismatchID := marketuniv4.PoolID(common.HexToHash("0x2"))
	sqrtPrice, _ := new(big.Int).SetString("1182815765319608250048300092661", 10)
	liquidity := big.NewInt(1000)

	matchedPool := marketuniv4.NewPool(matchedID, marketuniv4.PoolKey{
		Currency0: common.HexToAddress("0x1"),
		Currency1: common.HexToAddress("0x2"),
		Fee:       3000,
	})
	matchedPool.State.SqrtPriceX96 = sqrtPrice
	matchedPool.State.Tick = 100
	matchedPool.State.Liquidity = liquidity
	matchedPool.LastBlockNumber = 200
	matchedPool.Status = market.PoolStatusReady

	mismatchPool := marketuniv4.NewPool(mismatchID, marketuniv4.PoolKey{
		Currency0: common.HexToAddress("0x3"),
		Currency1: common.HexToAddress("0x4"),
		Fee:       3000,
	})
	mismatchPool.State.SqrtPriceX96 = big.NewInt(1)
	mismatchPool.State.Tick = 10
	mismatchPool.State.Liquidity = liquidity
	mismatchPool.LastBlockNumber = 200
	mismatchPool.Status = market.PoolStatusReady

	repo := &diagV4PoolRepoByID{
		pools: map[marketuniv4.PoolID]*marketuniv4.Pool{
			matchedID:   matchedPool,
			mismatchID: mismatchPool,
		},
	}

	service := NewAppService(nil, nil, repo, nil, nil, &diagV4Registry{
		poolIDs: []marketuniv4.PoolID{matchedID, mismatchID},
	}, nil, &ChainReaders{
		Head: diagHeadReader{head: 200},
		V4: diagV4ReaderByPool{
			matchedID: {
				SqrtPriceX96: new(big.Int).Set(sqrtPrice),
				Tick:         100,
				Liquidity:    new(big.Int).Set(liquidity),
			},
			mismatchID: {
				SqrtPriceX96: new(big.Int).Set(sqrtPrice),
				Tick:         100,
				Liquidity:    new(big.Int).Set(liquidity),
			},
		},
	})

	resp, err := service.DiagnosticsAll(context.Background())
	if err != nil {
		t.Fatalf("diagnostics all: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("expected 1 mismatching pool, got %#v", resp)
	}
	if resp.Items[0].PoolID != mismatchID.String() {
		t.Fatalf("unexpected pool: %s", resp.Items[0].PoolID)
	}
}

type diagV4PoolRepoByID struct {
	pools map[marketuniv4.PoolID]*marketuniv4.Pool
}

func (r *diagV4PoolRepoByID) Save(context.Context, *marketuniv4.Pool) error { return nil }
func (r *diagV4PoolRepoByID) Delete(context.Context, marketuniv4.PoolID) error { return nil }
func (r *diagV4PoolRepoByID) AdvanceSyncProgress(context.Context, marketuniv4.PoolID, uint64) error {
	return nil
}
func (r *diagV4PoolRepoByID) AdvanceSyncProgressMany(context.Context, []marketuniv4.PoolID, uint64) error {
	return nil
}
func (r *diagV4PoolRepoByID) Get(_ context.Context, id marketuniv4.PoolID) (*marketuniv4.Pool, error) {
	pool := r.pools[id]
	if pool == nil {
		return nil, nil
	}
	return pool.Clone(), nil
}
