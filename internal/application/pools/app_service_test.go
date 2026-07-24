package poolsapp_test

import (
	"context"
	"fmt"
	"testing"

	assetapp "github.com/brianliu-sysu/uniswapv3/internal/application/asset"
	poolsapp "github.com/brianliu-sysu/uniswapv3/internal/application/pools"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	"github.com/ethereum/go-ethereum/common"
)

type memoryPoolRepo struct {
	pools map[common.Address]*marketuniv3.Pool
}

func newMemoryPoolRepo() *memoryPoolRepo {
	return &memoryPoolRepo{pools: make(map[common.Address]*marketuniv3.Pool)}
}

func (r *memoryPoolRepo) Save(_ context.Context, pool *marketuniv3.Pool) error {
	r.pools[pool.Address] = pool.Clone()
	return nil
}

func (r *memoryPoolRepo) Get(_ context.Context, address common.Address) (*marketuniv3.Pool, error) {
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
	}
	return nil
}

type staticRegistry struct {
	addresses []common.Address
}

func (r *staticRegistry) List(context.Context) ([]common.Address, error) {
	return append([]common.Address(nil), r.addresses...), nil
}

func (r *staticRegistry) Add(context.Context, common.Address) error    { return nil }
func (r *staticRegistry) Remove(context.Context, common.Address) error { return nil }

type memoryTokenRepo struct {
	tokens map[common.Address]*asset.Token
}

func (r *memoryTokenRepo) Save(_ context.Context, token *asset.Token) error {
	if r.tokens == nil {
		r.tokens = make(map[common.Address]*asset.Token)
	}
	copyToken := *token
	r.tokens[token.Address] = &copyToken
	return nil
}

func (r *memoryTokenRepo) Get(_ context.Context, address common.Address) (*asset.Token, error) {
	token, ok := r.tokens[address]
	if !ok {
		return nil, nil
	}
	copyToken := *token
	return &copyToken, nil
}

func (r *memoryTokenRepo) GetMany(_ context.Context, addresses []common.Address) (map[common.Address]*asset.Token, error) {
	out := make(map[common.Address]*asset.Token)
	for _, address := range addresses {
		if token, ok := r.tokens[address]; ok {
			copyToken := *token
			out[address] = &copyToken
		}
	}
	return out, nil
}

func testToken(index byte) common.Address {
	return common.HexToAddress(fmt.Sprintf("0x00000000000000000000000000000000000000%02x", index))
}

func TestAppServiceListPools(t *testing.T) {
	token0 := testToken(1)
	token1 := testToken(2)
	poolAddr := testToken(10)

	repo := newMemoryPoolRepo()
	pool := marketuniv3.NewPool(poolAddr, token0, token1, 3000, 60)
	pool.Status = market.PoolStatusReady
	if err := repo.Save(context.Background(), pool); err != nil {
		t.Fatalf("save pool: %v", err)
	}

	tokenRepo := &memoryTokenRepo{
		tokens: map[common.Address]*asset.Token{
			token0: {Address: token0, Symbol: "TK0", Decimal: 18},
			token1: {Address: token1, Symbol: "TK1", Decimal: 6},
		},
	}

	adapter := poolsapp.NewUniv3Adapter(repo, &staticRegistry{addresses: []common.Address{poolAddr}}, nil)
	service := poolsapp.NewAppService(poolsapp.ServiceDeps{
		Protocols: []poolsapp.ProtocolAdapter{adapter},
	})

	resp, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("expected 1 pool, got %d", resp.Count)
	}
	item := resp.Items[0]
	if item.PoolType != poolsapp.PoolTypeUniv3 || item.PoolAddress != poolAddr.Hex() {
		t.Fatalf("unexpected pool item: %#v", item)
	}
	if item.Fee != 3000 {
		t.Fatalf("expected fee 3000, got %d", item.Fee)
	}

	service = poolsapp.NewAppService(poolsapp.ServiceDeps{
		Protocols: []poolsapp.ProtocolAdapter{adapter},
		Tokens:    assetapp.NewTokenMetadataService(tokenRepo, nil),
	})
	resp, err = service.List(context.Background())
	if err != nil {
		t.Fatalf("list pools with tokens: %v", err)
	}
	if resp.Items[0].Token0.Symbol != "TK0" || resp.Items[0].Token1.Decimal != 6 {
		t.Fatalf("unexpected token metadata: %#v", resp.Items[0])
	}
}

func TestAppServiceListBalancerPools(t *testing.T) {
	token0 := testToken(1)
	token1 := testToken(2)
	poolAddr := testToken(10)
	poolID := marketbalancer.PoolID(common.HexToHash(poolAddr.Hex()))

	balancerRepo := &balancerMemoryRepo{}
	pool, err := marketbalancer.NewPool(poolID, poolAddr, testToken(20), marketbalancer.PoolTypeStable, []common.Address{token0, token1})
	if err != nil {
		t.Fatalf("new balancer pool: %v", err)
	}
	if err := balancerRepo.Save(context.Background(), pool); err != nil {
		t.Fatalf("save balancer pool: %v", err)
	}

	service := poolsapp.NewAppService(poolsapp.ServiceDeps{
		Protocols: []poolsapp.ProtocolAdapter{poolsapp.NewBalancerAdapter(
			balancerRepo,
			&balancerStaticRegistry{poolIDs: []marketbalancer.PoolID{poolID}},
			nil,
		)},
	})

	resp, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("expected 1 pool, got %d", resp.Count)
	}
	item := resp.Items[0]
	if item.PoolType != poolsapp.PoolTypeBalancer || item.BalancerType != "stable" {
		t.Fatalf("unexpected balancer item: %#v", item)
	}
	if item.PoolAddress != poolAddr.Hex() || len(item.Tokens) != 2 {
		t.Fatalf("unexpected balancer pool fields: %#v", item)
	}
}

type balancerMemoryRepo struct {
	pools map[marketbalancer.PoolID]*marketbalancer.Pool
}

func (r *balancerMemoryRepo) Save(_ context.Context, pool *marketbalancer.Pool) error {
	if r.pools == nil {
		r.pools = make(map[marketbalancer.PoolID]*marketbalancer.Pool)
	}
	r.pools[pool.ID] = pool.Clone()
	return nil
}

func (r *balancerMemoryRepo) Get(_ context.Context, id marketbalancer.PoolID) (*marketbalancer.Pool, error) {
	pool, ok := r.pools[id]
	if !ok {
		return nil, nil
	}
	return pool.Clone(), nil
}

func (r *balancerMemoryRepo) Delete(context.Context, marketbalancer.PoolID) error { return nil }
func (r *balancerMemoryRepo) AdvanceSyncProgress(context.Context, marketbalancer.PoolID, uint64) error {
	return nil
}
func (r *balancerMemoryRepo) AdvanceSyncProgressMany(context.Context, []marketbalancer.PoolID, uint64) error {
	return nil
}

type balancerStaticRegistry struct {
	poolIDs []marketbalancer.PoolID
}

func (r *balancerStaticRegistry) List(context.Context) ([]marketbalancer.PoolID, error) {
	return append([]marketbalancer.PoolID(nil), r.poolIDs...), nil
}
func (r *balancerStaticRegistry) GetSpec(context.Context, marketbalancer.PoolID) (marketbalancer.PoolSpec, error) {
	return marketbalancer.PoolSpec{}, fmt.Errorf("not implemented")
}
func (r *balancerStaticRegistry) Add(context.Context, marketbalancer.PoolID, marketbalancer.PoolSpec) error {
	return nil
}
func (r *balancerStaticRegistry) Remove(context.Context, marketbalancer.PoolID) error { return nil }
