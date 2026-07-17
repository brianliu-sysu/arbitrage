package arbitrageapp

import (
	"context"
	"math/big"
	"testing"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

type stubV3PoolRepo struct {
	pool *marketuniv3.Pool
}

func (r stubV3PoolRepo) Save(context.Context, *marketuniv3.Pool) error { return nil }
func (r stubV3PoolRepo) Delete(context.Context, common.Address) error  { return nil }
func (r stubV3PoolRepo) Get(context.Context, common.Address) (*marketuniv3.Pool, error) {
	return r.pool, nil
}
func (r stubV3PoolRepo) AdvanceSyncProgress(context.Context, common.Address, uint64) error {
	return nil
}
func (r stubV3PoolRepo) AdvanceSyncProgressMany(context.Context, []common.Address, uint64) error {
	return nil
}

func TestLiveExecutionPlanBuilderEncodesV3(t *testing.T) {
	weth := asset.MainnetWETH
	usdt := common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7")
	poolAddr := common.HexToAddress("0x11b815efB8f581194ae79006d24E0d814B7697F6")
	router := common.HexToAddress("0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45")
	executor := common.HexToAddress("0xB45052DD52e14591C5cb4307e8fbd4bC11608f20")
	vault := common.HexToAddress("0xBA12222222228d8Ba445958a75a0704d566BF2C8")

	pool := marketuniv3.NewPool(poolAddr, weth, usdt, 500, 10)
	loader := NewRepositoryRoutePoolLoader(stubV3PoolRepo{pool: pool}, nil, nil, nil, nil)
	cfg := LivePlanConfig{
		WETH:          weth,
		BalancerVault: vault,
		SwapRouterV3:  router,
		Executor:      executor,
	}
	builder := NewLiveExecutionPlanBuilder(cfg, NewLiveCalldataEncoder(cfg, loader))

	opp := &domainarb.Opportunity{
		ID:        "opp-live-1",
		Status:    domainarb.OpportunityStatusAccepted,
		AmountIn:  big.NewInt(1_000_000),
		NetProfit: big.NewInt(100),
		FlashLoan: domainarb.FlashLoanQuote{
			Protocol: domainarb.FlashLoanProtocolBalancer,
			Amount:   big.NewInt(1_000_000),
		},
		Route: quoteunified.Route{
			TokenIn:  weth,
			TokenOut: usdt,
			Hops: []quoteunified.RouteHop{
				{Version: quoteunified.PoolVersionV3, PoolV3: poolAddr, TokenIn: weth, TokenOut: usdt},
			},
		},
	}

	plan, approvals, err := builder.BuildExecutionPlan(context.Background(), opp)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if plan.Loan.Protocol != domaincontract.FlashLoanProtocolBalancer {
		t.Fatalf("unexpected loan protocol %q", plan.Loan.Protocol)
	}
	if len(plan.Routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(plan.Routes))
	}
	if plan.Routes[0].RouterAddress != router {
		t.Fatalf("expected v3 router, got %s", plan.Routes[0].RouterAddress.Hex())
	}
	if got, want := common.Bytes2Hex(plan.Routes[0].Data[:4]), "04e45aaf"; got != want {
		t.Fatalf("expected SwapRouter02 exactInputSingle selector %s, got %s", want, got)
	}
	if plan.Routes[0].FillToken != (common.Address{}) {
		t.Fatalf("first hop should use known amount, not fill")
	}
	if len(approvals) != 1 || approvals[0].Spender != router || approvals[0].Token != weth {
		t.Fatalf("expected one WETH->router approval, got %+v", approvals)
	}
}

func TestLiveExecutionPlanBuilderEncodesUnwrap(t *testing.T) {
	weth := asset.MainnetWETH
	cfg := LivePlanConfig{
		WETH:          weth,
		BalancerVault: common.HexToAddress("0xBA12222222228d8Ba445958a75a0704d566BF2C8"),
		Executor:      common.HexToAddress("0xB45052DD52e14591C5cb4307e8fbd4bC11608f20"),
	}
	builder := NewLiveExecutionPlanBuilder(cfg, NewLiveCalldataEncoder(cfg, nil))

	opp := &domainarb.Opportunity{
		ID:        "opp-unwrap",
		AmountIn:  big.NewInt(1_000_000),
		NetProfit: big.NewInt(1),
		FlashLoan: domainarb.FlashLoanQuote{
			Protocol: domainarb.FlashLoanProtocolBalancer,
			Amount:   big.NewInt(1_000_000),
		},
		Route: quoteunified.Route{
			TokenIn:  weth,
			TokenOut: common.Address{},
			Hops: []quoteunified.RouteHop{
				{Version: quoteunified.PoolVersionUnwrapWETH, TokenIn: weth, TokenOut: common.Address{}},
			},
		},
	}
	plan, _, err := builder.BuildExecutionPlan(context.Background(), opp)
	if err != nil {
		t.Fatalf("build unwrap plan: %v", err)
	}
	if len(plan.Routes) != 1 || plan.Routes[0].RouterAddress != weth {
		t.Fatalf("unexpected unwrap route: %+v", plan.Routes)
	}
}

func TestLiveExecutionPlanBuilderEncodesBalancer(t *testing.T) {
	weth := asset.MainnetWETH
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	vault := common.HexToAddress("0xBA12222222228d8Ba445958a75a0704d566BF2C8")
	executor := common.HexToAddress("0xB45052DD52e14591C5cb4307e8fbd4bC11608f20")
	poolID := marketbalancer.PoolID(common.HexToHash("0xabcdef"))
	pool, err := marketbalancer.NewPool(poolID, common.HexToAddress("0x1111"), vault, marketbalancer.PoolTypeWeighted, []common.Address{weth, usdc})
	if err != nil {
		t.Fatalf("new balancer pool: %v", err)
	}
	loader := NewRepositoryRoutePoolLoader(nil, nil, nil, nil, stubBalancerPoolRepo{pool: pool})
	cfg := LivePlanConfig{
		WETH:          weth,
		BalancerVault: vault,
		Executor:      executor,
	}
	builder := NewLiveExecutionPlanBuilder(cfg, NewLiveCalldataEncoder(cfg, loader))
	opp := &domainarb.Opportunity{
		ID:        "opp-bal",
		AmountIn:  big.NewInt(1_000),
		NetProfit: big.NewInt(10),
		FlashLoan: domainarb.FlashLoanQuote{Protocol: domainarb.FlashLoanProtocolBalancer, Amount: big.NewInt(1_000)},
		Route: quoteunified.Route{
			TokenIn:  weth,
			TokenOut: usdc,
			Hops: []quoteunified.RouteHop{{
				Version:      quoteunified.PoolVersionBalancer,
				PoolBalancer: poolID,
				TokenIn:      weth,
				TokenOut:     usdc,
			}},
		},
	}
	plan, approvals, err := builder.BuildExecutionPlan(context.Background(), opp)
	if err != nil {
		t.Fatalf("build balancer plan: %v", err)
	}
	if len(plan.Routes) != 1 || plan.Routes[0].RouterAddress != vault {
		t.Fatalf("unexpected balancer route: %+v", plan.Routes)
	}
	if len(approvals) != 1 || approvals[0].Spender != vault {
		t.Fatalf("expected vault approval, got %+v", approvals)
	}
}

func TestApplyBPSReduction(t *testing.T) {
	if got := applyBPSReduction(big.NewInt(1_000), 50); got.Cmp(big.NewInt(995)) != 0 {
		t.Fatalf("expected 995 after 0.5%% slippage, got %s", got)
	}
	if got := applyBPSReduction(big.NewInt(995), 8_000); got.Cmp(big.NewInt(199)) != 0 {
		t.Fatalf("expected 199 after coinbase payment, got %s", got)
	}
}

func TestLiveExecutionPlanBuilderReplacesSettlementGraph(t *testing.T) {
	initial := quoteunified.NewStaticPoolGraph(nil)
	replacement := quoteunified.NewStaticPoolGraph([]quoteunified.PoolEdge{{
		Version: quoteunified.PoolVersionV3,
		PoolV3:  common.HexToAddress("0x1"),
		Token0:  common.HexToAddress("0x2"),
		Token1:  common.HexToAddress("0x3"),
	}})
	builder := NewLiveExecutionPlanBuilder(LivePlanConfig{}, nil, initial)
	builder.SetPoolGraph(replacement)

	if builder.poolGraph() != replacement {
		t.Fatal("expected settlement graph snapshot to be replaced")
	}
}

func TestLiveExecutionPlanBuilderEncodesBalancerV3ViaRouter(t *testing.T) {
	weth := asset.MainnetWETH
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	vaultV3 := common.HexToAddress("0xbA1333333333a1BA1108E8412f11850A5C319bA9")
	routerV3 := common.HexToAddress("0xAE563E3f8219521950555F5962419C8919758Ea2")
	executor := common.HexToAddress("0xB45052DD52e14591C5cb4307e8fbd4bC11608f20")
	poolAddr := common.HexToAddress("0x2222")
	poolID := marketbalancer.PoolID(common.BytesToHash(poolAddr.Bytes()))
	pool, err := marketbalancer.NewPool(poolID, poolAddr, vaultV3, marketbalancer.PoolTypeWeighted, []common.Address{weth, usdc})
	if err != nil {
		t.Fatalf("new balancer pool: %v", err)
	}
	loader := NewRepositoryRoutePoolLoader(nil, nil, nil, nil, stubBalancerPoolRepo{pool: pool})
	cfg := LivePlanConfig{
		WETH:             weth,
		BalancerVault:    common.HexToAddress("0xBA12222222228d8Ba445958a75a0704d566BF2C8"),
		BalancerVaultV3:  vaultV3,
		BalancerRouterV3: routerV3,
		Executor:         executor,
	}
	builder := NewLiveExecutionPlanBuilder(cfg, NewLiveCalldataEncoder(cfg, loader))
	opp := &domainarb.Opportunity{
		ID:        "opp-bal-v3",
		AmountIn:  big.NewInt(1_000),
		NetProfit: big.NewInt(10),
		FlashLoan: domainarb.FlashLoanQuote{Protocol: domainarb.FlashLoanProtocolBalancer, Amount: big.NewInt(1_000)},
		Route: quoteunified.Route{
			TokenIn:  weth,
			TokenOut: usdc,
			Hops: []quoteunified.RouteHop{{
				Version:      quoteunified.PoolVersionBalancer,
				PoolBalancer: poolID,
				TokenIn:      weth,
				TokenOut:     usdc,
			}},
		},
	}
	plan, approvals, err := builder.BuildExecutionPlan(context.Background(), opp)
	if err != nil {
		t.Fatalf("build balancer v3 plan: %v", err)
	}
	if len(plan.Routes) != 1 || plan.Routes[0].RouterAddress != routerV3 {
		t.Fatalf("unexpected balancer v3 route: %+v", plan.Routes)
	}
	if len(plan.Routes[0].Data) < domaincontract.BalancerV3SwapExactAmountInOffset+32 {
		t.Fatalf("balancer v3 calldata too short: %d", len(plan.Routes[0].Data))
	}
	gotAmount := new(big.Int).SetBytes(plan.Routes[0].Data[domaincontract.BalancerV3SwapExactAmountInOffset : domaincontract.BalancerV3SwapExactAmountInOffset+32])
	if gotAmount.Cmp(big.NewInt(1_000)) != 0 {
		t.Fatalf("expected exactAmountIn 1000, got %s", gotAmount)
	}
	if len(approvals) != 1 || approvals[0].Spender != routerV3 {
		t.Fatalf("expected router approval, got %+v", approvals)
	}
}

func TestLiveExecutionPlanBuilderEncodesV4ViaUniversalRouter(t *testing.T) {
	weth := asset.MainnetWETH
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	ur := common.HexToAddress("0x66a9893cc07d91d95644aedd05d03f95e1dba8af")
	vault := common.HexToAddress("0xBA12222222228d8Ba445958a75a0704d566BF2C8")
	executor := common.HexToAddress("0xB45052DD52e14591C5cb4307e8fbd4bC11608f20")
	key := marketuniv4.PoolKey{Currency0: weth, Currency1: usdc, Fee: 500, TickSpacing: 10}
	id, err := marketuniv4.ComputePoolID(key)
	if err != nil {
		t.Fatalf("compute pool id: %v", err)
	}
	pool := marketuniv4.NewPool(id, key)
	loader := NewRepositoryRoutePoolLoader(nil, nil, nil, stubV4PoolRepo{pool: pool}, nil)
	cfg := LivePlanConfig{
		WETH:            weth,
		BalancerVault:   vault,
		UniversalRouter: ur,
		Executor:        executor,
	}
	builder := NewLiveExecutionPlanBuilder(cfg, NewLiveCalldataEncoder(cfg, loader))
	opp := &domainarb.Opportunity{
		ID:        "opp-v4-ur",
		AmountIn:  big.NewInt(1_000),
		NetProfit: big.NewInt(10),
		FlashLoan: domainarb.FlashLoanQuote{Protocol: domainarb.FlashLoanProtocolBalancer, Amount: big.NewInt(1_000)},
		Route: quoteunified.Route{
			TokenIn:  weth,
			TokenOut: usdc,
			Hops: []quoteunified.RouteHop{{
				Version:  quoteunified.PoolVersionV4,
				PoolV4:   id,
				TokenIn:  weth,
				TokenOut: usdc,
			}},
		},
	}
	plan, _, err := builder.BuildExecutionPlan(context.Background(), opp)
	if err != nil {
		t.Fatalf("build v4 ur plan: %v", err)
	}
	if len(plan.Routes) != 2 {
		t.Fatalf("expected transfer + UR routes, got %d", len(plan.Routes))
	}
	if plan.Routes[0].RouterAddress != weth {
		t.Fatalf("expected transfer via token contract, got %s", plan.Routes[0].RouterAddress.Hex())
	}
	if plan.Routes[1].RouterAddress != ur {
		t.Fatalf("expected universal router, got %s", plan.Routes[1].RouterAddress.Hex())
	}
}

func TestLiveExecutionPlanBuilderEncodesV4ViaPoolManager(t *testing.T) {
	weth := asset.MainnetWETH
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	pm := common.HexToAddress("0x000000000004444c5dc75cB358380D2e3dE08A90")
	executor := common.HexToAddress("0xB45052DD52e14591C5cb4307e8fbd4bC11608f20")
	key := marketuniv4.PoolKey{Currency0: weth, Currency1: usdc, Fee: 500, TickSpacing: 10}
	id, err := marketuniv4.ComputePoolID(key)
	if err != nil {
		t.Fatalf("compute pool id: %v", err)
	}
	pool := marketuniv4.NewPool(id, key)
	loader := NewRepositoryRoutePoolLoader(nil, nil, nil, stubV4PoolRepo{pool: pool}, nil)
	cfg := LivePlanConfig{
		WETH:        weth,
		PoolManager: pm,
		Executor:    executor,
	}
	builder := NewLiveExecutionPlanBuilder(cfg, NewLiveCalldataEncoder(cfg, loader))
	opp := &domainarb.Opportunity{
		ID:        "opp-v4-pm",
		AmountIn:  big.NewInt(1_000),
		NetProfit: big.NewInt(10),
		FlashLoan: domainarb.FlashLoanQuote{Protocol: domainarb.FlashLoanProtocolUniv4, Amount: big.NewInt(1_000)},
		Route: quoteunified.Route{
			TokenIn:  weth,
			TokenOut: usdc,
			Hops: []quoteunified.RouteHop{{
				Version:  quoteunified.PoolVersionV4,
				PoolV4:   id,
				TokenIn:  weth,
				TokenOut: usdc,
			}},
		},
	}
	plan, _, err := builder.BuildExecutionPlan(context.Background(), opp)
	if err != nil {
		t.Fatalf("build v4 pm plan: %v", err)
	}
	if len(plan.Routes) != 1 || plan.Routes[0].RouterAddress != pm {
		t.Fatalf("unexpected pm route: %+v", plan.Routes)
	}
	if len(plan.SettleCurrencies) < 2 {
		t.Fatalf("expected settle currencies, got %+v", plan.SettleCurrencies)
	}
}

func TestLiveExecutionPlanBuilderRequiresUniversalRouterForLockedV4(t *testing.T) {
	cfg := LivePlanConfig{
		BalancerVault: common.HexToAddress("0xBA12222222228d8Ba445958a75a0704d566BF2C8"),
		SwapRouterV3:  common.HexToAddress("0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45"),
		Executor:      common.HexToAddress("0xB45052DD52e14591C5cb4307e8fbd4bC11608f20"),
	}
	key := marketuniv4.PoolKey{
		Currency0:   asset.MainnetWETH,
		Currency1:   common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
		Fee:         500,
		TickSpacing: 10,
	}
	id, err := marketuniv4.ComputePoolID(key)
	if err != nil {
		t.Fatalf("compute pool id: %v", err)
	}
	loader := NewRepositoryRoutePoolLoader(nil, nil, nil, stubV4PoolRepo{pool: marketuniv4.NewPool(id, key)}, nil)
	builder := NewLiveExecutionPlanBuilder(cfg, NewLiveCalldataEncoder(cfg, loader))
	opp := &domainarb.Opportunity{
		ID:        "opp-v4",
		AmountIn:  big.NewInt(1),
		NetProfit: big.NewInt(1),
		FlashLoan: domainarb.FlashLoanQuote{Protocol: domainarb.FlashLoanProtocolBalancer, Amount: big.NewInt(1)},
		Route: quoteunified.Route{
			TokenIn: asset.MainnetWETH,
			Hops: []quoteunified.RouteHop{{
				Version:  quoteunified.PoolVersionV4,
				PoolV4:   id,
				TokenIn:  asset.MainnetWETH,
				TokenOut: key.Currency1,
			}},
		},
	}
	_, _, err = builder.BuildExecutionPlan(context.Background(), opp)
	if err == nil || !isExecutionUnavailable(err) {
		t.Fatalf("expected execution unavailable without universal router, got %v", err)
	}
}

func TestLiveExecutionPlanBuilderEncodesNativeV4ViaUniversalRouter(t *testing.T) {
	weth := asset.MainnetWETH
	native := common.Address{}
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	ur := common.HexToAddress("0x66a9893cc07d91d95644aedd05d03f95e1dba8af")
	vault := common.HexToAddress("0xBA12222222228d8Ba445958a75a0704d566BF2C8")
	executor := common.HexToAddress("0xB45052DD52e14591C5cb4307e8fbd4bC11608f20")
	key := marketuniv4.PoolKey{Currency0: native, Currency1: usdc, Fee: 500, TickSpacing: 10}
	id, err := marketuniv4.ComputePoolID(key)
	if err != nil {
		t.Fatalf("compute pool id: %v", err)
	}
	pool := marketuniv4.NewPool(id, key)
	loader := NewRepositoryRoutePoolLoader(nil, nil, nil, stubV4PoolRepo{pool: pool}, nil)
	cfg := LivePlanConfig{
		WETH:            weth,
		BalancerVault:   vault,
		UniversalRouter: ur,
		Executor:        executor,
	}
	builder := NewLiveExecutionPlanBuilder(cfg, NewLiveCalldataEncoder(cfg, loader))
	opp := &domainarb.Opportunity{
		ID:        "opp-v4-native",
		AmountIn:  big.NewInt(1_000),
		NetProfit: big.NewInt(10),
		FlashLoan: domainarb.FlashLoanQuote{Protocol: domainarb.FlashLoanProtocolBalancer, Amount: big.NewInt(1_000)},
		Route: quoteunified.Route{
			TokenIn:  weth,
			TokenOut: usdc,
			Hops: []quoteunified.RouteHop{
				{Version: quoteunified.PoolVersionUnwrapWETH, TokenIn: weth, TokenOut: native},
				{
					Version:  quoteunified.PoolVersionV4,
					PoolV4:   id,
					TokenIn:  native,
					TokenOut: usdc,
				},
			},
		},
	}
	plan, _, err := builder.BuildExecutionPlan(context.Background(), opp)
	if err != nil {
		t.Fatalf("build native v4 plan: %v", err)
	}
	if len(plan.Routes) != 2 {
		t.Fatalf("expected unwrap + UR, got %d routes", len(plan.Routes))
	}
	if plan.Routes[1].RouterAddress != ur {
		t.Fatalf("expected universal router, got %s", plan.Routes[1].RouterAddress.Hex())
	}
	if plan.Routes[1].FillSource != domaincontract.FillSourceNativeBalance {
		t.Fatalf("expected native fill source, got %d", plan.Routes[1].FillSource)
	}
	if !plan.Routes[1].AmountAsCallValue {
		t.Fatalf("expected native fill to be used as call value")
	}
}

func TestLiveExecutionPlanBuilderEncodesWrapFromNativeBalance(t *testing.T) {
	weth := asset.MainnetWETH
	native := common.Address{}
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	ur := common.HexToAddress("0x66a9893cc07d91d95644aedd05d03f95e1dba8af")
	vault := common.HexToAddress("0xBA12222222228d8Ba445958a75a0704d566BF2C8")
	executor := common.HexToAddress("0xB45052DD52e14591C5cb4307e8fbd4bC11608f20")
	key := marketuniv4.PoolKey{Currency0: native, Currency1: usdc, Fee: 500, TickSpacing: 10}
	id, err := marketuniv4.ComputePoolID(key)
	if err != nil {
		t.Fatalf("compute pool id: %v", err)
	}
	loader := NewRepositoryRoutePoolLoader(nil, nil, nil, stubV4PoolRepo{pool: marketuniv4.NewPool(id, key)}, nil)
	cfg := LivePlanConfig{
		WETH:            weth,
		BalancerVault:   vault,
		UniversalRouter: ur,
		Executor:        executor,
	}
	builder := NewLiveExecutionPlanBuilder(cfg, NewLiveCalldataEncoder(cfg, loader))
	opp := &domainarb.Opportunity{
		ID:        "opp-wrap-fill",
		AmountIn:  big.NewInt(1_000),
		NetProfit: big.NewInt(10),
		FlashLoan: domainarb.FlashLoanQuote{Protocol: domainarb.FlashLoanProtocolBalancer, Amount: big.NewInt(1_000)},
		Route: quoteunified.Route{
			TokenIn:  usdc,
			TokenOut: weth,
			Hops: []quoteunified.RouteHop{
				{
					Version:  quoteunified.PoolVersionV4,
					PoolV4:   id,
					TokenIn:  usdc,
					TokenOut: native,
				},
				{Version: quoteunified.PoolVersionWrapWETH, TokenIn: native, TokenOut: weth},
			},
		},
	}
	plan, _, err := builder.BuildExecutionPlan(context.Background(), opp)
	if err != nil {
		t.Fatalf("build wrap-fill plan: %v", err)
	}
	if len(plan.Routes) < 2 {
		t.Fatalf("expected transfer+UR+wrap, got %d", len(plan.Routes))
	}
	wrap := plan.Routes[len(plan.Routes)-1]
	if wrap.RouterAddress != weth {
		t.Fatalf("expected wrap via weth, got %s", wrap.RouterAddress.Hex())
	}
	if wrap.FillSource != domaincontract.FillSourceNativeBalance {
		t.Fatalf("expected native fill on wrap, got %d", wrap.FillSource)
	}
	if !wrap.AmountAsCallValue {
		t.Fatalf("expected wrap native fill to be used as call value")
	}
}

func TestLiveExecutionPlanBuilderResolvesUniv3BorrowToken0(t *testing.T) {
	weth := asset.MainnetWETH
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	poolAddr := common.HexToAddress("0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640")
	router := common.HexToAddress("0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45")
	executor := common.HexToAddress("0xB45052DD52e14591C5cb4307e8fbd4bC11608f20")
	// token0=usdc, token1=weth — borrowing WETH must set borrowToken0=false.
	pool := marketuniv3.NewPool(poolAddr, usdc, weth, 500, 10)
	loader := NewRepositoryRoutePoolLoader(stubV3PoolRepo{pool: pool}, nil, nil, nil, nil)
	cfg := LivePlanConfig{
		WETH:         weth,
		SwapRouterV3: router,
		Executor:     executor,
	}
	builder := NewLiveExecutionPlanBuilder(cfg, NewLiveCalldataEncoder(cfg, loader))
	opp := &domainarb.Opportunity{
		ID:        "opp-univ3-flash",
		AmountIn:  big.NewInt(1_000),
		NetProfit: big.NewInt(10),
		FlashLoan: domainarb.FlashLoanQuote{
			Protocol:     domainarb.FlashLoanProtocolUniv3,
			PoolRef:      domainarb.PoolRefFromV3(poolAddr),
			Amount:       big.NewInt(1_000),
			BorrowToken0: true, // stale/wrong; encoder should correct from pool
		},
		Route: quoteunified.Route{
			TokenIn:  weth,
			TokenOut: usdc,
			Hops: []quoteunified.RouteHop{{
				Version:  quoteunified.PoolVersionV3,
				PoolV3:   poolAddr,
				TokenIn:  weth,
				TokenOut: usdc,
			}},
		},
	}
	plan, _, err := builder.BuildExecutionPlan(context.Background(), opp)
	if err != nil {
		t.Fatalf("build univ3 flash plan: %v", err)
	}
	if plan.Loan.BorrowToken0 {
		t.Fatal("expected borrowToken0=false when loan token is pool token1")
	}
}

type stubV4PoolRepo struct {
	pool *marketuniv4.Pool
}

func (r stubV4PoolRepo) Save(context.Context, *marketuniv4.Pool) error { return nil }
func (r stubV4PoolRepo) Delete(context.Context, marketuniv4.PoolID) error {
	return nil
}
func (r stubV4PoolRepo) Get(context.Context, marketuniv4.PoolID) (*marketuniv4.Pool, error) {
	return r.pool, nil
}
func (r stubV4PoolRepo) AdvanceSyncProgress(context.Context, marketuniv4.PoolID, uint64) error {
	return nil
}
func (r stubV4PoolRepo) AdvanceSyncProgressMany(context.Context, []marketuniv4.PoolID, uint64) error {
	return nil
}

type stubBalancerPoolRepo struct {
	pool *marketbalancer.Pool
}

func (r stubBalancerPoolRepo) Save(context.Context, *marketbalancer.Pool) error { return nil }
func (r stubBalancerPoolRepo) Delete(context.Context, marketbalancer.PoolID) error {
	return nil
}
func (r stubBalancerPoolRepo) Get(context.Context, marketbalancer.PoolID) (*marketbalancer.Pool, error) {
	return r.pool, nil
}
func (r stubBalancerPoolRepo) AdvanceSyncProgress(context.Context, marketbalancer.PoolID, uint64) error {
	return nil
}
func (r stubBalancerPoolRepo) AdvanceSyncProgressMany(context.Context, []marketbalancer.PoolID, uint64) error {
	return nil
}
