package arbitrageapp_test

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	arbitrageapp "github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage"
	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	domainquote "github.com/brianliu-sysu/uniswapv3/internal/domain/quote"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
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

type memoryOpportunityRepo struct {
	items map[string]*domainarb.Opportunity
}

func newMemoryOpportunityRepo() *memoryOpportunityRepo {
	return &memoryOpportunityRepo{items: make(map[string]*domainarb.Opportunity)}
}

func (r *memoryOpportunityRepo) Save(_ context.Context, opportunity *domainarb.Opportunity) error {
	copyItem := *opportunity
	r.items[opportunity.ID] = &copyItem
	return nil
}

func (r *memoryOpportunityRepo) List(_ context.Context, limit int) ([]*domainarb.Opportunity, error) {
	items := make([]*domainarb.Opportunity, 0, len(r.items))
	for _, item := range r.items {
		copyItem := *item
		items = append(items, &copyItem)
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *memoryOpportunityRepo) Delete(_ context.Context, id string) error {
	delete(r.items, id)
	return nil
}

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

func setupQuotedPool(address, token0, token1 common.Address, liquidity int64) *market.Pool {
	pool := market.NewPool(address, token0, token1, 3000, 60)
	meta := market.EventMeta{PoolAddress: address, BlockNumber: 1}
	_ = pool.Apply(market.NewInitializeEvent(meta, sqrtPriceAtTick0(), 0))
	_ = pool.Apply(market.NewMintEvent(meta, common.Address{}, common.Address{}, -120, 120, big.NewInt(liquidity), big.NewInt(1), big.NewInt(1)))
	pool.Status = market.PoolStatusReady
	return pool
}

func TestScanServiceFindsAffectedRoutes(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	poolAB := testToken(10)

	scan := arbitrageapp.NewScanService(domainarb.NewDependencyGraph())
	scan.RegisterRoute(domainarb.RouteRef{
		ID: "cycle-ab",
		Route: domainquote.Route{
			TokenIn:  tokenA,
			TokenOut: tokenA,
			Hops: []domainquote.RouteHop{
				{PoolAddress: poolAB, TokenIn: tokenA, TokenOut: tokenB},
				{PoolAddress: poolAB, TokenIn: tokenB, TokenOut: tokenA},
			},
		},
	})

	affected := scan.FindAffected([]common.Address{poolAB})
	if len(affected) != 1 || affected[0].ID != "cycle-ab" {
		t.Fatalf("expected cycle-ab route, got %+v", affected)
	}
}

func TestScanServiceRegistersTriangleRoutes(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	tokenC := testToken(4)
	poolAB := testToken(10)
	poolBC := testToken(11)
	poolCA := testToken(12)

	graph := domainquote.NewStaticPoolGraph([]domainquote.PoolEdge{
		{PoolAddress: poolAB, Token0: tokenA, Token1: tokenB},
		{PoolAddress: poolBC, Token0: tokenB, Token1: tokenC},
		{PoolAddress: poolCA, Token0: tokenC, Token1: tokenA},
	})

	scan := arbitrageapp.NewScanService(domainarb.NewDependencyGraph())
	count := scan.RegisterTriangleRoutes(graph, tokenA)
	if count != 2 {
		t.Fatalf("expected 2 triangle routes, got %d", count)
	}

	affected := scan.FindAffected([]common.Address{poolBC})
	if len(affected) != 2 {
		t.Fatalf("expected 2 affected triangle routes, got %+v", affected)
	}
}

func TestServicesOnPoolsChangedRunsPipeline(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	poolAB := testToken(10)

	repo := newMemoryPoolRepo()
	pool := setupQuotedPool(poolAB, tokenA, tokenB, 100_000_000_000_000_000)
	if err := repo.Save(context.Background(), pool); err != nil {
		t.Fatalf("save pool: %v", err)
	}

	oppRepo := newMemoryOpportunityRepo()
	services := arbitrageapp.NewServices(arbitrageapp.ServiceDeps{
		Pools:   repo,
		Quotes:  domainquote.NewQuoteService(),
		Gas:     domainarb.NewStaticGasEstimator(100_000, 80_000, big.NewInt(1)),
		Strategies: []domainarb.Strategy{
			domainarb.NewCycleStrategy("cycle-a", tokenA, 2, big.NewInt(1)),
		},
		Readiness:           alwaysReady{},
		Repository:          oppRepo,
		MinAmount:           big.NewInt(1_000_000),
		MaxAmount:           big.NewInt(10_000_000_000),
		OptimizerIterations: 8,
		Routes: []domainarb.RouteRef{
			{
				ID: "cycle-ab",
				Route: domainquote.Route{
					TokenIn:  tokenA,
					TokenOut: tokenA,
					Hops: []domainquote.RouteHop{
						{PoolAddress: poolAB, TokenIn: tokenA, TokenOut: tokenB},
						{PoolAddress: poolAB, TokenIn: tokenB, TokenOut: tokenA},
					},
				},
			},
		},
	})

	if err := services.OnPoolsChanged(context.Background(), 100, []common.Address{poolAB}); err != nil {
		t.Fatalf("on pools changed: %v", err)
	}

	items, err := oppRepo.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("list opportunities: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no opportunities for unprofitable round-trip route, got %d", len(items))
	}
}

func TestPublishServicePersistsOpportunity(t *testing.T) {
	repo := newMemoryOpportunityRepo()
	publish := arbitrageapp.NewPublishService(arbitrageapp.NewRepositoryPublisher(repo))

	opp := domainarb.NewOpportunity(
		"opp-1",
		domainarb.NewCycleStrategy("cycle-a", testToken(2), 2, big.NewInt(1)),
		42,
		domainquote.NewDirectRoute(testToken(10), testToken(2), testToken(3)),
		domainarb.EvaluationResult{
			AmountIn:    big.NewInt(1_000),
			AmountOut:   big.NewInt(1_100),
			GrossProfit: big.NewInt(100),
			NetProfit:   big.NewInt(80),
			Profitable:  true,
			Accepted:    true,
		},
		domainarb.GasEstimate{CostWei: big.NewInt(20)},
		time.Unix(0, 0).UTC(),
	)

	if err := publish.PublishOne(context.Background(), opp); err != nil {
		t.Fatalf("publish: %v", err)
	}
	items, err := repo.List(context.Background(), 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected saved opportunity, got %#v err=%v", items, err)
	}
}
