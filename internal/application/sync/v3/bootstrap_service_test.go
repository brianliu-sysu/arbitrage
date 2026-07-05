package syncv3

import (
	"context"
	"math/big"
	"testing"

	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v3"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

type countingBootstrapReader struct {
	calls int
}

func (r *countingBootstrapReader) ReadBootstrapData(_ context.Context, _ common.Address, _ uint64) (*BootstrapData, error) {
	r.calls++
	sqrtPrice, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	return &BootstrapData{
		TickSpacing: 60,
		Fee:         3000,
		State: market.PoolState{
			SqrtPriceX96: sqrtPrice,
			Tick:         0,
			Liquidity:    big.NewInt(123),
		},
		Ticks:  market.NewTickTable(),
		Bitmap: market.NewTickBitmap(),
	}, nil
}

func initializedPool(address common.Address, lastBlock uint64) *marketv3.Pool {
	pool := marketv3.NewPool(address, common.Address{}, common.Address{}, 3000, 60)
	sqrtPrice, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	pool.State.SqrtPriceX96 = sqrtPrice
	pool.State.Tick = 0
	pool.State.Liquidity = big.NewInt(1)
	pool.LastBlockNumber = lastBlock
	return pool
}

func TestBootstrapRefreshesStalePoolFromChain(t *testing.T) {
	ctx := context.Background()
	address := common.HexToAddress("0x0000000000000000000000000000000000000001")
	reader := &countingBootstrapReader{}

	poolRepo := &bootstrapPoolRepo{
		pool: initializedPool(address, 9000),
	}
	service := NewBootstrapService(poolRepo, reader, nil, 1000)

	pool, err := service.Bootstrap(ctx, address, 10_001)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if reader.calls != 1 {
		t.Fatalf("expected one chain bootstrap read, got %d", reader.calls)
	}
	if pool.LastBlockNumber != 10_001 {
		t.Fatalf("expected last block 10001, got %d", pool.LastBlockNumber)
	}
	if pool.State.Liquidity.Cmp(big.NewInt(123)) != 0 {
		t.Fatalf("expected refreshed liquidity 123, got %s", pool.State.Liquidity)
	}
	if pool.Status != market.PoolStatusCatchingUp {
		t.Fatalf("expected catching_up status, got %s", pool.Status)
	}
}

func TestBootstrapSkipsChainRefreshWithinThreshold(t *testing.T) {
	ctx := context.Background()
	address := common.HexToAddress("0x0000000000000000000000000000000000000001")
	reader := &countingBootstrapReader{}

	poolRepo := &bootstrapPoolRepo{
		pool: initializedPool(address, 9500),
	}
	service := NewBootstrapService(poolRepo, reader, nil, 1000)

	pool, err := service.Bootstrap(ctx, address, 10_000)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if reader.calls != 0 {
		t.Fatalf("expected no chain bootstrap read, got %d", reader.calls)
	}
	if pool.LastBlockNumber != 9500 {
		t.Fatalf("expected last block to remain 9500, got %d", pool.LastBlockNumber)
	}
}

type bootstrapPoolRepo struct {
	pool *marketv3.Pool
}

func (r *bootstrapPoolRepo) Save(_ context.Context, pool *marketv3.Pool) error {
	r.pool = pool.Clone()
	return nil
}

func (r *bootstrapPoolRepo) Get(_ context.Context, _ common.Address) (*marketv3.Pool, error) {
	if r.pool == nil {
		return nil, nil
	}
	return r.pool.Clone(), nil
}

func (r *bootstrapPoolRepo) Delete(_ context.Context, _ common.Address) error {
	r.pool = nil
	return nil
}

func (r *bootstrapPoolRepo) AdvanceSyncProgress(_ context.Context, _ common.Address, blockNumber uint64) error {
	if r.pool != nil {
		r.pool.LastBlockNumber = blockNumber
	}
	return nil
}

func (r *bootstrapPoolRepo) AdvanceSyncProgressMany(_ context.Context, _ []common.Address, blockNumber uint64) error {
	return r.AdvanceSyncProgress(context.Background(), common.Address{}, blockNumber)
}
