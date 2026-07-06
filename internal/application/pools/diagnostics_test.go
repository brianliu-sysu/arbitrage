package poolsapp

import (
	"context"
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
