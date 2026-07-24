package execution

import (
	"context"
	"fmt"
	"math/big"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

// RoutePoolLoader loads pool aggregates needed to encode a route.
type RoutePoolLoader interface {
	LoadRoutePools(ctx context.Context, route quoteunified.Route) (quoteunified.RoutePools, error)
}

type routePoolLoaderFunc func(context.Context, quoteunified.Route) (quoteunified.RoutePools, error)

func (f routePoolLoaderFunc) LoadRoutePools(ctx context.Context, route quoteunified.Route) (quoteunified.RoutePools, error) {
	return f(ctx, route)
}

// LiveCalldataEncoder builds RouterCall calldata from an opportunity route.
type LiveCalldataEncoder struct {
	cfg    LivePlanConfig
	loader RoutePoolLoader
}

func NewLiveCalldataEncoder(cfg LivePlanConfig, loader RoutePoolLoader) *LiveCalldataEncoder {
	return &LiveCalldataEncoder{cfg: cfg, loader: loader}
}

func (e *LiveCalldataEncoder) Encode(
	ctx context.Context,
	opportunity *domainarb.Opportunity,
	loan domaincontract.FlashLoan,
) (domaincontract.ExecutionPlan, []domaincontract.TokenApproval, error) {
	if e == nil {
		return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("live calldata encoder is nil")
	}
	if opportunity == nil {
		return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("opportunity is nil")
	}
	if err := opportunity.ApplyPayload(); err != nil {
		return domaincontract.ExecutionPlan{}, nil, err
	}
	if opportunity.Route.Len() == 0 {
		return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("%w: opportunity route is empty", ErrExecutionPlanUnavailable)
	}

	weth := e.cfg.WETH
	if weth == (common.Address{}) {
		weth = asset.MainnetWETH
	}
	executor := e.cfg.Executor
	if executor == (common.Address{}) {
		return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("executor address is required for live calldata encoding")
	}

	var pools quoteunified.RoutePools
	if e.loader != nil {
		loaded, err := e.loader.LoadRoutePools(ctx, opportunity.Route)
		if err != nil {
			return domaincontract.ExecutionPlan{}, nil, err
		}
		pools = loaded
	}
	loan = resolveUniv3BorrowToken0(loan, pools)

	routes := make([]domaincontract.SwapRoute, 0, opportunity.Route.Len())
	approvals := make([]domaincontract.TokenApproval, 0)
	seenApproval := make(map[string]bool)
	settle := make([]common.Address, 0)
	seenSettle := make(map[common.Address]bool)

	addApproval := func(token, spender common.Address) {
		if token == (common.Address{}) || spender == (common.Address{}) {
			return
		}
		key := token.Hex() + ":" + spender.Hex()
		if seenApproval[key] {
			return
		}
		seenApproval[key] = true
		approvals = append(approvals, domaincontract.TokenApproval{
			Token:   token,
			Spender: spender,
			Amount:  maxUint256(),
		})
	}
	addSettle := func(currency common.Address) {
		if seenSettle[currency] {
			return
		}
		seenSettle[currency] = true
		settle = append(settle, currency)
	}

	for i, hop := range opportunity.Route.Hops {
		fillFromBalance := i > 0 || loan.Amount == nil || loan.Amount.Sign() <= 0
		amountIn := loan.Amount
		if fillFromBalance {
			amountIn = big.NewInt(0)
		} else if amountIn != nil {
			amountIn = new(big.Int).Set(amountIn)
		}
		if i == 0 && !fillFromBalance && hop.Version != quoteunified.PoolVersionWrapWETH {
			if hop.TokenIn != loan.Token {
				return domaincontract.ExecutionPlan{}, nil, fmt.Errorf(
					"first hop tokenIn %s does not match flash loan token %s",
					hop.TokenIn.Hex(), loan.Token.Hex(),
				)
			}
		}

		switch hop.Version {
		case quoteunified.PoolVersionWrapWETH:
			data, err := domaincontract.PackWETHDeposit()
			if err != nil {
				return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("encode wrap hop[%d]: %w", i, err)
			}
			route := domaincontract.SwapRoute{
				RouterAddress: weth,
				Data:          data,
			}
			if fillFromBalance {
				// Native fill overrides msg.value; deposit() has no amount slot to patch.
				route.FillSource = domaincontract.FillSourceNativeBalance
				route.AmountAsCallValue = true
			} else {
				route.Value = amountIn
			}
			routes = append(routes, route)

		case quoteunified.PoolVersionUnwrapWETH:
			data, err := domaincontract.PackWETHWithdraw(amountIn)
			if err != nil {
				return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("encode unwrap hop[%d]: %w", i, err)
			}
			route := domaincontract.SwapRoute{
				RouterAddress: weth,
				Data:          data,
			}
			if fillFromBalance {
				route.FillSource = domaincontract.FillSourceERC20Balance
				route.FillToken = weth
				route.PatchAmount = true
				route.FillOffset = domaincontract.WETHWithdrawAmountOffset
			}
			routes = append(routes, route)

		case quoteunified.PoolVersionV3:
			router := e.cfg.SwapRouterV3
			if router == (common.Address{}) {
				return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("swap_router_v3 address is not configured")
			}
			pool := pools.V3[hop.PoolV3]
			if pool == nil {
				return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("v3 pool %s not loaded", hop.PoolV3.Hex())
			}
			data, err := domaincontract.PackExactInputSingle(domaincontract.ExactInputSingleParams{
				TokenIn:          hop.TokenIn,
				TokenOut:         hop.TokenOut,
				Fee:              big.NewInt(int64(pool.Fee)),
				Recipient:        executor,
				AmountIn:         amountIn,
				AmountOutMinimum: big.NewInt(0),
			})
			if err != nil {
				return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("encode v3 hop[%d]: %w", i, err)
			}
			route := domaincontract.SwapRoute{
				RouterAddress: router,
				Data:          data,
			}
			if fillFromBalance {
				route.FillSource = domaincontract.FillSourceERC20Balance
				route.FillToken = hop.TokenIn
				route.PatchAmount = true
				route.FillOffset = domaincontract.ExactInputSingleAmountInOffset
			}
			routes = append(routes, route)
			addApproval(hop.TokenIn, router)

		case quoteunified.PoolVersionPancakeV3:
			router := e.cfg.SwapRouterPancakeV3
			if router == (common.Address{}) {
				return domaincontract.ExecutionPlan{}, nil, fmt.Errorf(
					"%w: pancakev3 hop[%d] requires swap_router_pancake_v3", ErrExecutionPlanUnavailable, i,
				)
			}
			pool := pools.PancakeV3[hop.PoolPancakeV3]
			if pool == nil {
				return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("pancakev3 pool %s not loaded", hop.PoolPancakeV3.Hex())
			}
			data, err := domaincontract.PackPancakeV3ExactInputSingle(domaincontract.ExactInputSingleParams{
				TokenIn:          hop.TokenIn,
				TokenOut:         hop.TokenOut,
				Fee:              big.NewInt(int64(pool.Fee)),
				Recipient:        executor,
				AmountIn:         amountIn,
				AmountOutMinimum: big.NewInt(0),
			})
			if err != nil {
				return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("encode pancakev3 hop[%d]: %w", i, err)
			}
			route := domaincontract.SwapRoute{RouterAddress: router, Data: data}
			if fillFromBalance {
				route.FillSource = domaincontract.FillSourceERC20Balance
				route.FillToken = hop.TokenIn
				route.PatchAmount = true
				route.FillOffset = domaincontract.PancakeV3ExactInputSingleAmountInOffset
			}
			routes = append(routes, route)
			addApproval(hop.TokenIn, router)

		case quoteunified.PoolVersionQuickSwapV3:
			return domaincontract.ExecutionPlan{}, nil, fmt.Errorf(
				"%w: quickswapv3 hop[%d] live calldata encoding is not supported yet",
				ErrExecutionPlanUnavailable, i,
			)

		case quoteunified.PoolVersionV4:
			pool := pools.V4[hop.PoolV4]
			if pool == nil {
				return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("v4 pool %s not loaded", hop.PoolV4.String())
			}
			zeroForOne := hop.TokenIn == pool.Key.Currency0
			if !zeroForOne && hop.TokenIn != pool.Key.Currency1 {
				return domaincontract.ExecutionPlan{}, nil, fmt.Errorf(
					"v4 hop[%d] tokenIn %s is not in pool key", i, hop.TokenIn.Hex(),
				)
			}
			poolKey := domaincontract.V4PoolKeyABI{
				Currency0:   pool.Key.Currency0,
				Currency1:   pool.Key.Currency1,
				Fee:         big.NewInt(int64(pool.Key.Fee)),
				TickSpacing: big.NewInt(int64(pool.Key.TickSpacing)),
				Hooks:       pool.Key.Hooks,
			}

			switch loan.Protocol {
			case domaincontract.FlashLoanProtocolUniswapV4:
				if e.cfg.PoolManager == (common.Address{}) {
					return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("pool manager address is not configured")
				}
				if fillFromBalance {
					return domaincontract.ExecutionPlan{}, nil, fmt.Errorf(
						"%w: univ4 hop[%d] under v4 flash requires a known amountIn (signed PoolManager.swap cannot use fillToken)",
						ErrExecutionPlanUnavailable, i,
					)
				}
				data, err := domaincontract.PackPoolManagerSwap(poolKey, zeroForOne, amountIn, nil)
				if err != nil {
					return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("encode v4 pool manager hop[%d]: %w", i, err)
				}
				routes = append(routes, domaincontract.SwapRoute{
					RouterAddress: e.cfg.PoolManager,
					Data:          data,
				})
				addSettle(pool.Key.Currency0)
				addSettle(pool.Key.Currency1)

			default:
				router := e.cfg.UniversalRouter
				if router == (common.Address{}) {
					return domaincontract.ExecutionPlan{}, nil, fmt.Errorf(
						"%w: univ4 hop[%d] requires universal_router when flash loan is not uniswapV4",
						ErrExecutionPlanUnavailable, i,
					)
				}
				urAmountIn := amountIn
				if fillFromBalance {
					urAmountIn = domaincontract.V4ActionOpenDelta
				}
				urData, err := domaincontract.PackUniversalRouterV4ExactInSingle(poolKey, zeroForOne, urAmountIn, big.NewInt(0), nil)
				if err != nil {
					return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("encode v4 universal router hop[%d]: %w", i, err)
				}
				if asset.IsNativeETH(hop.TokenIn) {
					// Native input: send ETH as msg.value; UR settles currency(0) from contract balance.
					urRoute := domaincontract.SwapRoute{
						RouterAddress: router,
						Data:          urData,
					}
					if fillFromBalance {
						urRoute.FillSource = domaincontract.FillSourceNativeBalance
						urRoute.AmountAsCallValue = true
					} else {
						urRoute.Value = amountIn
					}
					routes = append(routes, urRoute)
					break
				}
				// ERC20 input: transfer onto Universal Router, then SETTLE(CONTRACT_BALANCE)
				// so Permit2 is not required.
				transferData, err := domaincontract.PackERC20Transfer(router, amountIn)
				if err != nil {
					return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("encode v4 transfer hop[%d]: %w", i, err)
				}
				transferRoute := domaincontract.SwapRoute{
					RouterAddress: hop.TokenIn,
					Data:          transferData,
				}
				if fillFromBalance {
					transferRoute.FillSource = domaincontract.FillSourceERC20Balance
					transferRoute.FillToken = hop.TokenIn
					transferRoute.PatchAmount = true
					transferRoute.FillOffset = domaincontract.ERC20TransferAmountOffset
				}
				routes = append(routes, transferRoute)
				routes = append(routes, domaincontract.SwapRoute{
					RouterAddress: router,
					Data:          urData,
				})
			}

		case quoteunified.PoolVersionBalancer:
			pool := pools.Balancer[hop.PoolBalancer]
			if pool == nil {
				return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("balancer pool %s not loaded", hop.PoolBalancer.String())
			}
			vault := resolveBalancerVault(pool, e.cfg.BalancerVault, e.cfg.BalancerVaultV3)
			if isBalancerVaultV3(vault, e.cfg.BalancerVaultV3) {
				router := e.cfg.BalancerRouterV3
				if router == (common.Address{}) {
					return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("balancer v3 router address is not configured")
				}
				poolAddr := pool.Address
				if poolAddr == (common.Address{}) {
					idHash := pool.ID.Hash()
					poolAddr = common.BytesToAddress(idHash[12:])
				}
				if poolAddr == (common.Address{}) {
					return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("balancer v3 pool %s missing address", hop.PoolBalancer.String())
				}
				data, err := domaincontract.PackBalancerV3RouterSwapExactIn(domaincontract.BalancerV3RouterSwapParams{
					Pool:          poolAddr,
					TokenIn:       hop.TokenIn,
					TokenOut:      hop.TokenOut,
					ExactAmountIn: amountIn,
					MinAmountOut:  big.NewInt(0),
				})
				if err != nil {
					return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("encode balancer v3 hop[%d]: %w", i, err)
				}
				route := domaincontract.SwapRoute{
					RouterAddress: router,
					Data:          data,
				}
				if fillFromBalance {
					route.FillSource = domaincontract.FillSourceERC20Balance
					route.FillToken = hop.TokenIn
					route.PatchAmount = true
					route.FillOffset = domaincontract.BalancerV3SwapExactAmountInOffset
				}
				routes = append(routes, route)
				addApproval(hop.TokenIn, router)
				break
			}
			if vault == (common.Address{}) {
				return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("balancer vault address is not configured")
			}
			data, err := domaincontract.PackBalancerVaultSwap(domaincontract.BalancerVaultSwapParams{
				PoolID:    pool.ID.Hash(),
				AssetIn:   hop.TokenIn,
				AssetOut:  hop.TokenOut,
				Amount:    amountIn,
				Sender:    executor,
				Recipient: executor,
				Limit:     big.NewInt(0),
			})
			if err != nil {
				return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("encode balancer hop[%d]: %w", i, err)
			}
			route := domaincontract.SwapRoute{
				RouterAddress: vault,
				Data:          data,
			}
			if fillFromBalance {
				route.FillSource = domaincontract.FillSourceERC20Balance
				route.FillToken = hop.TokenIn
				route.PatchAmount = true
				route.FillOffset = domaincontract.BalancerSwapAmountOffset
			}
			routes = append(routes, route)
			addApproval(hop.TokenIn, vault)

		default:
			return domaincontract.ExecutionPlan{}, nil, fmt.Errorf("unsupported hop version %s", hop.Version)
		}
	}

	if loan.Protocol == domaincontract.FlashLoanProtocolUniswapV4 {
		addSettle(loan.Token)
	}

	profitToken := opportunity.Route.TokenIn
	if profitToken == (common.Address{}) {
		profitToken = loan.Token
	}
	minProfit := cloneBigIntOrZero(opportunity.NetProfit)

	return domaincontract.ExecutionPlan{
		Loan:             loan,
		Routes:           routes,
		SettleCurrencies: settle,
		ProfitToken:      profitToken,
		MinProfit:        minProfit,
	}, approvals, nil
}

func maxUint256() *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
}

// NewRepositoryRoutePoolLoader builds a RoutePoolLoader from protocol repositories.
func NewRepositoryRoutePoolLoader(
	univ3Pools marketuniv3.PoolRepository,
	pancakePools marketpancake.PoolRepository,
	quickSwapPools marketquick.PoolRepository,
	univ4Pools marketuniv4.PoolRepository,
	balancerPools marketbalancer.PoolRepository,
) RoutePoolLoader {
	return routePoolLoaderFunc(func(ctx context.Context, route quoteunified.Route) (quoteunified.RoutePools, error) {
		pools := quoteunified.RoutePools{
			V3:          make(map[common.Address]*marketuniv3.Pool),
			PancakeV3:   make(map[common.Address]*marketpancake.Pool),
			QuickSwapV3: make(map[common.Address]*marketquick.Pool),
			V4:          make(map[marketuniv4.PoolID]*marketuniv4.Pool),
			Balancer:    make(map[marketbalancer.PoolID]*marketbalancer.Pool),
		}
		for _, hop := range route.Hops {
			switch hop.Version {
			case quoteunified.PoolVersionV3:
				if _, ok := pools.V3[hop.PoolV3]; ok {
					continue
				}
				if univ3Pools == nil {
					return quoteunified.RoutePools{}, fmt.Errorf("v3 pool repository is nil")
				}
				pool, err := univ3Pools.Get(ctx, hop.PoolV3)
				if err != nil {
					return quoteunified.RoutePools{}, fmt.Errorf("load v3 pool %s: %w", hop.PoolV3.Hex(), err)
				}
				if pool == nil {
					return quoteunified.RoutePools{}, fmt.Errorf("v3 pool %s not found", hop.PoolV3.Hex())
				}
				pools.V3[hop.PoolV3] = pool
			case quoteunified.PoolVersionPancakeV3:
				if _, ok := pools.PancakeV3[hop.PoolPancakeV3]; ok {
					continue
				}
				if pancakePools == nil {
					return quoteunified.RoutePools{}, fmt.Errorf("pancakev3 pool repository is nil")
				}
				pool, err := pancakePools.Get(ctx, hop.PoolPancakeV3)
				if err != nil {
					return quoteunified.RoutePools{}, fmt.Errorf("load pancakev3 pool %s: %w", hop.PoolPancakeV3.Hex(), err)
				}
				if pool == nil {
					return quoteunified.RoutePools{}, fmt.Errorf("pancakev3 pool %s not found", hop.PoolPancakeV3.Hex())
				}
				pools.PancakeV3[hop.PoolPancakeV3] = pool
			case quoteunified.PoolVersionQuickSwapV3:
				if _, ok := pools.QuickSwapV3[hop.PoolQuickSwapV3]; ok {
					continue
				}
				if quickSwapPools == nil {
					return quoteunified.RoutePools{}, fmt.Errorf("quickswapv3 pool repository is nil")
				}
				pool, err := quickSwapPools.Get(ctx, hop.PoolQuickSwapV3)
				if err != nil {
					return quoteunified.RoutePools{}, fmt.Errorf("load quickswapv3 pool %s: %w", hop.PoolQuickSwapV3.Hex(), err)
				}
				if pool == nil {
					return quoteunified.RoutePools{}, fmt.Errorf("quickswapv3 pool %s not found", hop.PoolQuickSwapV3.Hex())
				}
				pools.QuickSwapV3[hop.PoolQuickSwapV3] = pool
			case quoteunified.PoolVersionV4:
				if _, ok := pools.V4[hop.PoolV4]; ok {
					continue
				}
				if univ4Pools == nil {
					return quoteunified.RoutePools{}, fmt.Errorf("v4 pool repository is nil")
				}
				pool, err := univ4Pools.Get(ctx, hop.PoolV4)
				if err != nil {
					return quoteunified.RoutePools{}, fmt.Errorf("load v4 pool %s: %w", hop.PoolV4.String(), err)
				}
				if pool == nil {
					return quoteunified.RoutePools{}, fmt.Errorf("v4 pool %s not found", hop.PoolV4.String())
				}
				pools.V4[hop.PoolV4] = pool
			case quoteunified.PoolVersionBalancer:
				if _, ok := pools.Balancer[hop.PoolBalancer]; ok {
					continue
				}
				if balancerPools == nil {
					return quoteunified.RoutePools{}, fmt.Errorf("balancer pool repository is nil")
				}
				pool, err := balancerPools.Get(ctx, hop.PoolBalancer)
				if err != nil {
					return quoteunified.RoutePools{}, fmt.Errorf("load balancer pool %s: %w", hop.PoolBalancer.String(), err)
				}
				if pool == nil {
					return quoteunified.RoutePools{}, fmt.Errorf("balancer pool %s not found", hop.PoolBalancer.String())
				}
				pools.Balancer[hop.PoolBalancer] = pool
			case quoteunified.PoolVersionWrapWETH, quoteunified.PoolVersionUnwrapWETH:
				continue
			default:
				return quoteunified.RoutePools{}, fmt.Errorf("unsupported pool version %d", hop.Version)
			}
		}
		return pools, nil
	})
}

func isBalancerVaultV3(vault, vaultV3 common.Address) bool {
	return vaultV3 != (common.Address{}) && vault == vaultV3
}

func resolveBalancerVault(pool *marketbalancer.Pool, vaultV2, vaultV3 common.Address) common.Address {
	if pool == nil {
		return vaultV2
	}
	if pool.Vault != (common.Address{}) {
		return pool.Vault
	}
	// V3 pool ids are the pool address left-padded into bytes32.
	if vaultV3 != (common.Address{}) && looksLikeBalancerV3Pool(pool) {
		return vaultV3
	}
	return vaultV2
}

func looksLikeBalancerV3Pool(pool *marketbalancer.Pool) bool {
	if pool == nil || pool.Address == (common.Address{}) {
		return false
	}
	idHash := pool.ID.Hash()
	return common.BytesToAddress(idHash[12:]) == pool.Address
}

func resolveUniv3BorrowToken0(loan domaincontract.FlashLoan, pools quoteunified.RoutePools) domaincontract.FlashLoan {
	if loan.Protocol != domaincontract.FlashLoanProtocolUniswapV3 || loan.Lender == (common.Address{}) {
		return loan
	}
	pool := pools.V3[loan.Lender]
	if pool == nil {
		return loan
	}
	loan.BorrowToken0 = pool.Token0 == loan.Token
	return loan
}
