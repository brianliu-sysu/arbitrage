// Package pool 定义 Uniswap V3 池子状态及核心操作。
package pool

import (
	"math/big"
	"sync"

	"github.com/brianliu-sysu/arbitrage/internal/utils"
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
	LiquidityNet   *big.Int // 该 tick 上的流动性净额
	LiquidityGross *big.Int // 该 tick 上的流动性
}

// State 保存 Uniswap V3 池子的当前运行时状态。
type State struct {
	mu sync.RWMutex

	Address      common.Address // 池子合约地址
	Token0       common.Address // Token0 地址（数值较小者为 token0）
	Token1       common.Address // Token1 地址
	Fee          uint32         // 手续费率，以 1e-6 为单位
	TickSpacing  int32          // tick spacing
	Token0Symbol string
	Token1Symbol string

	// ---- 核心状态字段（通过事件更新）----
	SqrtPriceX96 *big.Int // sqrt(token1/token0) * 2^96
	Tick         int32    // 当前 tick
	Liquidity    *big.Int // 当前活跃流动性 L

	// ---- 所有有流动性的 tick 的流动性净额（通过 Mint/Burn 事件维护）----
	Ticks  map[int32]*TickLiquidity
	Bitmap map[uint16]utils.Word256

	BlockNumber uint64
}

// PoolState 兼容历史命名，避免外部调用方一次性改动。
type PoolState = State

// TokenInfo 代币元信息。
type TokenInfo struct {
	Symbol   string
	Decimals int
}

// NewPoolState 创建一个尚未初始化的池子状态。
func NewState(address, token0, token1 common.Address, fee uint32) *State {
	return &State{
		Address:      address,
		Token0:       token0,
		Token1:       token1,
		Fee:          fee,
		TickSpacing:  1,
		SqrtPriceX96: new(big.Int),
		Liquidity:    new(big.Int),
		Ticks:        make(map[int32]*TickLiquidity),
		Bitmap:       make(map[uint16]utils.Word256),
	}
}

// NewPoolState 兼容历史命名，等价于 NewState。
func NewPoolState(address, token0, token1 common.Address, fee uint32) *State {
	return NewState(address, token0, token1, fee)
}

// SetTokens 设置池子的代币地址和手续费率（代币元信息留空，后续由缓存填充）。
func (p *State) SetTokens(token0, token1 common.Address, fee uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Token0 = token0
	p.Token1 = token1
	p.Fee = fee
}

// UpdateFromSwap 根据 Swap 事件更新状态。
// Swap 事件包含完整的 sqrtPriceX96 / tick / liquidity，是最理想的状态更新来源。
func (p *State) UpdateFromSwap(sqrtPriceX96 *big.Int, tick int32, liquidity *big.Int, blockNumber uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.SqrtPriceX96.Set(sqrtPriceX96)
	p.Liquidity.Set(liquidity)
	p.Tick = tick

	p.BlockNumber = blockNumber
}

// UpdateTickFromMint 根据 Mint 事件更新 tick 级的流动性地图。
//
// tickLower → liquidityNet +amount
// tickUpper → liquidityNet -amount
// 如果当前 tick 位于 [tickLower, tickUpper)，该头寸立即成为活跃流动性。
func (p *State) UpdateTickFromMint(tickLower, tickUpper int32, amount *big.Int, blockNumber ...uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.updateTickAndBitmap(tickLower, new(big.Int).Set(amount), false, true) // +amount
	p.updateTickAndBitmap(tickUpper, new(big.Int).Set(amount), true, true)  // -amount
	if p.tickInRange(tickLower, tickUpper) {
		p.Liquidity.Add(p.Liquidity, amount)
	}

	if len(blockNumber) > 0 {
		p.BlockNumber = blockNumber[0]
	}
}

// UpdateTickFromBurn 根据 Burn 事件更新 tick 级的流动性地图。
//
// 与 Mint 方向相反：
// tickLower → liquidityNet -amount
// tickUpper → liquidityNet +amount
// 如果当前 tick 位于 [tickLower, tickUpper)，该头寸从活跃流动性中移除。
func (p *State) UpdateTickFromBurn(tickLower, tickUpper int32, amount *big.Int, blockNumber ...uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.updateTickAndBitmap(tickLower, new(big.Int).Set(amount), false, false) // -amount
	p.updateTickAndBitmap(tickUpper, new(big.Int).Set(amount), true, false)  // +amount
	if p.tickInRange(tickLower, tickUpper) {
		p.Liquidity.Sub(p.Liquidity, amount)
	}

	if len(blockNumber) > 0 {
		p.BlockNumber = blockNumber[0]
	}
}

// tickInRange 判断当前 tick 是否落在 Uniswap V3 头寸区间 [lower, upper)。
// 需在持有锁时调用。
func (p *State) tickInRange(tickLower, tickUpper int32) bool {
	return p.Tick >= tickLower && p.Tick < tickUpper
}

// addTickLiquidity 更新指定 tick 上的流动性净额（需持有写锁）。
func (p *State) updateTickAndBitmap(tick int32, delta *big.Int, isUpper, isMint bool) {
	tl, ok := p.Ticks[tick]
	if !ok {
		if !isMint {
			return
		}
		tl = &TickLiquidity{LiquidityNet: new(big.Int), LiquidityGross: new(big.Int)}
		p.Ticks[tick] = tl
	}
	if tl.LiquidityNet == nil {
		tl.LiquidityNet = new(big.Int)
	}
	if tl.LiquidityGross == nil {
		// 历史快照可能只有 LiquidityNet，缺少 LiquidityGross；先做保守恢复避免空指针。
		tl.LiquidityGross = new(big.Int).Abs(tl.LiquidityNet)
	}

	liquidityGrossBefore := new(big.Int).Set(tl.LiquidityGross)
	if isMint {
		tl.LiquidityGross.Add(tl.LiquidityGross, delta)
	} else {
		tl.LiquidityGross.Sub(tl.LiquidityGross, delta)
		if tl.LiquidityGross.Cmp(big.NewInt(0)) <= 0 {
			tl.LiquidityGross = big.NewInt(0)
		}
	}

	if isMint {
		if isUpper {
			tl.LiquidityNet.Sub(tl.LiquidityNet, delta)
		} else {
			tl.LiquidityNet.Add(tl.LiquidityNet, delta)
		}
	} else {
		if isUpper {
			tl.LiquidityNet.Add(tl.LiquidityNet, delta)
		} else {
			tl.LiquidityNet.Sub(tl.LiquidityNet, delta)
		}
	}

	// 3. 联动更新 Bitmap 位图索引与内存垃圾回收 (GC)
	if isMint && liquidityGrossBefore.Sign() == 0 && tl.LiquidityGross.Sign() > 0 {
		// Mint 场景：流动性从无到有 (0 -> >0)，将位图对应的 0 翻转为 1
		p.flipBitmapBit(tick)
	} else if !isMint && liquidityGrossBefore.Sign() > 0 && tl.LiquidityGross.Sign() == 0 {
		// Burn 场景：流动性被完全抽干 (>0 -> 0)，将位图对应的 1 翻转为 0
		p.flipBitmapBit(tick)

		// 内存清理：彻底移除僵尸 Tick，防止 Map 随着时间推移无限制膨胀
		delete(p.Ticks, tick)
	}
}

func (p *State) flipBitmapBit(tick int32) {
	compressed := tick / p.TickSpacing
	if tick < 0 && tick%p.TickSpacing != 0 {
		compressed--
	}

	wordIndex := uint16(compressed >> 8)
	bitIndex := uint8(compressed & 0xFF)

	word64Idx := bitIndex / 64
	bit64Shift := bitIndex % 64

	word := p.Bitmap[wordIndex]
	word[word64Idx] ^= uint64(1) << bit64Shift // 巧妙利用异或实现双向翻转
	p.Bitmap[wordIndex] = word
}

// ClearTicks 清空所有 tick 流动性记录。
func (p *State) ClearTicks() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Ticks = make(map[int32]*TickLiquidity)
	p.Bitmap = make(map[uint16]utils.Word256)
}

// ReplaceTicks 原子替换所有 tick 流动性地图。
// 先构建好 newTicks，一次性替换，避免 ClearTicks + 逐个 SetTickLiquidity 之间的竞态窗口。
func (p *State) ReplaceTicks(newTicks map[int32]*TickLiquidity) {
	bitmap := make(map[uint16]utils.Word256)
	normalized := make(map[int32]*TickLiquidity, len(newTicks))
	for tick, tl := range newTicks {
		if tl == nil {
			tl = &TickLiquidity{}
		}
		liqNet := new(big.Int)
		if tl.LiquidityNet != nil {
			liqNet.Set(tl.LiquidityNet)
		}
		liqGross := new(big.Int)
		if tl.LiquidityGross != nil {
			liqGross.Set(tl.LiquidityGross)
		} else {
			// DB 快照旧格式可能不包含 gross，使用 |net| 作为保守初值，避免后续事件处理崩溃。
			liqGross.Abs(liqNet)
		}
		normalized[tick] = &TickLiquidity{
			LiquidityNet:   liqNet,
			LiquidityGross: liqGross,
		}
		utils.SetBitmapBit(tick, p.TickSpacing, true, bitmap)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.Ticks = normalized
	p.Bitmap = bitmap
}

// ReplaceFromState 原子替换整个池子运行时状态。
// 传入状态会先深拷贝，避免外部后续修改影响内部内存。
func (p *State) ReplaceFromState(next *State) {
	if next == nil {
		return
	}
	cp := next.GetStateCopy()

	p.mu.Lock()
	defer p.mu.Unlock()

	p.Address = cp.Address
	p.Token0 = cp.Token0
	p.Token1 = cp.Token1
	p.Fee = cp.Fee
	p.TickSpacing = cp.TickSpacing
	p.Token0Symbol = cp.Token0Symbol
	p.Token1Symbol = cp.Token1Symbol
	p.SqrtPriceX96 = new(big.Int).Set(cp.SqrtPriceX96)
	p.Tick = cp.Tick
	p.Liquidity = new(big.Int).Set(cp.Liquidity)
	p.BlockNumber = cp.BlockNumber

	p.Ticks = make(map[int32]*TickLiquidity, len(cp.Ticks))
	for tick, tl := range cp.Ticks {
		if tl == nil {
			p.Ticks[tick] = &TickLiquidity{
				LiquidityNet:   new(big.Int),
				LiquidityGross: new(big.Int),
			}
			continue
		}
		liqNet := new(big.Int)
		if tl.LiquidityNet != nil {
			liqNet.Set(tl.LiquidityNet)
		}
		liqGross := new(big.Int)
		if tl.LiquidityGross != nil {
			liqGross.Set(tl.LiquidityGross)
		}
		p.Ticks[tick] = &TickLiquidity{
			LiquidityNet:   liqNet,
			LiquidityGross: liqGross,
		}
	}

	p.Bitmap = make(map[uint16]utils.Word256, len(cp.Bitmap))
	for k, v := range cp.Bitmap {
		p.Bitmap[k] = v
	}
}

// GetTickLiquidity 获取指定 tick 上的流动性净额。
// 如果该 tick 上无流动性，返回 0。
func (p *State) GetTickLiquidity(tick int32) *big.Int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if tl, ok := p.Ticks[tick]; ok {
		return new(big.Int).Set(tl.LiquidityNet)
	}
	return big.NewInt(0)
}

// GetTickCount 返回当前有流动性记录的 tick 数量。
func (p *State) GetTickCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.Ticks)
}

// GetTicksCopy 返回所有 tick 流动性数据的深拷贝。
func (p *State) GetTicksCopy() map[int32]*TickLiquidity {
	p.mu.RLock()
	defer p.mu.RUnlock()

	cp := make(map[int32]*TickLiquidity, len(p.Ticks))
	for k, v := range p.Ticks {
		liqNet := new(big.Int)
		liqGross := new(big.Int)
		if v != nil {
			if v.LiquidityNet != nil {
				liqNet.Set(v.LiquidityNet)
			}
			if v.LiquidityGross != nil {
				liqGross.Set(v.LiquidityGross)
			}
		}
		cp[k] = &TickLiquidity{
			LiquidityNet:   liqNet,
			LiquidityGross: liqGross,
		}
	}
	return cp
}

// GetRawState 线程安全地读取原始链上状态字段，用于健康检查比价。
// 返回 sqrtPriceX96 的副本、tick、liquidity 的副本、blockNumber。
func (p *State) GetRawState() (sqrtPriceX96 *big.Int, tick int32, liquidity *big.Int, blockNumber int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return new(big.Int).Set(p.SqrtPriceX96), p.Tick, new(big.Int).Set(p.Liquidity), blockNumber
}

// GetStateCopy 获取状态的深拷贝，避免外部后续修改影响内部状态。
func (p *State) GetStateCopy() *State {
	p.mu.RLock()
	defer p.mu.RUnlock()

	cp := &State{
		Address:      p.Address,
		Token0:       p.Token0,
		Token1:       p.Token1,
		Fee:          p.Fee,
		TickSpacing:  p.TickSpacing,
		Token0Symbol: p.Token0Symbol,
		Token1Symbol: p.Token1Symbol,
		SqrtPriceX96: new(big.Int).Set(p.SqrtPriceX96),
		Tick:         p.Tick,
		Liquidity:    new(big.Int).Set(p.Liquidity),
		Ticks:        p.GetTicksCopy(),
		Bitmap:       make(map[uint16]utils.Word256, len(p.Bitmap)),
		BlockNumber:  p.BlockNumber,
	}
	for k, v := range p.Bitmap {
		cp.Bitmap[k] = v
	}
	return cp
}

// SwapState 记录 Swap 过程中的全局剩余状态
type SwapState struct {
	amountSpecifiedRemaining *big.Int // 剩余需要输入的代币数量
	amountCalculated         *big.Int // 已经换出的代币数量
	sqrtPriceX96             *big.Int // 当前迭代的价格
	tick                     int32    // 当前迭代的 tick
	liquidity                *big.Int // 当前迭代激活的全局流动性
}

// StepComputations 记录单步（当前 Tick 到下一个 Tick 之间）的计算中间结果
type StepComputations struct {
	sqrtPriceNextX96  *big.Int // 下一个有效 Tick 的价格
	tickNext          int32    // 下一个有效 Tick 的位置
	initialized       bool     // 下一个 Tick 是否被初始化
	sqrtPriceAfterX96 *big.Int // 执行完当前步后的最终价格
	amountIn          *big.Int // 当前步消耗的输入代币
	amountOut         *big.Int // 当前步产出的输出代币
}

func (p *State) QuoteExactInput(amountIn *big.Int, address common.Address) (*big.Int, error) {
	zeroForOne := false
	if p.Token0 == address {
		zeroForOne = true
	}

	return p.SimulateSwap(amountIn, zeroForOne), nil
}

// SimulateSwap 内存报价核心函数
// amountIn: 输入代币的数量
// zeroForOne: true 代表用 Token0 换 Token1 (价格下跌); false 代表用 Token1 换 Token0 (价格上涨)
func (p *State) SimulateSwap(amountIn *big.Int, zeroForOne bool) *big.Int {
	if amountIn.Sign() == 0 {
		return big.NewInt(0)
	}

	// 1. 初始化交易状态机（从池子当前状态拷贝，不污染原始数据）
	state := SwapState{
		amountSpecifiedRemaining: new(big.Int).Set(amountIn),
		amountCalculated:         big.NewInt(0),
		sqrtPriceX96:             new(big.Int).Set(p.SqrtPriceX96),
		tick:                     p.Tick,
		liquidity:                new(big.Int).Set(p.Liquidity),
	}

	// 2. 循环迭代，直到输入的代币被完全消耗
	for state.amountSpecifiedRemaining.Sign() > 0 {
		var step StepComputations

		// 步骤一：利用位图，寻找当前方向上的下一个有效 Tick
		// zeroForOne = true 时向左找 (lte = true)；zeroForOne = false 时向右找 (lte = false)
		step.tickNext, step.initialized = p.NextInitializedTickWithinOneWord(state.tick, zeroForOne)

		// 步骤二：根据下一个 Tick 算出对应的目标价格 sqrt(P)
		step.sqrtPriceNextX96 = utils.SqrtPriceMathGetSqrtPriceAtTick(step.tickNext)

		// 步骤三：计算在当前流动性下，价格移动到边界所需的最大输入量，以及实际达到的新价格
		step.sqrtPriceAfterX96, step.amountIn, step.amountOut = utils.SqrtPriceMathComputeSwapStep(
			state.sqrtPriceX96,
			step.sqrtPriceNextX96,
			state.liquidity,
			state.amountSpecifiedRemaining,
			zeroForOne,
		)

		// 步骤四：更新状态机中的代币余额
		state.amountSpecifiedRemaining.Sub(state.amountSpecifiedRemaining, step.amountIn)
		state.amountCalculated.Add(state.amountCalculated, step.amountOut)

		// 步骤五：如果价格成功推到了下一个 Tick 边界，且该 Tick 被真实初始化了，需要“穿过”它
		if step.sqrtPriceAfterX96.Cmp(step.sqrtPriceNextX96) == 0 {
			if step.initialized {
				tickData := p.Ticks[step.tickNext]
				if tickData != nil {
					// 穿过 Tick 更新全局流动性 L
					// 如果 zeroForOne = true（价格下跌），向左穿过 Tick 需减去 LiquidityNet
					// 如果 zeroForOne = false（价格上涨），向右穿过 Tick 需加上 LiquidityNet
					if zeroForOne {
						state.liquidity.Sub(state.liquidity, tickData.LiquidityNet)
					} else {
						state.liquidity.Add(state.liquidity, tickData.LiquidityNet)
					}
				}
			}
			// 迈入下一个区间
			if zeroForOne {
				state.tick = step.tickNext - 1
			} else {
				state.tick = step.tickNext
			}
			state.sqrtPriceX96 = step.sqrtPriceNextX96
		} else if state.sqrtPriceX96.Cmp(step.sqrtPriceAfterX96) != 0 {
			// 钱不够推到边界，价格停在了区间内部，更新当前价格并结束当前步
			state.sqrtPriceX96 = step.sqrtPriceAfterX96
			state.tick = utils.SqrtPriceMathGetTickAtSqrtPrice(step.sqrtPriceAfterX96)
		}
	}

	// 返回最终换出的代币总量
	return state.amountCalculated
}

// NextInitializedTickWithinOneWord 考虑 tickSpacing 的单 Word 内有效 Tick 搜索
// tick: 当前的物理 tick 刻度（未压缩）
// lte: true 代表向左找（价格下跌，<= tick）；false 代表向右找（价格上涨，> tick）
func (p *State) NextInitializedTickWithinOneWord(tick int32, lte bool) (nextTick int32, initialized bool) {
	if p.TickSpacing <= 0 {
		p.TickSpacing = 1
	}

	// 1. 边界对齐与压缩
	// 如果 tick 是负数且不能被 tickSpacing 整除，Go 的 '/' 是向零取整，
	// 而 Uniswap 官方需要向下（向负无穷）取整，所以需要特殊处理。
	compressed := tick / p.TickSpacing
	if tick < 0 && tick%p.TickSpacing != 0 {
		compressed--
	}

	if lte {
		// === 向左找 (<= 当前 tick) ===
		wordIndex := uint16(compressed >> 8)
		bitIndex := uint8(compressed & 0xFF)

		// 构造掩码：抹去比 bitIndex 高的位
		mask := utils.GetWordMaskLE(bitIndex)
		word := p.Bitmap[wordIndex]

		// 按位与，提取出合法范围内的 1
		maskedWord := utils.AndWord(word, mask)

		if !utils.IsWordZero(maskedWord) {
			// 找到最高位的 1
			nextBitIndex := utils.MostSignificantBit(maskedWord)
			// 还原成物理 Tick：(wordIndex * 256 + bitIndex) * tickSpacing
			nextTick = ((int32(wordIndex) << 8) | int32(nextBitIndex)) * p.TickSpacing
			return clampTick(nextTick), true
		}

		// 没找到，返回当前 Word 的左边界
		nextTick = (int32(wordIndex) << 8) * p.TickSpacing
		return clampTick(nextTick), false

	} else {
		// === 向右找 (> 当前 tick) ===
		// 向右找需要严格大于当前 tick，所以要加 1
		compressed++

		wordIndex := uint16(compressed >> 8)
		bitIndex := uint8(compressed & 0xFF)

		// 构造掩码：抹去比 bitIndex 低的位
		mask := utils.GetWordMaskGE(bitIndex)
		word := p.Bitmap[wordIndex]

		maskedWord := utils.AndWord(word, mask)

		if !utils.IsWordZero(maskedWord) {
			// 找到最低位的 1
			nextBitIndex := utils.LeastSignificantBit(maskedWord)
			nextTick = ((int32(wordIndex) << 8) | int32(nextBitIndex)) * p.TickSpacing
			return clampTick(nextTick), true
		}

		// 没找到，返回当前 Word 的右边界（即下一个 Word 的开头）
		nextTick = ((int32(wordIndex) + 1) << 8) * p.TickSpacing
		return clampTick(nextTick), false
	}
}

func clampTick(tick int32) int32 {
	if tick < utils.MinTick {
		return utils.MinTick
	}
	if tick > utils.MaxTick {
		return utils.MaxTick
	}
	return tick
}
