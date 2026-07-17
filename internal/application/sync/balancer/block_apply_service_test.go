package balancersync

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

func TestBlockApplyAnchorsV3PoolStateFromChain(t *testing.T) {
	ctx := context.Background()
	poolID := marketbalancer.PoolID(common.HexToHash("0x1000000000000000000000000000000000000000000000000000000000000000"))
	token0 := common.HexToAddress("0x0000000000000000000000000000000000000001")
	token1 := common.HexToAddress("0x0000000000000000000000000000000000000002")
	spec := marketbalancer.PoolSpec{
		Address:      common.HexToAddress("0x00000000000000000000000000000000000000aa"),
		Vault:        common.HexToAddress("0x00000000000000000000000000000000000000bb"),
		Type:         marketbalancer.PoolTypeWeighted,
		VaultVersion: marketbalancer.VaultV3,
	}
	pool := mustTestBalancerPool(t, poolID, spec, token0, token1)
	pool.Balances[token0] = big.NewInt(1000)
	pool.Balances[token1] = big.NewInt(2000)
	pool.LastBlockNumber = 99
	pool.Status = market.PoolStatusSyncing

	pools := newTestBalancerPoolRepo(pool)
	reader := testBalancerBootstrapReader{data: &BootstrapData{
		Spec:              spec,
		Tokens:            []common.Address{token0, token1},
		Balances:          map[common.Address]*big.Int{token0: big.NewInt(980), token1: big.NewInt(2090)},
		Weights:           map[common.Address]*big.Int{token0: big.NewInt(50), token1: big.NewInt(50)},
		Amplification:     big.NewInt(0),
		SwapFeePercentage: big.NewInt(1),
		BlockNumber:       100,
	}}
	service := NewBlockApplyService(
		pools,
		&testBalancerCheckpointRepo{},
		nil,
		NewReadinessService(),
		testBalancerRegistry{specs: map[marketbalancer.PoolID]marketbalancer.PoolSpec{poolID: spec}},
		reader,
		nil,
	)

	_, err := service.ApplyBlock(ctx, ApplyBlockRequest{
		BlockNumber:  100,
		TrackedPools: []marketbalancer.PoolID{poolID},
		Events: []marketbalancer.PoolEvent{
			marketbalancer.NewSwapEvent(
				marketbalancer.EventMeta{PoolID: poolID, BlockNumber: 100},
				token1,
				token0,
				big.NewInt(100),
				big.NewInt(10),
			),
		},
	})
	if err != nil {
		t.Fatalf("apply block: %v", err)
	}

	saved, err := pools.Get(ctx, poolID)
	if err != nil {
		t.Fatalf("load saved pool: %v", err)
	}
	if saved.Balances[token0].Cmp(big.NewInt(980)) != 0 || saved.Balances[token1].Cmp(big.NewInt(2090)) != 0 {
		t.Fatalf("expected chain-anchored balances, got %#v", saved.Balances)
	}
}

func mustTestBalancerPool(t *testing.T, poolID marketbalancer.PoolID, spec marketbalancer.PoolSpec, tokens ...common.Address) *marketbalancer.Pool {
	t.Helper()
	pool, err := marketbalancer.NewPool(poolID, spec.Address, spec.Vault, spec.Type, tokens)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	return pool
}

type testBalancerPoolRepo struct {
	pools map[marketbalancer.PoolID]*marketbalancer.Pool
}

func newTestBalancerPoolRepo(pools ...*marketbalancer.Pool) *testBalancerPoolRepo {
	repo := &testBalancerPoolRepo{pools: make(map[marketbalancer.PoolID]*marketbalancer.Pool, len(pools))}
	for _, pool := range pools {
		repo.pools[pool.ID] = pool.Clone()
	}
	return repo
}

func (r *testBalancerPoolRepo) Save(_ context.Context, pool *marketbalancer.Pool) error {
	r.pools[pool.ID] = pool.Clone()
	return nil
}

func (r *testBalancerPoolRepo) Get(_ context.Context, id marketbalancer.PoolID) (*marketbalancer.Pool, error) {
	pool := r.pools[id]
	if pool == nil {
		return nil, nil
	}
	return pool.Clone(), nil
}

func (r *testBalancerPoolRepo) Delete(_ context.Context, id marketbalancer.PoolID) error {
	delete(r.pools, id)
	return nil
}

func (r *testBalancerPoolRepo) AdvanceSyncProgress(ctx context.Context, id marketbalancer.PoolID, blockNumber uint64) error {
	return r.AdvanceSyncProgressMany(ctx, []marketbalancer.PoolID{id}, blockNumber)
}

func (r *testBalancerPoolRepo) AdvanceSyncProgressMany(_ context.Context, ids []marketbalancer.PoolID, blockNumber uint64) error {
	for _, id := range ids {
		if pool := r.pools[id]; pool != nil && blockNumber > pool.LastBlockNumber {
			pool.LastBlockNumber = blockNumber
		}
	}
	return nil
}

type testBalancerCheckpointRepo struct{}

func (testBalancerCheckpointRepo) Save(ctx context.Context, checkpoint *blockchain.BalancerCheckpoint) error {
	return testBalancerCheckpointRepo{}.SaveMany(ctx, []*blockchain.BalancerCheckpoint{checkpoint})
}

func (testBalancerCheckpointRepo) SaveMany(context.Context, []*blockchain.BalancerCheckpoint) error {
	return nil
}

func (testBalancerCheckpointRepo) Get(context.Context, marketbalancer.PoolID) (*blockchain.BalancerCheckpoint, error) {
	return nil, nil
}

func (testBalancerCheckpointRepo) Delete(context.Context, marketbalancer.PoolID) error {
	return nil
}

type testBalancerRegistry struct {
	specs map[marketbalancer.PoolID]marketbalancer.PoolSpec
}

func (r testBalancerRegistry) List(context.Context) ([]marketbalancer.PoolID, error) {
	ids := make([]marketbalancer.PoolID, 0, len(r.specs))
	for id := range r.specs {
		ids = append(ids, id)
	}
	return ids, nil
}

func (r testBalancerRegistry) GetSpec(_ context.Context, id marketbalancer.PoolID) (marketbalancer.PoolSpec, error) {
	spec, ok := r.specs[id]
	if !ok {
		return marketbalancer.PoolSpec{}, fmt.Errorf("balancer pool %s not found in registry", id)
	}
	return spec, nil
}

func (r testBalancerRegistry) Add(_ context.Context, id marketbalancer.PoolID, spec marketbalancer.PoolSpec) error {
	r.specs[id] = spec
	return nil
}

func (r testBalancerRegistry) Remove(_ context.Context, id marketbalancer.PoolID) error {
	delete(r.specs, id)
	return nil
}

type testBalancerBootstrapReader struct {
	data *BootstrapData
}

func (r testBalancerBootstrapReader) ReadBootstrapData(context.Context, marketbalancer.PoolID, marketbalancer.PoolSpec, uint64) (*BootstrapData, error) {
	return r.data, nil
}

func (r testBalancerBootstrapReader) ReadManyBootstrapData(_ context.Context, inputs []BootstrapInput, _ uint64) (map[marketbalancer.PoolID]*BootstrapData, error) {
	out := make(map[marketbalancer.PoolID]*BootstrapData, len(inputs))
	for _, input := range inputs {
		out[input.PoolID] = r.data
	}
	return out, nil
}
