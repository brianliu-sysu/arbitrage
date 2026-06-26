// Package pool 定义 Uniswap V3 池子状态及核心操作。
package pool

import (
	"math"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// TickLiquidity 单个 tick 上的流动性净额。
//
// liquidityNet 的含义：
//   - 正值：tickLower → 穿过该 tick 向右移动时流动性增加
//   - 负值：tickUpper → 穿过该 tick 向右移动时流动性减少
//
// 通过 Mint/Burn 事件维护。
type TickLiquidity struct {
	LiquidityNet *big.Int // 该 tick 上的流动性净额
}

// PoolState 保存 Uniswap V3 池子的当前运行时状态。
type PoolState struct {
	mu sync.RWMutex

	Address      common.Address // 池子合约地址
	Token0       common.Address // Token0 地址（数值较小者为 token0）
	Token1       common.Address // Token1 地址
	Token0Symbol string         // Token0 符号，如 "USDC"
	Token1Symbol string         // Token1 符号，如 "WETH"
	Fee          uint32         // 手续费率，以 1e-6 为单位
	Token0Decimals int          // Token0 小数位数，默认 18
	Token1Decimals int          // Token1 小数位数，默认 18

	// ---- 核心状态字段（通过事件更新）----
	SqrtPriceX96 *big.Int // sqrt(token1/token0) * 2^96
	Tick         int32    // 当前 tick
	Liquidity    *big.Int // 当前活跃流动性 L

	// ---- 所有有流动性的 tick 的流动性净额（通过 Mint/Burn 事件维护）----
	Ticks map[int32]*TickLiquidity

	// ---- 派生价格（人类可读）----
	Price0In1 float64 // token0 以 token1 计价的现货价格
	Price1In0 float64 // token1 以 token0 计价的现货价格

	BlockNumber uint64 // 最后一次更新的区块号
}

// NewPoolState 创建一个尚未初始化的池子状态。
func NewPoolState(address, token0, token1 common.Address, fee uint32) *PoolState {
	return &PoolState{
		Address:      address,
		Token0:       token0,
		Token1:       token1,
		Fee:          fee,
		SqrtPriceX96: new(big.Int),
		Liquidity:    new(big.Int),
		Ticks:        make(map[int32]*TickLiquidity),
	}
}

// SetTokens 设置池子的代币地址、手续费率并推断小数位数。
func (p *PoolState) SetTokens(token0, token1 common.Address, fee uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Token0 = token0
	p.Token1 = token1
	p.Fee = fee
	p.Token0Decimals = guessDecimals(token0)
	p.Token1Decimals = guessDecimals(token1)
	p.Token0Symbol = guessSymbol(token0)
	p.Token1Symbol = guessSymbol(token1)
}

// guessDecimals 根据已知地址推断代币小数位数，未知代币默认 18。
func guessDecimals(addr common.Address) int {
	switch addr.Hex() {
	// 6 decimals
	case "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48": // USDC
		return 6
	case "0xdAC17F958D2ee523a2206206994597C13D831ec7": // USDT
		return 6
	case "0xaf88d065e77c8cC2239327C5EDb3A432268e5831": // USDC (Arbitrum)
		return 6
	case "0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9": // USDT (Arbitrum)
		return 6
	case "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913": // USDC (Base)
		return 6
	// 8 decimals
	case "0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599": // WBTC
		return 8
	case "0x2f2a2543B76A4166549F7aaB2e75Bef0aefC5B0f": // WBTC (Arbitrum)
		return 8
	default:
		return 18 // 绝大多数 ERC20 都是 18 位
	}
}

// guessSymbol 根据已知地址推断代币符号，未知返回地址前 6 位。
func guessSymbol(addr common.Address) string {
	switch addr.Hex() {
	// ---- Ethereum Mainnet ----
	case "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2":
		return "WETH"
	case "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48":
		return "USDC"
	case "0xdAC17F958D2ee523a2206206994597C13D831ec7":
		return "USDT"
	case "0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599":
		return "WBTC"
	case "0x6B175474E89094C44Da98b954EedeAC495271d0F":
		return "DAI"
	case "0x514910771AF9Ca656af840dff83E8264EcF986CA":
		return "LINK"
	case "0x7D1AfA7B718fb893dB30A3aBc0Cfc608AaCfeBB0":
		return "MATIC"
	case "0x1f9840a85d5aF5bf1D1762F925BDADdC4201F984":
		return "UNI"
	case "0x7Fc66500c84A76Ad7e9c93437bFc5Ac33E2DDaE9":
		return "AAVE"
	case "0x0d8775F648430679A709E98d2b0Cb6250d2887EF":
		return "BAT"
	case "0x9f8F72aA9304c8B593d555F12eF6589cC3A579A2":
		return "MKR"
	case "0x111111111117dC0aa78b770fA6A738034120C302":
		return "1INCH"
	case "0x95aD61b0a150d79219dCF64E1E6Cc01f0B64C4cE":
		return "SHIB"
	case "0xfAbA6f8e4a5E8Ab82F62fe7C39859FA577269BE3":
		return "ONDO"
	case "0x6982508145454Ce325dDbE47a25d4ec3d2311933":
		return "PEPE"
	case "0xc00e94Cb662C3520282E6f5717214004A7f26888":
		return "COMP"
	case "0x0F5D2fB29fb7d3CFeE444a200298f468908cC942":
		return "MANA"
	case "0x4d224452801ACEd8B2F0aebE155379bb5D594381":
		return "APE"
	case "0xd533a949740bb3306d119CC777fa900bA034cd52":
		return "CRV"
	case "0xae7ab96520DE3A18E5e111B5EaAb095312D7fE84":
		return "stETH"
	case "0xBe9895146f7af43049ca1c1AE358B0541Ea49704":
		return "cbETH"
	case "0x5A98FcBEA516Cf06857215779Fd812CA3beF1B32":
		return "LDO"
	case "0x5283D291DBCF85356A21bA090E6db59121208b44":
		return "BLUR"
	// ---- Arbitrum ----
	case "0x82aF49447D8a07e3bd95BD0d56f35241523fBab1":
		return "WETH"
	case "0xaf88d065e77c8cC2239327C5EDb3A432268e5831":
		return "USDC"
	case "0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9":
		return "USDT"
	case "0x912CE59144191C1204E64559FE8253a0e49E6548":
		return "ARB"
	// ---- Base ----
	case "0x4200000000000000000000000000000000000006":
		return "WETH"
	case "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913":
		return "USDC"
	// ---- Optimism ----
	case "0x4200000000000000000000000000000000000042":
		return "OP"
	case "0x0b2C639c533813f4Aa9D7837CAf62653d097Ff85":
		return "USDC"
	case "0x94b008aA00579c1307B0EF2c499aD98a8ce58e58":
		return "USDT"
	default:
		// 返回地址缩写: "0xa0b8..."
		h := addr.Hex()
		if len(h) >= 10 {
			return h[:6] + ".." + h[len(h)-4:]
		}
		return h
	}
}

// UpdateFromSwap 根据 Swap 事件更新状态。
// Swap 事件包含完整的 sqrtPriceX96 / tick / liquidity，是最理想的状态更新来源。
func (p *PoolState) UpdateFromSwap(sqrtPriceX96 *big.Int, tick int32, liquidity *big.Int, blockNumber uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.SqrtPriceX96.Set(sqrtPriceX96)
	p.Liquidity.Set(liquidity)
	p.Tick = tick
	p.BlockNumber = blockNumber

	p.recalcPrices()
}

// UpdateFromMint 已弃用，请使用 UpdateTickFromMint。
// 保留用于向后兼容。
func (p *PoolState) UpdateFromMint(newLiquidity *big.Int, blockNumber uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Liquidity.Set(newLiquidity)
	p.BlockNumber = blockNumber
}

// UpdateFromBurn 已弃用，请使用 UpdateTickFromBurn。
// 保留用于向后兼容。
func (p *PoolState) UpdateFromBurn(newLiquidity *big.Int, blockNumber uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Liquidity.Set(newLiquidity)
	p.BlockNumber = blockNumber
}

// UpdateTickFromMint 根据 Mint 事件更新 tick 级的流动性地图。
//
// tickLower → liquidityNet +amount
// tickUpper → liquidityNet -amount
func (p *PoolState) UpdateTickFromMint(tickLower, tickUpper int32, amount *big.Int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.addTickLiquidity(tickLower, new(big.Int).Set(amount))       // +amount
	p.addTickLiquidity(tickUpper, new(big.Int).Neg(amount))        // -amount
}

// UpdateTickFromBurn 根据 Burn 事件更新 tick 级的流动性地图。
//
// 与 Mint 方向相反：
// tickLower → liquidityNet -amount
// tickUpper → liquidityNet +amount
func (p *PoolState) UpdateTickFromBurn(tickLower, tickUpper int32, amount *big.Int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.addTickLiquidity(tickLower, new(big.Int).Neg(amount)) // -amount
	p.addTickLiquidity(tickUpper, new(big.Int).Set(amount))  // +amount
}

// addTickLiquidity 更新指定 tick 上的流动性净额（需持有写锁）。
func (p *PoolState) addTickLiquidity(tick int32, delta *big.Int) {
	tl, ok := p.Ticks[tick]
	if !ok {
		tl = &TickLiquidity{LiquidityNet: new(big.Int)}
		p.Ticks[tick] = tl
	}
	tl.LiquidityNet.Add(tl.LiquidityNet, delta)

	// 如果净额归零，删除该 tick 条目以节省内存
	if tl.LiquidityNet.Sign() == 0 {
		delete(p.Ticks, tick)
	}
}

// SetTickLiquidity 直接设置指定 tick 上的流动性净额（覆盖已有值）。
// 用于从链上 Tick Bitmap 重建 tick 地图时使用。
func (p *PoolState) SetTickLiquidity(tick int32, liquidityNet *big.Int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if liquidityNet.Sign() == 0 {
		return
	}
	p.Ticks[tick] = &TickLiquidity{LiquidityNet: new(big.Int).Set(liquidityNet)}
}

// ClearTicks 清空所有 tick 流动性记录。
func (p *PoolState) ClearTicks() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Ticks = make(map[int32]*TickLiquidity)
}

const (
	// TickMin / TickMax — Uniswap V3 tick 的合法范围。
	TickMin = int32(-887272)
	TickMax = int32(887272)
)

// GetTickLiquidity 获取指定 tick 上的流动性净额。
// 如果该 tick 上无流动性，返回 0。
func (p *PoolState) GetTickLiquidity(tick int32) *big.Int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if tl, ok := p.Ticks[tick]; ok {
		return new(big.Int).Set(tl.LiquidityNet)
	}
	return big.NewInt(0)
}

// GetTickCount 返回当前有流动性记录的 tick 数量。
func (p *PoolState) GetTickCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.Ticks)
}

// GetTicksCopy 返回所有 tick 流动性数据的深拷贝。
func (p *PoolState) GetTicksCopy() map[int32]*TickLiquidity {
	p.mu.RLock()
	defer p.mu.RUnlock()

	cp := make(map[int32]*TickLiquidity, len(p.Ticks))
	for k, v := range p.Ticks {
		cp[k] = &TickLiquidity{LiquidityNet: new(big.Int).Set(v.LiquidityNet)}
	}
	return cp
}

// recalcPrices 根据 sqrtPriceX96 计算人类可读价格（需持有写锁）。
//
// sqrtPriceX96 = sqrt(price) * 2^96
// => price = (sqrtPriceX96 / 2^96)^2
//
// 其中 price = token1 / token0，因此:
//
//	price0In1 = price       （1 token0 值多少 token1）
//	price1In0 = 1 / price   （1 token1 值多少 token0）
func (p *PoolState) recalcPrices() {
	if p.SqrtPriceX96 == nil || p.SqrtPriceX96.Sign() == 0 {
		return
	}

	q := new(big.Float).SetInt(p.SqrtPriceX96)
	q.Quo(q, new(big.Float).SetFloat64(79228162514264337593543950336.0)) // 2^96
	q.Mul(q, q) // square to get price

	price0In1, _ := q.Float64()
	p.Price0In1 = price0In1
	if price0In1 > 0 {
		p.Price1In0 = 1.0 / price0In1
	}
}

// TickToHumanPrice 将 tick 转为人类可读价格。
// 返回 1 token1 以 token0 计价的实际价格（含小数位调整）。
//
// 例：ETH/USDC 池 (token0=USDC 6dec, token1=WETH 18dec)
//
//	tick=202669 → 1 ETH = 10^(18-6) / 1.0001^202669 ≈ 1580 USDC
func (p *PoolState) HumanPrice() float64 {
	if p.Tick == 0 {
		return 0
	}
	rawPrice := math.Pow(1.0001, float64(p.Tick))
	// 1 token1 = 10^(decimals1 - decimals0) / rawPrice token0
	return math.Pow10(p.Token1Decimals-p.Token0Decimals) / rawPrice
}

// GetPrices 线程安全地读取当前现货价格。
func (p *PoolState) GetPrices() (price0In1, price1In0 float64, tick int32, blockNum uint64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Price0In1, p.Price1In0, p.Tick, p.BlockNumber
}

// GetRawState 线程安全地读取原始链上状态字段，用于健康检查比价。
// 返回 sqrtPriceX96 的副本、tick、liquidity 的副本、blockNumber。
func (p *PoolState) GetRawState() (sqrtPriceX96 *big.Int, tick int32, liquidity *big.Int, blockNum uint64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return new(big.Int).Set(p.SqrtPriceX96), p.Tick, new(big.Int).Set(p.Liquidity), p.BlockNumber
}

// GetStateCopy 获取状态的深拷贝，避免外部后续修改影响内部状态。
func (p *PoolState) GetStateCopy() *PoolState {
	p.mu.RLock()
	defer p.mu.RUnlock()

	cp := &PoolState{
		Address:        p.Address,
		Token0:         p.Token0,
		Token1:         p.Token1,
		Token0Symbol:   p.Token0Symbol,
		Token1Symbol:   p.Token1Symbol,
		Fee:            p.Fee,
		Token0Decimals: p.Token0Decimals,
		Token1Decimals: p.Token1Decimals,
		SqrtPriceX96:   new(big.Int).Set(p.SqrtPriceX96),
		Tick:           p.Tick,
		Liquidity:      new(big.Int).Set(p.Liquidity),
		Price0In1:      p.Price0In1,
		Price1In0:      p.Price1In0,
		BlockNumber:    p.BlockNumber,
		Ticks:          p.GetTicksCopy(),
	}
	return cp
}
