// Package service 编排池子状态管理与事件订阅，对外暴露统一的报价服务接口。
package service

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/metrics"
	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/brianliu-sysu/arbitrage/internal/store"
	"github.com/brianliu-sysu/arbitrage/internal/subscriber"
	"github.com/brianliu-sysu/arbitrage/internal/tracing"
	"github.com/ethereum/go-ethereum/common"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// PoolQuoteService 是 Uniswap V3 池子报价服务的顶层封装。
// 它持有池子状态（pool.PoolState），实现 subscriber.EventHandler 接口
// 以接收链上事件，并向上层暴露报价查询接口。
type PoolQuoteService struct {
	pool       *pool.PoolState
	subscriber *subscriber.Subscriber
	logger     logx.Logger
	store      store.Storer

	mu            sync.RWMutex
	onPriceUpdate func(poolAddr common.Address, price0In1, price1In0 float64, tick int32)

	// 健康检查
	healthCheckInterval time.Duration
	healthCheckCtx      context.Context
	healthCheckCancel   context.CancelFunc
	healthCheckWg       sync.WaitGroup

	// 全量同步节流
	lastFullSyncTime      time.Time
	fullSyncMinGap        time.Duration // 两次全量同步的最小间隔，默认 5 分钟
	maxBlockGapForFullSync uint64        // 全量同步最大区块间隔，超过此值才重建 tick 地图
}

// Config 创建 PoolQuoteService 所需的配置。
type Config struct {
	PoolAddress              common.Address // Uniswap V3 Pool 合约地址
	HealthCheckIntervalSec   int            // 健康检查间隔（秒），0 表示禁用
	MaxBlockGapForFullSync   uint64         // 全量同步最大区块间隔，0 使用默认值 100
}

// NewPoolQuoteService 创建报价服务。
//
// wsURL 为 WebSocket 端点（订阅链上事件），rpcURL 为 HTTP RPC 端点（eth_call 等）。
// token0 / token1 / fee 会在调用 ResolvePoolMetadata 时通过 RPC 获取。
func NewPoolQuoteService(wsURL, rpcURL string, cfg Config, logger logx.Logger, st store.Storer) (*PoolQuoteService, error) {
	poolState := pool.NewPoolState(cfg.PoolAddress, common.Address{}, common.Address{}, 0)

	maxGap := cfg.MaxBlockGapForFullSync
	if maxGap == 0 {
		maxGap = 100
	}

	svc := &PoolQuoteService{
		pool:                   poolState,
		logger:                 logger,
		store:                  st,
		healthCheckInterval:    time.Duration(cfg.HealthCheckIntervalSec) * time.Second,
		fullSyncMinGap:         5 * time.Minute,
		maxBlockGapForFullSync: maxGap,
	}

	sub, err := subscriber.NewSubscriber(wsURL, rpcURL, cfg.PoolAddress, svc, logger)
	if err != nil {
		return nil, fmt.Errorf("create subscriber: %w", err)
	}
	svc.subscriber = sub

	return svc, nil
}

// ResolvePoolMetadata 通过 RPC 获取池子的 token0 / token1 / fee 并设置到内部状态中。
//
// 必须在 Start 之前调用（Start 中的 SyncStateFromRPC 会用到 fee 信息）。
func (s *PoolQuoteService) ResolvePoolMetadata() error {
	if s.subscriber == nil {
		return fmt.Errorf("subscriber is nil")
	}
	meta, err := s.subscriber.FetchPoolMetadata()
	if err != nil {
		return fmt.Errorf("fetch pool metadata for %s: %w", s.pool.Address.Hex(), err)
	}

	s.pool.SetTokens(meta.Token0, meta.Token1, meta.Fee)
	s.logger.Info("pool metadata resolved",
		"pool", s.pool.Address.Hex(),
		"token0", meta.Token0.Hex(),
		"token1", meta.Token1.Hex(),
		"fee", meta.Fee,
	)
	return nil
}

// Start 启动服务：
//  1. 通过 RPC + 历史事件回放完成全量状态同步
//  2. 开始实时事件订阅
//  3. 启动定时健康检查（如已配置）
//
// syncFromBlock 为 0 表示跳过事件历史同步（RPC 初始化仍会执行）。
func (s *PoolQuoteService) Start(syncFromBlock uint64) error {
	if s.subscriber != nil {
		if err := s.DoFullSync(syncFromBlock); err != nil {
			s.logger.Warn("full sync failed, will rely on events", "error", err)
		}

		if err := s.subscriber.Start(); err != nil {
			return err
		}
	}

	// 启动定时健康检查
	s.startHealthCheck()
	return nil
}

// Stop 停止事件订阅、健康检查，并释放连接资源。
func (s *PoolQuoteService) Stop() {
	// 先停健康检查
	if s.healthCheckCancel != nil {
		s.healthCheckCancel()
	}
	s.healthCheckWg.Wait()

	if s.subscriber != nil {
		s.subscriber.Stop()
	}
}

// ---------------------------------------------------------------------------
// subscriber.EventHandler 接口实现
// ---------------------------------------------------------------------------

func (s *PoolQuoteService) OnSwap(event *pool.SwapEvent) {
	_, span := tracing.Tracer().Start(context.Background(), "service.on_swap", // context.Background() since events arrive async from WS
		trace.WithAttributes(
			attribute.String("pool", s.pool.Address.Hex()),
			attribute.Int64("block", int64(event.Raw.BlockNumber)),
		),
	)
	defer span.End()

	s.pool.UpdateFromSwap(
		event.SqrtPriceX96,
		event.Tick,
		event.Liquidity,
		event.Raw.BlockNumber,
	)

	s.logger.Info("swap event",
		"block", event.Raw.BlockNumber,
		"amount0", event.Amount0.String(),
		"amount1", event.Amount1.String(),
		"tick", event.Tick,
	)

	s.emitPriceUpdate()
	s.saveSnapshot()
}

func (s *PoolQuoteService) OnMint(event *pool.MintEvent) {
	// 仅更新 tick 级流动性地图（总流动性 L 由后续 Swap 事件或 RPC 同步修正）
	s.pool.UpdateTickFromMint(event.TickLower, event.TickUpper, event.Amount)

	s.logger.Info("mint event",
		"block", event.Raw.BlockNumber,
		"owner", event.Owner.Hex(),
		"tickLower", event.TickLower,
		"tickUpper", event.TickUpper,
		"amount", event.Amount.String(),
		"totalTicks", s.pool.GetTickCount(),
	)
}

func (s *PoolQuoteService) OnBurn(event *pool.BurnEvent) {
	// 仅更新 tick 级流动性地图（总流动性 L 由后续 Swap 事件或 RPC 同步修正）
	s.pool.UpdateTickFromBurn(event.TickLower, event.TickUpper, event.Amount)

	s.logger.Info("burn event",
		"block", event.Raw.BlockNumber,
		"owner", event.Owner.Hex(),
		"tickLower", event.TickLower,
		"tickUpper", event.TickUpper,
		"amount", event.Amount.String(),
		"totalTicks", s.pool.GetTickCount(),
	)
}

func (s *PoolQuoteService) OnError(err error) {
	s.logger.Error("subscriber error", "error", err)
}

// OnReconnected 在 WebSocket 断线重连成功后调用。
// 执行轻量同步：仅 RPC 快照 + 事件回放，不重建 tick 地图。
// 如果距上次全量重建超过 fullSyncMinGap（5分钟），则触发完整的 tick 地图重建。
func (s *PoolQuoteService) OnReconnected() {
	_, _, _, memBlock := s.pool.GetPrices()
	s.logger.Info("reconnected, syncing state", "pool", s.pool.Address.Hex(), "fromBlock", memBlock)

	// 轻量同步：RPC 快照 + 可选事件回放（不重建 tick 地图）
	if err := s.DoLightSync(memBlock); err != nil {
		s.logger.Error("light sync failed after reconnect", "pool", s.pool.Address.Hex(), "error", err)
	}

	// 如果距上次全量 tick 重建超过阈值，触发完整同步
	if time.Since(s.lastFullSyncTime) > s.fullSyncMinGap {
		s.logger.Info("full tick rebuild due (last rebuild was %v ago)", "elapsed", time.Since(s.lastFullSyncTime))
		if err := s.DoFullSync(memBlock); err != nil {
			s.logger.Error("full sync failed after reconnect", "pool", s.pool.Address.Hex(), "error", err)
		}
	}
}

// ---------------------------------------------------------------------------
// 报价接口
// ---------------------------------------------------------------------------

// SetOnPriceUpdate 设置价格更新时的回调函数。
func (s *PoolQuoteService) SetOnPriceUpdate(fn func(poolAddr common.Address, price0In1, price1In0 float64, tick int32)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onPriceUpdate = fn
}

// GetPrice 获取当前现货价格及其所在 tick。
func (s *PoolQuoteService) GetPrice() (price0In1, price1In0 float64, tick int32) {
	price0In1, price1In0, tick, _ = s.pool.GetPrices()
	return
}

// QuoteExactInput 精确输入报价：通过 eth_call 模拟 pool.swap() 得到 EVM 精度的报价。
//
// amountIn - 输入数量（最小单位）
// tokenIn  - 输入代币地址（必须是 token0 或 token1 之一）
func (s *PoolQuoteService) QuoteExactInput(amountIn *big.Int, tokenIn common.Address) (*big.Int, error) {
	if s.subscriber == nil {
		return nil, fmt.Errorf("subscriber is nil")
	}
	zeroForOne := tokenIn == s.pool.Token0
	poolAddr := s.pool.Address.Hex()

	_, span := tracing.Tracer().Start(context.Background(), "service.quote_exact_input", // Background(): called from gin handler with no propagated ctx
		trace.WithAttributes(
			attribute.String("pool", poolAddr),
			attribute.String("token_in", tokenIn.Hex()),
			attribute.String("amount_in", amountIn.String()),
		),
	)
	defer span.End()

	amountOut, err := s.subscriber.SimulateSwap(amountIn, zeroForOne, nil)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("simulate swap for %s: %w", poolAddr, err)
	}
	span.SetAttributes(attribute.String("amount_out", amountOut.String()))
	metrics.QuotesTotal.WithLabelValues(poolAddr, "eth_call").Inc()
	return amountOut, nil
}

// GetPoolInfo 获取池子基本信息的快照，用于调试与监控。
func (s *PoolQuoteService) GetPoolInfo() map[string]interface{} {
	state := s.pool.GetStateCopy()
	return map[string]interface{}{
		"address":      state.Address.Hex(),
		"token0":       state.Token0.Hex(),
		"token1":       state.Token1.Hex(),
		"token0Symbol": state.Token0Symbol,
		"token1Symbol": state.Token1Symbol,
		"fee":          state.Fee,
		"tick":         state.Tick,
		"sqrtPriceX96": state.SqrtPriceX96.String(),
		"liquidity":    state.Liquidity.String(),
		"price0In1":    state.Price0In1,
		"price1In0":    state.Price1In0,
		"blockNumber":  state.BlockNumber,
	}
}

// DoFullSync 执行全量状态同步：
//  1. 通过 RPC slot0() + liquidity() 获取当前链上快照
//  2. 通过 Tick Bitmap 直接从链上重建所有活跃 tick 的流动性地图
//  3. 应用 RPC 快照兜底
//
// syncFromBlock 不再用于事件回放（tick 状态直接从链上读取）。
func (s *PoolQuoteService) DoFullSync(syncFromBlock uint64) error {
	if s.subscriber == nil {
		return fmt.Errorf("subscriber is nil")
	}

	// 1. RPC 快照 —— 获取当前链上最新状态
	rpcState, err := s.subscriber.FetchStateViaRPC()
	if err != nil {
		return fmt.Errorf("fetch rpc state: %w", err)
	}

	// 2. 判断是否需要全量重建 tick 地图
	_, _, _, memBlock := s.pool.GetPrices()
	tickCount := s.pool.GetTickCount()
	gap := rpcState.BlockNumber - memBlock

	// 需要全量重建的条件：
	//   a) memBlock == 0: 冷启动，无历史状态
	//   b) gap > 阈值: 断线太久，落后期过大
	//   c) tickCount == 0 && memBlock > 0: 旧 DB 数据有 price 但缺少 tick 地图（修复历史脏数据）
	needsRebuild := memBlock == 0 || gap > s.maxBlockGapForFullSync || tickCount == 0

	if needsRebuild {
		reason := "cold start"
		if tickCount == 0 && memBlock > 0 {
			reason = "tick map empty (stale data)"
		} else if gap > s.maxBlockGapForFullSync {
			reason = "gap exceeds threshold"
		}
		s.logger.Info("full sync: rebuilding tick map",
			"reason", reason, "gap", gap, "threshold", s.maxBlockGapForFullSync,
			"memBlock", memBlock, "chainBlock", rpcState.BlockNumber, "tickCount", tickCount)
		release := AcquireFullSyncSlot()
		if err := s.RebuildTickMapFromChain(); err != nil {
			s.logger.Warn("tick map rebuild failed, price/liquidity sync will proceed", "error", err)
		}
		release()
	} else {
		s.logger.Info("full sync: skipping tick rebuild (gap within threshold, tick map populated)",
			"gap", gap, "threshold", s.maxBlockGapForFullSync, "tickCount", tickCount)
	}

	// 3. 应用 RPC 快照 —— 兜底保证最终状态一致
	s.pool.UpdateFromSwap(rpcState.SqrtPriceX96, rpcState.Tick, rpcState.Liquidity, rpcState.BlockNumber)

	s.logger.Info("full sync completed",
		"block", rpcState.BlockNumber,
		"tick", rpcState.Tick,
		"sqrtPriceX96", rpcState.SqrtPriceX96.String(),
		"liquidity", rpcState.Liquidity.String(),
		"ticks", s.pool.GetTickCount(),
	)

	s.lastFullSyncTime = time.Now()
	s.emitPriceUpdate()
	s.saveSnapshot() // 全量同步后持久化最新状态
	return nil
}

// DoLightSync 执行轻量同步：仅 RPC 快照（slot0 + liquidity），不重建 tick 地图。
// 适用于 WebSocket 重连后快速恢复状态，避免频繁的 tick bitmap 扫描。
// RPC 快照直接返回链上权威的 slot0 + liquidity，无需额外的事件回放。
func (s *PoolQuoteService) DoLightSync(syncFromBlock uint64) error {
	if s.subscriber == nil {
		return fmt.Errorf("subscriber is nil")
	}

	rpcState, err := s.subscriber.FetchStateViaRPC()
	if err != nil {
		return fmt.Errorf("fetch rpc state: %w", err)
	}

	s.pool.UpdateFromSwap(rpcState.SqrtPriceX96, rpcState.Tick, rpcState.Liquidity, rpcState.BlockNumber)
	s.logger.Info("light sync completed",
		"block", rpcState.BlockNumber, "tick", rpcState.Tick,
		"ticks", s.pool.GetTickCount(),
	)

	s.emitPriceUpdate()
	s.saveSnapshot() // 轻量同步后持久化，确保 DB 有最新状态
	return nil
}

// RebuildTickMapFromChain 通过 Tick Bitmap 直接从链上重建完整的 tick 流动性地图。
func (s *PoolQuoteService) RebuildTickMapFromChain() error {
	if s.subscriber == nil {
		return fmt.Errorf("subscriber is nil")
	}

	currentTick := s.pool.Tick
	beforeCount := s.pool.GetTickCount()
	s.pool.ClearTicks()

	tickSpacing, err := s.subscriber.FetchTickSpacing()
	if err != nil {
		return fmt.Errorf("fetch tick spacing: %w", err)
	}
	wordRange := int32(256) * tickSpacing
	startWord := tickToWord(currentTick, wordRange)

	// 1. 发现所有活跃 word
	words, wordBitmaps, err := s.discoverActiveWords(startWord, wordRange, 8192)
	if err != nil {
		return fmt.Errorf("discover words: %w", err)
	}
	s.logger.Info("active words", "length", len(words))

	// 2. 从已缓存的 bitmap 中提取活跃 tick 索引（无需重复 RPC）
	allTicks := s.collectTicksFromWordsCached(words, wordRange, tickSpacing, wordBitmaps)
	s.logger.Info("active tick", "length", len(allTicks))

	// 3. 并发获取每个 tick 的链上数据（10 并发）
	tickDataMap := s.fetchTicksConcurrently(context.Background(), allTicks)

	// 4. 应用结果
	totalTicks := s.applyTickData(tickDataMap)

	s.logger.Info("tick rebuild complete",
		"ticks", totalTicks, "before", beforeCount,
		"tickSpacing", tickSpacing, "currentTick", currentTick)
	return nil
}

// ---------------------------------------------------------------------------
// Tick Bitmap 重建 — 子方法
// ---------------------------------------------------------------------------

// tickToWord 计算 tick 所在的 bitmap word 索引。
func tickToWord(tick int32, wordRange int32) int16 {
	if tick >= 0 {
		return int16(tick / wordRange)
	}
	return int16((tick - (wordRange - 1)) / wordRange)
}

// tickFromWordBit 从 word 和 bit 计算对应的 tick 值。
func tickFromWordBit(wordPos int16, bit int, wordRange, tickSpacing int32) int32 {
	return int32(wordPos)*wordRange + int32(bit)*tickSpacing
}

// discoverActiveWords 从 startWord 开始向两端扫描，收集所有非空 word 索引。
// 遇到连续 3 个空 word 即停止。
func (s *PoolQuoteService) discoverActiveWords(startWord int16, wordRange int32, maxWord int16) ([]int16, map[int16]*big.Int, error) {
	var words []int16
	bitmaps := make(map[int16]*big.Int)
	words = append(words, startWord)

	// 正方向
	emptyStreak := 0
	for w := startWord + 1; w <= maxWord && emptyStreak < 3; w++ {
		if bitmap := s.fetchAndCacheWord(w, bitmaps); bitmap {
			words = append(words, w)
			emptyStreak = 0
		} else {
			emptyStreak++
		}
	}

	// 负方向
	emptyStreak = 0
	for w := startWord - 1; w >= -maxWord && emptyStreak < 3; w-- {
		if bitmap := s.fetchAndCacheWord(w, bitmaps); bitmap {
			words = append(words, w)
			emptyStreak = 0
		} else {
			emptyStreak++
		}
	}

	return words, bitmaps, nil
}

// fetchAndCacheWord 获取 word 的 bitmap 并缓存，返回是否非空。
func (s *PoolQuoteService) fetchAndCacheWord(wordPos int16, cache map[int16]*big.Int) bool {
	bitmap, err := s.subscriber.FetchTickBitmap(wordPos)
	if err != nil || bitmap.Sign() == 0 {
		return false
	}
	cache[wordPos] = bitmap
	return true
}



// collectTicksFromWords 遍历所有 word 的 bitmap，提取其中 set bit 对应的 tick 列表。
// collectTicksFromWords 已弃用，请使用 collectTicksFromWordsCached 以避免重复 RPC。
func (s *PoolQuoteService) collectTicksFromWords(words []int16, wordRange, tickSpacing int32) []int32 {
	return s.collectTicksFromWordsCached(words, wordRange, tickSpacing, nil)
}

// collectTicksFromWordsCached 遍历 word bitmap，提取活跃 tick。bitmaps 作为缓存复用。
func (s *PoolQuoteService) collectTicksFromWordsCached(words []int16, wordRange, tickSpacing int32, bitmaps map[int16]*big.Int) []int32 {
	var ticks []int32
	for _, wordPos := range words {
		bitmap, ok := bitmaps[wordPos]
		if !ok {
			var err error
			bitmap, err = s.subscriber.FetchTickBitmap(wordPos)
			if err != nil || bitmap.Sign() == 0 {
				continue
			}
			if bitmaps != nil {
				bitmaps[wordPos] = bitmap
			}
		}
		for bit := 0; bit < 256; bit++ {
			if bitmap.Bit(bit) == 0 {
				continue
			}
			tick := tickFromWordBit(wordPos, bit, wordRange, tickSpacing)
			if tick >= pool.TickMin && tick <= pool.TickMax {
				ticks = append(ticks, tick)
			}
		}
	}
	return ticks
}

// fetchTicksConcurrently 并发获取所有 tick 的链上数据。
// 最大 5 并发，速率限制 100 req/s，失败自动重试 2 次。
// ctx 取消时会提前退出，返回已获取的部分结果。
func (s *PoolQuoteService) fetchTicksConcurrently(ctx context.Context, ticks []int32) map[int32]*subscriber.TickData {
	if len(ticks) == 0 {
		return nil
	}

	const maxConcurrency = 5
	const requestsPerSecond = 100
	const maxRetries = 2

	sem := make(chan struct{}, maxConcurrency)
	rateLimiter := time.NewTicker(time.Second / requestsPerSecond)
	defer rateLimiter.Stop()

	result := make(map[int32]*subscriber.TickData)
	var mu sync.Mutex
	var wg sync.WaitGroup

	aborted := false
	var abortMu sync.Mutex

loop:
	for _, tick := range ticks {
		select {
		case <-rateLimiter.C:
		case <-ctx.Done():
			break loop
		}

		abortMu.Lock()
		if aborted {
			abortMu.Unlock()
			break loop
		}
		abortMu.Unlock()

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}

		wg.Add(1)
		go func(t int32) {
			defer wg.Done()
			defer func() { <-sem }()

			var data *subscriber.TickData
			var err error
			for attempt := 0; attempt <= maxRetries; attempt++ {
				data, err = s.subscriber.FetchTickInfo(t)
				if err == nil {
					break
				}
				if attempt < maxRetries {
					time.Sleep(time.Duration(100<<uint(attempt)) * time.Millisecond)
				}
			}
			if err != nil {
				s.logger.Warn("tick rebuild: fetch tick failed after retries, skipping",
					"tick", t, "retries", maxRetries, "error", err)
				return
			}
			mu.Lock()
			result[t] = data
			mu.Unlock()
		}(tick)
	}

	wg.Wait()

	if aborted {
		s.logger.Warn("tick rebuild: aborted early",
			"fetched", len(result), "total", len(ticks))
	}

	return result
}

// applyTickData 将从链上获取的 tick 数据应用到池子状态中。
func (s *PoolQuoteService) applyTickData(data map[int32]*subscriber.TickData) int {
	count := 0
	for tick, d := range data {
		if d.Initialized && d.LiquidityNet.Sign() != 0 {
			s.pool.SetTickLiquidity(tick, d.LiquidityNet)
			count++
		}
	}
	return count
}

// SyncStateFromRPC 已弃用，请使用 DoFullSync。
// 保留用于向后兼容。
func (s *PoolQuoteService) SyncStateFromRPC() error {
	return s.DoFullSync(0)
}

// startHealthCheck 启动定时健康检查 goroutine。
func (s *PoolQuoteService) startHealthCheck() {
	if s.healthCheckInterval <= 0 {
		s.logger.Info("[health] health check disabled (interval = 0)")
		return
	}

	s.healthCheckCtx, s.healthCheckCancel = context.WithCancel(context.Background())

	s.healthCheckWg.Add(1)
	go func() {
		defer s.healthCheckWg.Done()

		ticker := time.NewTicker(s.healthCheckInterval)
		defer ticker.Stop()

		s.logger.Info("health check started", "interval", s.healthCheckInterval)

		for {
			select {
			case <-ticker.C:
				s.runHealthCheck()
			case <-s.healthCheckCtx.Done():
				s.logger.Info("[health] stopped")
				return
			}
		}
	}()
}

// runHealthCheck 执行一次健康检查：
//  1. 通过 RPC 获取链上最新状态快照
//  2. 与内存中的 sqrtPriceX96 / liquidity / tick 逐一比对
//  3. 任一字段不一致 → 触发全量同步（RPC 快照 + 历史事件重放）
func (s *PoolQuoteService) runHealthCheck() {
	if s.subscriber == nil {
		return
	}
	rpcState, err := s.subscriber.FetchStateViaRPC()
	if err != nil {
		s.logger.Warn("health check: RPC fetch failed", "error", err)
		return
	}

	memSqrtPrice, memTick, memLiquidity, memBlock := s.pool.GetRawState()

	// 将 RPC 状态与内存状态逐一比对
	var diverged bool
	if memTick != rpcState.Tick {
		s.logger.Warn("health check: tick diverged", "mem", memTick, "chain", rpcState.Tick)
		diverged = true
	}
	if memSqrtPrice.Cmp(rpcState.SqrtPriceX96) != 0 {
		s.logger.Warn("health check: sqrtPrice diverged", "mem", memSqrtPrice.String(), "chain", rpcState.SqrtPriceX96.String())
		diverged = true
	}
	if memLiquidity.Cmp(rpcState.Liquidity) != 0 {
		s.logger.Warn("health check: liquidity diverged", "mem", memLiquidity.String(), "chain", rpcState.Liquidity.String())
		diverged = true
	}

	if !diverged {
		return
	}

	s.logger.Warn("health check: state diverged",
		"memBlock", memBlock, "memTick", memTick,
		"memSqrtPrice", memSqrtPrice.String(), "memLiq", memLiquidity.String(),
		"chainBlock", rpcState.BlockNumber, "chainTick", rpcState.Tick,
		"chainSqrtPrice", rpcState.SqrtPriceX96.String(), "chainLiq", rpcState.Liquidity.String())

	metrics.HealthRepairsTotal.WithLabelValues(s.pool.Address.Hex()).Inc()

	// 传入内存中的当前块号作为重放下界，补齐断线期间的缺失事件
	if err := s.DoFullSync(memBlock); err != nil {
		s.logger.Error("health check: full sync failed", "error", err)
	}
}

// saveSnapshot 将当前池子状态持久化到数据库。
// 使用带缓冲 channel 限制并发写入数为 5，防止高频 swap 时 goroutine 爆炸。
var saveSem = make(chan struct{}, 5)

func (s *PoolQuoteService) saveSnapshot() {
	if s.store == nil {
		return
	}
	state := s.pool.GetStateCopy()
	snap := &store.PoolSnapshot{
		PoolAddress:  state.Address.Hex(),
		BlockNumber:  state.BlockNumber,
		Tick:         state.Tick,
		SqrtPriceX96: state.SqrtPriceX96,
		Liquidity:    state.Liquidity,
		Price0In1:    state.Price0In1,
		TickData:     make(map[string]string),
	}
	for tick, tl := range state.Ticks {
		snap.TickData[fmt.Sprintf("%d", tick)] = tl.LiquidityNet.String()
	}

	select {
	case saveSem <- struct{}{}:
		go func() {
			defer func() { <-saveSem }()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.store.Save(ctx, snap); err != nil {
				s.logger.Warn("failed to save pool state", "pool", snap.PoolAddress, "error", err)
			}
		}()
	default:
		// 写入太频繁，丢弃本次快照（下一个 swap 会再次触发保存）
		s.logger.Debug("save snapshot skipped (rate limited)", "pool", snap.PoolAddress)
	}
}

// LoadFromStore 从数据库恢复池子状态，返回上次保存的区块号。
func (s *PoolQuoteService) LoadFromStore() (uint64, error) {
	if s.store == nil {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	snap, err := s.store.Load(ctx, s.pool.Address.Hex())
	if err != nil {
		return 0, err
	}
	if snap == nil {
		return 0, nil // 从未持久化过
	}
	s.pool.UpdateFromSwap(snap.SqrtPriceX96, snap.Tick, snap.Liquidity, snap.BlockNumber)
	for tickStr, liqStr := range snap.TickData {
		var tickVal int64
		fmt.Sscanf(tickStr, "%d", &tickVal)
		liqNet, ok := new(big.Int).SetString(liqStr, 10)
		if ok && liqNet.Sign() != 0 {
			s.pool.SetTickLiquidity(int32(tickVal), liqNet)
		}
	}
	s.logger.Info("loaded state from store", "pool", s.pool.Address.Hex(), "block", snap.BlockNumber, "ticks", len(snap.TickData))
	return snap.BlockNumber, nil
}

// emitPriceUpdate 触发价格更新回调（内部方法）。
func (s *PoolQuoteService) emitPriceUpdate() {
	s.mu.RLock()
	fn := s.onPriceUpdate
	s.mu.RUnlock()

	price0, price1, tick, blockNum := s.pool.GetPrices()

	// 更新 Prometheus 指标（小数位调整后的可读价格）
	poolAddr := s.pool.Address.Hex()
	metrics.Price.WithLabelValues(poolAddr).Set(s.pool.HumanPrice())
	metrics.BlockNumber.WithLabelValues(poolAddr).Set(float64(blockNum))

	if fn == nil {
		return
	}

	fn(s.pool.Address, price0, price1, tick)
}
