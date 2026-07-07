package poolsapp

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
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

func (r *diagV4PoolRepo) Save(context.Context, *marketuniv4.Pool) error    { return nil }
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

	service := NewAppService(nil, nil, &diagV4PoolRepo{pool: pool}, nil, nil, nil, nil, nil, nil, &ChainReaders{
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
func (r *diagV4Registry) Add(context.Context, marketuniv4.PoolID, marketuniv4.PoolKey) error {
	return nil
}
func (r *diagV4Registry) Remove(context.Context, marketuniv4.PoolID) error { return nil }

type diagV4ReaderByPool map[marketuniv4.PoolID]*BaseState

func (r diagV4ReaderByPool) ReadV4BaseState(_ context.Context, poolID marketuniv4.PoolID, _ uint64) (*BaseState, error) {
	state, ok := r[poolID]
	if !ok {
		return nil, fmt.Errorf("pool not found")
	}
	return state, nil
}

func (r diagV4ReaderByPool) ReadManyV4BaseStates(_ context.Context, poolIDs []marketuniv4.PoolID, _ uint64) (map[marketuniv4.PoolID]*BaseState, error) {
	out := make(map[marketuniv4.PoolID]*BaseState, len(poolIDs))
	for _, poolID := range poolIDs {
		state, ok := r[poolID]
		if !ok {
			continue
		}
		out[poolID] = state
	}
	return out, nil
}

type diagV4BatchCallCounter struct {
	diagV4ReaderByPool
	calls int
}

func (r *diagV4BatchCallCounter) ReadManyV4BaseStates(ctx context.Context, poolIDs []marketuniv4.PoolID, blockNumber uint64) (map[marketuniv4.PoolID]*BaseState, error) {
	r.calls++
	return r.diagV4ReaderByPool.ReadManyV4BaseStates(ctx, poolIDs, blockNumber)
}

func TestDiagnosticsAllUsesBatchV4ChainReader(t *testing.T) {
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
			matchedID:  matchedPool,
			mismatchID: mismatchPool,
		},
	}

	chainReader := &diagV4BatchCallCounter{diagV4ReaderByPool: diagV4ReaderByPool{
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
	}}

	service := NewAppService(nil, nil, repo, nil, nil, nil, &diagV4Registry{
		poolIDs: []marketuniv4.PoolID{matchedID, mismatchID},
	}, nil, nil, &ChainReaders{
		Head: diagHeadReader{head: 200},
		V4:   chainReader,
	})

	resp, err := service.DiagnosticsAll(context.Background())
	if err != nil {
		t.Fatalf("diagnostics all: %v", err)
	}
	if chainReader.calls != 1 {
		t.Fatalf("expected 1 batch chain read, got %d", chainReader.calls)
	}
	if resp.Count != 1 {
		t.Fatalf("expected 1 mismatching pool, got %#v", resp)
	}
	if resp.Items[0].PoolID != mismatchID.String() {
		t.Fatalf("unexpected pool: %s", resp.Items[0].PoolID)
	}
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
			matchedID:  matchedPool,
			mismatchID: mismatchPool,
		},
	}

	service := NewAppService(nil, nil, repo, nil, nil, nil, &diagV4Registry{
		poolIDs: []marketuniv4.PoolID{matchedID, mismatchID},
	}, nil, nil, &ChainReaders{
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

func (r *diagV4PoolRepoByID) Save(context.Context, *marketuniv4.Pool) error    { return nil }
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

type diagBalancerPoolRepo struct {
	pools map[marketbalancer.PoolID]*marketbalancer.Pool
}

func (r *diagBalancerPoolRepo) Save(context.Context, *marketbalancer.Pool) error    { return nil }
func (r *diagBalancerPoolRepo) Delete(context.Context, marketbalancer.PoolID) error { return nil }
func (r *diagBalancerPoolRepo) AdvanceSyncProgress(context.Context, marketbalancer.PoolID, uint64) error {
	return nil
}
func (r *diagBalancerPoolRepo) AdvanceSyncProgressMany(context.Context, []marketbalancer.PoolID, uint64) error {
	return nil
}
func (r *diagBalancerPoolRepo) Get(_ context.Context, id marketbalancer.PoolID) (*marketbalancer.Pool, error) {
	pool := r.pools[id]
	if pool == nil {
		return nil, nil
	}
	return pool.Clone(), nil
}

type diagBalancerRegistry struct {
	poolIDs []marketbalancer.PoolID
	specs   map[marketbalancer.PoolID]marketbalancer.PoolSpec
}

func (r *diagBalancerRegistry) List(context.Context) ([]marketbalancer.PoolID, error) {
	return append([]marketbalancer.PoolID(nil), r.poolIDs...), nil
}
func (r *diagBalancerRegistry) GetSpec(_ context.Context, id marketbalancer.PoolID) (marketbalancer.PoolSpec, error) {
	return r.specs[id], nil
}
func (r *diagBalancerRegistry) Add(context.Context, marketbalancer.PoolID, marketbalancer.PoolSpec) error {
	return nil
}
func (r *diagBalancerRegistry) Remove(context.Context, marketbalancer.PoolID) error { return nil }

type diagBalancerReaderByPool map[marketbalancer.PoolID]*marketbalancer.BootstrapData

func (r diagBalancerReaderByPool) ReadBalancerState(_ context.Context, poolID marketbalancer.PoolID, _ marketbalancer.PoolSpec, _ uint64) (*marketbalancer.BootstrapData, error) {
	state := r[poolID]
	if state == nil {
		return nil, fmt.Errorf("pool not found")
	}
	return state, nil
}

func (r diagBalancerReaderByPool) ReadManyBalancerStates(_ context.Context, inputs []marketbalancer.BootstrapInput, _ uint64) (map[marketbalancer.PoolID]*marketbalancer.BootstrapData, error) {
	out := make(map[marketbalancer.PoolID]*marketbalancer.BootstrapData, len(inputs))
	for _, input := range inputs {
		state := r[input.PoolID]
		if state == nil {
			continue
		}
		out[input.PoolID] = state
	}
	return out, nil
}

type diagBalancerBatchCallCounter struct {
	diagBalancerReaderByPool
	calls int
}

func (r *diagBalancerBatchCallCounter) ReadManyBalancerStates(ctx context.Context, inputs []marketbalancer.BootstrapInput, blockNumber uint64) (map[marketbalancer.PoolID]*marketbalancer.BootstrapData, error) {
	r.calls++
	return r.diagBalancerReaderByPool.ReadManyBalancerStates(ctx, inputs, blockNumber)
}

func TestDiagnosticsBalancerComparesChainState(t *testing.T) {
	poolID := marketbalancer.PoolID(common.HexToHash("0x1000000000000000000000000000000000000000000000000000000000000000"))
	token0 := common.HexToAddress("0x0000000000000000000000000000000000000001")
	token1 := common.HexToAddress("0x0000000000000000000000000000000000000002")
	pool := mustDiagBalancerPool(t, poolID, token0, token1)
	pool.LastBlockNumber = 100
	pool.Status = market.PoolStatusReady

	spec := diagBalancerSpec(pool)
	service := NewAppService(nil, nil, nil, &diagBalancerPoolRepo{
		pools: map[marketbalancer.PoolID]*marketbalancer.Pool{poolID: pool},
	}, nil, nil, nil, &diagBalancerRegistry{
		poolIDs: []marketbalancer.PoolID{poolID},
		specs:   map[marketbalancer.PoolID]marketbalancer.PoolSpec{poolID: spec},
	}, nil, &ChainReaders{
		Head: diagHeadReader{head: 105},
		Balancer: diagBalancerReaderByPool{
			poolID: diagBalancerBootstrap(pool, spec, 100),
		},
	})

	resp, err := service.Diagnostics(context.Background(), DiagnosticsRequest{
		PoolType:       PoolTypeBalancer,
		BalancerPoolID: poolID,
	})
	if err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	if resp.BalancerDiff == nil || !balancerStateConsistent(*resp.BalancerDiff) {
		t.Fatalf("expected matching balancer state, got %#v", resp.BalancerDiff)
	}
	if resp.Local.BlockLag != 5 {
		t.Fatalf("expected block lag 5, got %d", resp.Local.BlockLag)
	}
	if resp.Local.Balances[token0.Hex()] != "1000" {
		t.Fatalf("expected local token0 balance, got %#v", resp.Local.Balances)
	}
}

func TestDiagnosticsAllIncludesMismatchingBalancerPools(t *testing.T) {
	matchedID := marketbalancer.PoolID(common.HexToHash("0x1"))
	mismatchID := marketbalancer.PoolID(common.HexToHash("0x2"))
	token0 := common.HexToAddress("0x0000000000000000000000000000000000000001")
	token1 := common.HexToAddress("0x0000000000000000000000000000000000000002")
	matchedPool := mustDiagBalancerPool(t, matchedID, token0, token1)
	mismatchPool := mustDiagBalancerPool(t, mismatchID, token0, token1)
	matchedPool.LastBlockNumber = 200
	mismatchPool.LastBlockNumber = 200

	matchedSpec := diagBalancerSpec(matchedPool)
	mismatchSpec := diagBalancerSpec(mismatchPool)
	mismatchChain := diagBalancerBootstrap(mismatchPool, mismatchSpec, 200)
	mismatchChain.Balances[token0] = big.NewInt(999)
	reader := &diagBalancerBatchCallCounter{diagBalancerReaderByPool: diagBalancerReaderByPool{
		matchedID:  diagBalancerBootstrap(matchedPool, matchedSpec, 200),
		mismatchID: mismatchChain,
	}}

	service := NewAppService(nil, nil, nil, &diagBalancerPoolRepo{
		pools: map[marketbalancer.PoolID]*marketbalancer.Pool{
			matchedID:  matchedPool,
			mismatchID: mismatchPool,
		},
	}, nil, nil, nil, &diagBalancerRegistry{
		poolIDs: []marketbalancer.PoolID{matchedID, mismatchID},
		specs: map[marketbalancer.PoolID]marketbalancer.PoolSpec{
			matchedID:  matchedSpec,
			mismatchID: mismatchSpec,
		},
	}, nil, &ChainReaders{
		Head:     diagHeadReader{head: 200},
		Balancer: reader,
	})

	resp, err := service.DiagnosticsAll(context.Background())
	if err != nil {
		t.Fatalf("diagnostics all: %v", err)
	}
	if reader.calls != 1 {
		t.Fatalf("expected 1 balancer batch read, got %d", reader.calls)
	}
	if resp.Count != 1 {
		t.Fatalf("expected 1 mismatching pool, got %#v", resp)
	}
	if resp.Items[0].PoolID != mismatchID.String() || resp.Items[0].BalancerDiff.BalancesMatch {
		t.Fatalf("unexpected diagnostics item: %#v", resp.Items[0])
	}
}

func mustDiagBalancerPool(t *testing.T, poolID marketbalancer.PoolID, token0, token1 common.Address) *marketbalancer.Pool {
	t.Helper()
	pool, err := marketbalancer.NewPool(
		poolID,
		common.HexToAddress("0x00000000000000000000000000000000000000aa"),
		common.HexToAddress("0x00000000000000000000000000000000000000bb"),
		marketbalancer.PoolTypeWeighted,
		[]common.Address{token0, token1},
	)
	if err != nil {
		t.Fatalf("new balancer pool: %v", err)
	}
	pool.Balances[token0] = big.NewInt(1000)
	pool.Balances[token1] = big.NewInt(2000)
	pool.Weights[token0] = big.NewInt(50)
	pool.Weights[token1] = big.NewInt(50)
	pool.SwapFeePercentage = big.NewInt(1)
	pool.Status = market.PoolStatusReady
	return pool
}

func diagBalancerSpec(pool *marketbalancer.Pool) marketbalancer.PoolSpec {
	return marketbalancer.PoolSpec{
		Address:      pool.Address,
		Vault:        pool.Vault,
		Type:         pool.Type,
		VaultVersion: marketbalancer.VaultV2,
	}
}

func diagBalancerBootstrap(pool *marketbalancer.Pool, spec marketbalancer.PoolSpec, blockNumber uint64) *marketbalancer.BootstrapData {
	clone := pool.Clone()
	return &marketbalancer.BootstrapData{
		Spec:              spec,
		Tokens:            append([]common.Address(nil), clone.Tokens...),
		Balances:          clone.Balances,
		Weights:           clone.Weights,
		Amplification:     clone.Amplification,
		SwapFeePercentage: clone.SwapFeePercentage,
		BlockNumber:       blockNumber,
	}
}
