// Package pool 定义 Uniswap V3 池子状态及核心操作。
package pool

import (
	"fmt"
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

	Address        common.Address // 池子合约地址
	Token0         common.Address // Token0 地址（数值较小者为 token0）
	Token1         common.Address // Token1 地址
	Token0Symbol   string         // Token0 符号，如 "USDC"
	Token1Symbol   string         // Token1 符号，如 "WETH"
	Fee            uint32         // 手续费率，以 1e-6 为单位
	Token0Decimals int            // Token0 小数位数，默认 18
	Token1Decimals int            // Token1 小数位数，默认 18

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

// TokenInfo 代币元信息。
type TokenInfo struct {
	Symbol   string
	Decimals int
}

var q96 = new(big.Int).Lsh(big.NewInt(1), 96)

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

// SetTokens 设置池子的代币地址和手续费率（代币元信息留空，后续由缓存填充）。
func (p *PoolState) SetTokens(token0, token1 common.Address, fee uint32) {
	p.SetTokensWithInfo(token0, token1, fee, nil, nil)
}

// SetTokensWithInfo 设置池子的代币地址、手续费率和代币元信息。
// 当 token info 为空时，使用合理默认值（decimals=18，symbol 取地址缩写）。
func (p *PoolState) SetTokensWithInfo(token0, token1 common.Address, fee uint32, token0Info, token1Info *TokenInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Token0 = token0
	p.Token1 = token1
	p.Fee = fee

	if token0Info != nil {
		p.Token0Decimals = token0Info.Decimals
		p.Token0Symbol = token0Info.Symbol
	} else {
		p.Token0Decimals = 18
		p.Token0Symbol = shortAddr(token0)
	}
	if token1Info != nil {
		p.Token1Decimals = token1Info.Decimals
		p.Token1Symbol = token1Info.Symbol
	} else {
		p.Token1Decimals = 18
		p.Token1Symbol = shortAddr(token1)
	}
}

// shortAddr 返回地址的缩写形式（如 "0xa0b8..c599"）。
func shortAddr(addr common.Address) string {
	h := addr.Hex()
	if len(h) >= 10 {
		return h[:6] + ".." + h[len(h)-4:]
	}
	return h
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

// UpdateTickFromMint 根据 Mint 事件更新 tick 级的流动性地图。
//
// tickLower → liquidityNet +amount
// tickUpper → liquidityNet -amount
// 如果当前 tick 位于 [tickLower, tickUpper)，该头寸立即成为活跃流动性。
func (p *PoolState) UpdateTickFromMint(tickLower, tickUpper int32, amount *big.Int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.addTickLiquidity(tickLower, new(big.Int).Set(amount)) // +amount
	p.addTickLiquidity(tickUpper, new(big.Int).Neg(amount)) // -amount
	if p.tickInRange(tickLower, tickUpper) {
		p.Liquidity.Add(p.Liquidity, amount)
	}
}

// UpdateTickFromBurn 根据 Burn 事件更新 tick 级的流动性地图。
//
// 与 Mint 方向相反：
// tickLower → liquidityNet -amount
// tickUpper → liquidityNet +amount
// 如果当前 tick 位于 [tickLower, tickUpper)，该头寸从活跃流动性中移除。
func (p *PoolState) UpdateTickFromBurn(tickLower, tickUpper int32, amount *big.Int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.addTickLiquidity(tickLower, new(big.Int).Neg(amount)) // -amount
	p.addTickLiquidity(tickUpper, new(big.Int).Set(amount)) // +amount
	if p.tickInRange(tickLower, tickUpper) {
		p.Liquidity.Sub(p.Liquidity, amount)
	}
}

// tickInRange 判断当前 tick 是否落在 Uniswap V3 头寸区间 [lower, upper)。
// 需在持有锁时调用。
func (p *PoolState) tickInRange(tickLower, tickUpper int32) bool {
	return p.Tick >= tickLower && p.Tick < tickUpper
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

// ReplaceTicks 原子替换所有 tick 流动性地图。
// 先构建好 newTicks，一次性替换，避免 ClearTicks + 逐个 SetTickLiquidity 之间的竞态窗口。
func (p *PoolState) ReplaceTicks(newTicks map[int32]*TickLiquidity) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Ticks = newTicks
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
	q.Mul(q, q)                                                          // square to get price

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

// QuoteExactInput 使用当前内存状态本地模拟 exact-input 报价。
//
// 该实现按当前活跃流动性区间计算，不访问 RPC。对于不会跨 initialized tick
// 的常见小额报价，它与 Uniswap V3 的核心公式一致；大额跨 tick 报价后续可在
// 此基础上扩展为逐 tick 模拟。
func (p *PoolState) QuoteExactInput(amountIn *big.Int, tokenIn common.Address) (*big.Int, error) {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return nil, fmt.Errorf("amountIn must be positive")
	}

	p.mu.RLock()
	token0 := p.Token0
	token1 := p.Token1
	fee := p.Fee
	sqrtP := new(big.Int).Set(p.SqrtPriceX96)
	liquidity := new(big.Int).Set(p.Liquidity)
	p.mu.RUnlock()

	if tokenIn != token0 && tokenIn != token1 {
		return nil, fmt.Errorf("tokenIn %s is not token0 or token1", tokenIn.Hex())
	}
	if sqrtP.Sign() <= 0 {
		return nil, fmt.Errorf("pool sqrtPriceX96 is not initialized")
	}
	if liquidity.Sign() <= 0 {
		return nil, fmt.Errorf("pool liquidity is not initialized")
	}
	if fee >= 1_000_000 {
		return nil, fmt.Errorf("invalid pool fee %d", fee)
	}

	amountLessFee := new(big.Int).Mul(amountIn, big.NewInt(int64(1_000_000-fee)))
	amountLessFee.Div(amountLessFee, big.NewInt(1_000_000))
	if amountLessFee.Sign() == 0 {
		return big.NewInt(0), nil
	}

	if tokenIn == token0 {
		return quoteToken0ForToken1(amountLessFee, sqrtP, liquidity), nil
	}
	return quoteToken1ForToken0(amountLessFee, sqrtP, liquidity), nil
}

// token0 -> token1 exact input within current liquidity range.
func quoteToken0ForToken1(amount0In, sqrtP, liquidity *big.Int) *big.Int {
	// sqrtQ = L * sqrtP * Q96 / (L * Q96 + amount0In * sqrtP)
	numerator := new(big.Int).Mul(liquidity, sqrtP)
	numerator.Mul(numerator, q96)

	denominator := new(big.Int).Mul(liquidity, q96)
	denominator.Add(denominator, new(big.Int).Mul(amount0In, sqrtP))
	if denominator.Sign() == 0 {
		return big.NewInt(0)
	}
	sqrtQ := new(big.Int).Div(numerator, denominator)
	if sqrtQ.Cmp(sqrtP) >= 0 {
		return big.NewInt(0)
	}

	// amount1Out = L * (sqrtP - sqrtQ) / Q96
	delta := new(big.Int).Sub(sqrtP, sqrtQ)
	out := new(big.Int).Mul(liquidity, delta)
	out.Div(out, q96)
	return out
}

// token1 -> token0 exact input within current liquidity range.
func quoteToken1ForToken0(amount1In, sqrtP, liquidity *big.Int) *big.Int {
	// sqrtQ = sqrtP + amount1In * Q96 / L
	delta := new(big.Int).Mul(amount1In, q96)
	delta.Div(delta, liquidity)
	sqrtQ := new(big.Int).Add(sqrtP, delta)
	if sqrtQ.Cmp(sqrtP) <= 0 {
		return big.NewInt(0)
	}

	// amount0Out = L * (sqrtQ - sqrtP) * Q96 / (sqrtQ * sqrtP)
	numerator := new(big.Int).Sub(sqrtQ, sqrtP)
	numerator.Mul(numerator, liquidity)
	numerator.Mul(numerator, q96)

	denominator := new(big.Int).Mul(sqrtQ, sqrtP)
	if denominator.Sign() == 0 {
		return big.NewInt(0)
	}
	out := new(big.Int).Div(numerator, denominator)
	return out
}
