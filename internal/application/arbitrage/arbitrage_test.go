package arbitrageapp_test

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	arbitrageapp "github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage"
	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	domainquote "github.com/brianliu-sysu/uniswapv3/internal/domain/quote"
	quotepancakev3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/pancakev3"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	quoteuniv3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ3"
	quoteuniv4domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ4"
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
		if pool.Status == market.PoolStatusCatchingUp {
			pool.Status = market.PoolStatusSyncing
		}
	}
	return nil
}

type memoryV4PoolRepo struct {
	pools map[marketuniv4.PoolID]*marketuniv4.Pool
}

func newMemoryV4PoolRepo() *memoryV4PoolRepo {
	return &memoryV4PoolRepo{pools: make(map[marketuniv4.PoolID]*marketuniv4.Pool)}
}

func (r *memoryV4PoolRepo) Save(_ context.Context, pool *marketuniv4.Pool) error {
	r.pools[pool.ID] = pool.Clone()
	return nil
}

func (r *memoryV4PoolRepo) Get(_ context.Context, id marketuniv4.PoolID) (*marketuniv4.Pool, error) {
	pool, ok := r.pools[id]
	if !ok {
		return nil, nil
	}
	return pool.Clone(), nil
}

func (r *memoryV4PoolRepo) Delete(_ context.Context, id marketuniv4.PoolID) error {
	delete(r.pools, id)
	return nil
}

func (r *memoryV4PoolRepo) AdvanceSyncProgress(ctx context.Context, id marketuniv4.PoolID, blockNumber uint64) error {
	return r.AdvanceSyncProgressMany(ctx, []marketuniv4.PoolID{id}, blockNumber)
}

func (r *memoryV4PoolRepo) AdvanceSyncProgressMany(_ context.Context, ids []marketuniv4.PoolID, blockNumber uint64) error {
	for _, id := range ids {
		pool, ok := r.pools[id]
		if !ok || pool == nil {
			return fmt.Errorf("pool %s not found", id.String())
		}
		if blockNumber > pool.LastBlockNumber {
			pool.LastBlockNumber = blockNumber
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

func (alwaysReady) IsSystemReady() bool                          { return true }
func (alwaysReady) IsV3PoolReady(_ common.Address) bool          { return true }
func (alwaysReady) IsPancakeV3PoolReady(_ common.Address) bool   { return true }
func (alwaysReady) IsQuickSwapV3PoolReady(_ common.Address) bool { return true }
func (alwaysReady) IsV4PoolReady(_ marketuniv4.PoolID) bool      { return true }
func (alwaysReady) IsBalancerPoolReady(_ marketbalancer.PoolID) bool {
	return true
}

func testToken(index byte) common.Address {
	return common.HexToAddress(fmt.Sprintf("0x000000000000000000000000000000000000000%x", index))
}

func sqrtPriceAtTick0() *big.Int {
	v, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	return v
}

func setupQuotedPool(address, token0, token1 common.Address, liquidity int64) *marketuniv3.Pool {
	pool := marketuniv3.NewPool(address, token0, token1, 3000, 60)
	meta := marketuniv3.EventMeta{PoolAddress: address, BlockNumber: 1}
	_ = pool.Apply(marketuniv3.NewInitializeEvent(meta, sqrtPriceAtTick0(), 0))
	_ = pool.Apply(marketuniv3.NewMintEvent(meta, common.Address{}, common.Address{}, -120, 120, big.NewInt(liquidity), big.NewInt(1), big.NewInt(1)))
	pool.Status = market.PoolStatusReady
	return pool
}

func setupV4Pool(token0, token1 common.Address, liquidity int64) (*marketuniv4.Pool, marketuniv4.PoolID) {
	key := marketuniv4.PoolKey{
		Currency0:   token0,
		Currency1:   token1,
		Fee:         3000,
		TickSpacing: 60,
	}
	id, err := marketuniv4.ComputePoolID(key)
	if err != nil {
		panic(err)
	}

	pool := marketuniv4.NewPool(id, key)
	meta := marketuniv4.EventMeta{PoolID: id, BlockNumber: 1}
	_ = pool.Apply(marketuniv4.NewInitializeEvent(meta, sqrtPriceAtTick0(), 0))
	_ = pool.Apply(marketuniv4.NewModifyLiquidityEvent(meta, common.Address{}, -120, 120, big.NewInt(liquidity), common.Hash{}))
	pool.Status = market.PoolStatusReady
	return pool, id
}

func unifiedQuotes() *quoteunified.QuoteService {
	return quoteunified.NewQuoteService(
		quoteuniv3domain.NewQuoteService(),
		quotepancakev3domain.NewQuoteService(),
		quoteuniv4domain.NewQuoteService(),
	)
}

func TestScanServiceFindsAffectedRoutes(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	poolAB := testToken(10)

	scan := arbitrageapp.NewScanService(domainarb.NewDependencyGraph())
	scan.RegisterRoute(domainarb.RouteRef{
		ID: "cycle-ab",
		Route: quoteunified.Route{
			TokenIn:  tokenA,
			TokenOut: tokenA,
			Hops: []quoteunified.RouteHop{
				{Version: quoteunified.PoolVersionV3, PoolV3: poolAB, TokenIn: tokenA, TokenOut: tokenB},
				{Version: quoteunified.PoolVersionV3, PoolV3: poolAB, TokenIn: tokenB, TokenOut: tokenA},
			},
		},
	})

	affected := scan.FindAffected([]common.Address{poolAB}, nil, nil, nil)
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

	affected := scan.FindAffected([]common.Address{poolBC}, nil, nil, nil)
	if len(affected) != 2 {
		t.Fatalf("expected 2 affected triangle routes, got %+v", affected)
	}
}

func TestScanServiceRegistersMixedTriangleRoutes(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	tokenC := testToken(4)
	poolAB := testToken(10)
	poolCA := testToken(12)

	poolBC, poolBCID := setupV4Pool(tokenB, tokenC, 1_000_000_000_000)

	graph := quoteunified.NewStaticPoolGraph([]quoteunified.PoolEdge{
		{Version: quoteunified.PoolVersionV3, PoolV3: poolAB, Token0: tokenA, Token1: tokenB},
		{Version: quoteunified.PoolVersionV4, PoolV4: poolBCID, Token0: tokenB, Token1: tokenC},
		{Version: quoteunified.PoolVersionV3, PoolV3: poolCA, Token0: tokenC, Token1: tokenA},
	})

	scan := arbitrageapp.NewScanService(domainarb.NewDependencyGraph())
	count := scan.RegisterUnifiedTriangleRoutes(graph, tokenA)
	if count != 2 {
		t.Fatalf("expected 2 mixed triangle routes, got %d", count)
	}

	affected := scan.FindAffected(nil, nil, nil, []marketuniv4.PoolID{poolBCID})
	if len(affected) != 2 {
		t.Fatalf("expected 2 affected mixed triangle routes, got %+v", affected)
	}
	_ = poolBC
}

func TestScanServiceFindsAffectedPancakeRoutes(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	poolAB := testToken(10)

	scan := arbitrageapp.NewScanService(domainarb.NewDependencyGraph())
	scan.RegisterRoute(domainarb.RouteRef{
		ID: "cycle-pancake",
		Route: quoteunified.Route{
			TokenIn:  tokenA,
			TokenOut: tokenA,
			Hops: []quoteunified.RouteHop{
				{Version: quoteunified.PoolVersionPancakeV3, PoolPancakeV3: poolAB, TokenIn: tokenA, TokenOut: tokenB},
				{Version: quoteunified.PoolVersionPancakeV3, PoolPancakeV3: poolAB, TokenIn: tokenB, TokenOut: tokenA},
			},
		},
	})

	affected := scan.FindAffected(nil, []common.Address{poolAB}, nil, nil)
	if len(affected) != 1 || affected[0].ID != "cycle-pancake" {
		t.Fatalf("expected cycle-pancake route, got %+v", affected)
	}

	notAffected := scan.FindAffected([]common.Address{poolAB}, nil, nil, nil)
	if len(notAffected) != 0 {
		t.Fatalf("expected no univ3 matches for pancake pool, got %+v", notAffected)
	}
}

func TestScanServiceFindsAffectedQuickSwapRoutes(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	poolAB := testToken(10)

	scan := arbitrageapp.NewScanService(domainarb.NewDependencyGraph())
	scan.RegisterRoute(domainarb.RouteRef{
		ID: "cycle-quickswap",
		Route: quoteunified.Route{
			TokenIn:  tokenA,
			TokenOut: tokenA,
			Hops: []quoteunified.RouteHop{
				{Version: quoteunified.PoolVersionQuickSwapV3, PoolQuickSwapV3: poolAB, TokenIn: tokenA, TokenOut: tokenB},
				{Version: quoteunified.PoolVersionQuickSwapV3, PoolQuickSwapV3: poolAB, TokenIn: tokenB, TokenOut: tokenA},
			},
		},
	})

	affected := scan.FindAffected(nil, nil, []common.Address{poolAB}, nil)
	if len(affected) != 1 || affected[0].ID != "cycle-quickswap" {
		t.Fatalf("expected cycle-quickswap route, got %+v", affected)
	}

	notAffected := scan.FindAffected([]common.Address{poolAB}, nil, nil, nil)
	if len(notAffected) != 0 {
		t.Fatalf("expected no univ3 matches for quickswap pool, got %+v", notAffected)
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
		Pools:  repo,
		Quotes: unifiedQuotes(),
		Gas:    domainarb.NewStaticGasEstimator(100_000, 80_000, big.NewInt(1)),
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
				Route: quoteunified.Route{
					TokenIn:  tokenA,
					TokenOut: tokenA,
					Hops: []quoteunified.RouteHop{
						{Version: quoteunified.PoolVersionV3, PoolV3: poolAB, TokenIn: tokenA, TokenOut: tokenB},
						{Version: quoteunified.PoolVersionV3, PoolV3: poolAB, TokenIn: tokenB, TokenOut: tokenA},
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

func TestRefreshTriangleRoutesRebuildsGraph(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	tokenC := testToken(4)
	poolAB := testToken(10)
	poolBC := testToken(11)
	poolCA := testToken(12)

	repo := newMemoryPoolRepo()
	for _, spec := range []struct {
		address, token0, token1 common.Address
	}{
		{poolAB, tokenA, tokenB},
		{poolBC, tokenB, tokenC},
		{poolCA, tokenC, tokenA},
	} {
		pool := setupQuotedPool(spec.address, spec.token0, spec.token1, 100_000_000_000_000_000)
		if err := repo.Save(context.Background(), pool); err != nil {
			t.Fatalf("save pool: %v", err)
		}
	}

	registry := &staticPoolRegistry{}
	services := arbitrageapp.NewServices(arbitrageapp.ServiceDeps{
		Pools:                 repo,
		Registry:              registry,
		Quotes:                unifiedQuotes(),
		Readiness:             alwaysReady{},
		TriangleEnabled:       true,
		ConfiguredStartTokens: []common.Address{tokenA},
		MinNetProfitWei:       big.NewInt(1),
	})

	if routes := len(services.Scan.Routes()); routes != 0 {
		t.Fatalf("expected no routes before pools are registered, got %d", routes)
	}

	registry.addresses = []common.Address{poolAB, poolBC, poolCA}
	routes, err := services.RefreshTriangleRoutes(context.Background())
	if err != nil {
		t.Fatalf("refresh triangle routes: %v", err)
	}
	if routes != 6 {
		t.Fatalf("expected 6 triangle routes for 3 start tokens, got %d", routes)
	}
	if len(services.Scan.Routes()) != 6 {
		t.Fatalf("expected scan service to track 6 routes, got %d", len(services.Scan.Routes()))
	}

	routes, err = services.RefreshTriangleRoutes(context.Background())
	if err != nil {
		t.Fatalf("refresh triangle routes again: %v", err)
	}
	if routes != 6 {
		t.Fatalf("expected 6 triangle routes after refresh, got %d", routes)
	}
	if len(services.Scan.Routes()) != 6 {
		t.Fatalf("expected scan service to keep 6 routes, got %d", len(services.Scan.Routes()))
	}
}

func TestRefreshTriangleRoutesAddsAutoStartTokens(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	tokenC := testToken(4)
	tokenD := testToken(5)
	poolAB := testToken(10)
	poolBC := testToken(11)
	poolCA := testToken(12)
	poolAD := testToken(13)

	repo := newMemoryPoolRepo()
	for _, spec := range []struct {
		address, token0, token1 common.Address
	}{
		{poolAB, tokenA, tokenB},
		{poolBC, tokenB, tokenC},
		{poolCA, tokenC, tokenA},
		{poolAD, tokenA, tokenD},
	} {
		pool := setupQuotedPool(spec.address, spec.token0, spec.token1, 100_000_000_000_000_000)
		if err := repo.Save(context.Background(), pool); err != nil {
			t.Fatalf("save pool: %v", err)
		}
	}

	registry := &staticPoolRegistry{
		addresses: []common.Address{poolAB, poolBC, poolCA, poolAD},
	}
	services := arbitrageapp.NewServices(arbitrageapp.ServiceDeps{
		Pools:           repo,
		Registry:        registry,
		Quotes:          unifiedQuotes(),
		Readiness:       alwaysReady{},
		TriangleEnabled: true,
		MinNetProfitWei: big.NewInt(1),
	})

	if _, err := services.RefreshTriangleRoutes(context.Background()); err != nil {
		t.Fatalf("refresh triangle routes: %v", err)
	}

	startTokens := services.StartTokens()
	if len(startTokens) != 3 {
		t.Fatalf("expected 3 auto start tokens, got %d: %+v", len(startTokens), startTokens)
	}
	if startTokens[0] != tokenA {
		t.Fatalf("expected tokenA to rank first, got %s", startTokens[0].Hex())
	}
}

type staticPoolRegistry struct {
	addresses []common.Address
}

func (r *staticPoolRegistry) List(context.Context) ([]common.Address, error) {
	return append([]common.Address(nil), r.addresses...), nil
}

func (r *staticPoolRegistry) Add(context.Context, common.Address) error    { return nil }
func (r *staticPoolRegistry) Remove(context.Context, common.Address) error { return nil }

func TestScanServiceRegistersSpreadRoutes(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	poolAB1 := testToken(10)
	poolAB2 := testToken(11)

	graph := quoteunified.NewStaticPoolGraph([]quoteunified.PoolEdge{
		{Version: quoteunified.PoolVersionV3, PoolV3: poolAB1, Token0: tokenA, Token1: tokenB},
		{Version: quoteunified.PoolVersionV3, PoolV3: poolAB2, Token0: tokenA, Token1: tokenB},
	})

	scan := arbitrageapp.NewScanService(domainarb.NewDependencyGraph())
	count := scan.RegisterUnifiedSpreadRoutes(graph, tokenA)
	if count != 2 {
		t.Fatalf("expected 2 spread routes, got %d", count)
	}

	affected := scan.FindAffected([]common.Address{poolAB1}, nil, nil, nil)
	if len(affected) != 2 {
		t.Fatalf("expected 2 affected spread routes, got %+v", affected)
	}
}

func TestRefreshSpreadRoutesRebuildsGraph(t *testing.T) {
	tokenA := testToken(2)
	tokenB := testToken(3)
	poolAB1 := testToken(10)
	poolAB2 := testToken(11)

	repo := newMemoryPoolRepo()
	for _, spec := range []struct {
		address, token0, token1 common.Address
	}{
		{poolAB1, tokenA, tokenB},
		{poolAB2, tokenA, tokenB},
	} {
		pool := setupQuotedPool(spec.address, spec.token0, spec.token1, 100_000_000_000_000_000)
		if err := repo.Save(context.Background(), pool); err != nil {
			t.Fatalf("save pool: %v", err)
		}
	}

	registry := &staticPoolRegistry{addresses: []common.Address{poolAB1, poolAB2}}
	services := arbitrageapp.NewServices(arbitrageapp.ServiceDeps{
		Pools:             repo,
		Registry:          registry,
		Quotes:            unifiedQuotes(),
		Readiness:         alwaysReady{},
		SpreadEnabled:     true,
		SpreadStartTokens: []common.Address{tokenA},
		MinNetProfitWei:   big.NewInt(1),
	})

	routes, err := services.RefreshArbitrageRoutes(context.Background())
	if err != nil {
		t.Fatalf("refresh spread routes: %v", err)
	}
	if routes != 4 {
		t.Fatalf("expected 4 spread routes for both start tokens, got %d", routes)
	}
}

func TestPublishServicePersistsOpportunity(t *testing.T) {
	repo := newMemoryOpportunityRepo()
	publish := arbitrageapp.NewPublishService(arbitrageapp.NewRepositoryPublisher(repo))

	opp := domainarb.NewOpportunity(
		"opp-1",
		domainarb.NewCycleStrategy("cycle-a", testToken(2), 2, big.NewInt(1)),
		42,
		quoteunified.NewDirectV3Route(testToken(10), testToken(2), testToken(3)),
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
