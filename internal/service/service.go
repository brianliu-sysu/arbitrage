// Package service 编排池子状态管理与事件订阅，对外暴露统一的报价服务接口。
package service

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/metrics"
	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/brianliu-sysu/arbitrage/internal/store"
	"github.com/brianliu-sysu/arbitrage/internal/subscriber"
	"github.com/brianliu-sysu/arbitrage/internal/tracing"
	"github.com/brianliu-sysu/arbitrage/internal/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/robfig/cron/v3"
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
	chainName  string

	mu            sync.RWMutex
	onPriceUpdate func(poolAddr common.Address, price0In1, price1In0 float64, tick int32)

	// 健康检查
	healthCheckInterval time.Duration
	healthCheckCron     *cron.Cron // cron 定时调度器

	// 全量同步节流
	lastFullSyncTime       time.Time
	fullSyncMinGap         time.Duration // 两次全量同步的最小间隔，默认 5 分钟
	maxBlockGapForFullSync uint64        // 全量同步最大区块间隔，超过此值才重建 tick 地图

	// 快照 + 缓冲重放：在全量同步期间缓存 WebSocket 事件，同步完成后重放。
	bufferingMode      atomic.Bool     // true 时事件进入缓冲区而非直接更新内存
	eventBufferMu      sync.Mutex      // 保护 eventBuffer
	eventBuffer        []bufferedEvent // 全量同步期间缓存的事件
	snapshotStartBlock uint64          // 全量同步开始时的链上区块高度
}

// bufferedEvent 在全量同步期间缓存的 WebSocket 事件。
// 三个指针中有且只有一个非 nil。
type bufferedEvent struct {
	swap *pool.SwapEvent
	mint *pool.MintEvent
	burn *pool.BurnEvent
}

// Config 创建 PoolQuoteService 所需的配置。
type Config struct {
	ChainName              string         // 链名称（用于持久化 key）
	PoolAddress            common.Address // Uniswap V3 Pool 合约地址
	HealthCheckIntervalSec int            // 健康检查间隔（秒），0 表示禁用
	MaxBlockGapForFullSync uint64         // 全量同步最大区块间隔，0 使用默认值 100
	MulticallAddress       common.Address // Multicall3 合约地址，zero 表示使用标准部署地址
	QuoterAddress          common.Address // Quoter 合约地址，zero 表示禁用报价
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
		chainName:              cfg.ChainName,
		healthCheckInterval:    time.Duration(cfg.HealthCheckIntervalSec) * time.Second,
		fullSyncMinGap:         5 * time.Minute,
		maxBlockGapForFullSync: maxGap,
	}

	sub, err := subscriber.NewSubscriber(wsURL, rpcURL, cfg.PoolAddress, svc, cfg.MulticallAddress, cfg.QuoterAddress, logger)
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
	// 如果 DB 已有元数据，跳过 RPC 查询（从 LoadFromStore 已恢复）
	if s.pool.Token0 != (common.Address{}) && s.pool.Token1 != (common.Address{}) && s.pool.Fee != 0 {
		s.logger.Info("pool metadata already loaded from store",
			"pool", s.pool.Address.Hex(),
			"token0", s.pool.Token0.Hex(),
			"token1", s.pool.Token1.Hex(),
			"fee", s.pool.Fee,
		)
		return nil
	}
	meta, err := s.subscriber.FetchPoolMetadata()
	if err != nil {
		return fmt.Errorf("fetch pool metadata for %s: %w", s.pool.Address.Hex(), err)
	}

	token0Info, err := s.resolveTokenInfo(meta.Token0)
	if err != nil {
		s.logger.Warn("failed to resolve token0 metadata, fallback to local guess",
			"token", meta.Token0.Hex(), "error", err)
	}
	token1Info, err := s.resolveTokenInfo(meta.Token1)
	if err != nil {
		s.logger.Warn("failed to resolve token1 metadata, fallback to local guess",
			"token", meta.Token1.Hex(), "error", err)
	}

	s.pool.SetTokensWithInfo(meta.Token0, meta.Token1, meta.Fee, token0Info, token1Info)
	return nil
}

// resolveTokenInfo 先从数据库缓存读取代币元信息，未命中时通过 RPC 获取并写回缓存。
func (s *PoolQuoteService) resolveTokenInfo(tokenAddr common.Address) (*pool.TokenInfo, error) {
	tokenHex := tokenAddr.Hex()
	if s.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cached, err := s.store.LoadTokenMetadata(ctx, s.chainName, tokenHex)
		cancel()
		if err == nil && cached != nil {
			return &pool.TokenInfo{
				Symbol:   cached.Symbol,
				Decimals: cached.Decimals,
			}, nil
		}
		if err != nil {
			s.logger.Warn("failed to load token metadata cache", "token", tokenHex, "error", err)
		}
	}

	meta, err := s.subscriber.FetchTokenMetadata(tokenAddr)
	if err != nil {
		return nil, err
	}

	if s.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		saveErr := s.store.SaveTokenMetadata(ctx, &store.TokenMetadata{
			ChainName:    s.chainName,
			TokenAddress: tokenHex,
			Symbol:       meta.Symbol,
			Decimals:     meta.Decimals,
		})
		cancel()
		if saveErr != nil {
			s.logger.Warn("failed to save token metadata cache", "token", tokenHex, "error", saveErr)
		}
	}

	return &pool.TokenInfo{
		Symbol:   meta.Symbol,
		Decimals: meta.Decimals,
	}, nil
}

// Start 启动服务：
//  1. 启动实时事件订阅
//  2. 启动定时健康检查（如已配置）
//
// 注意：
//   - 启动主流程不再执行全量同步，优先保证快速启动（DB 状态 + 配置即可上线）。
//   - 全量/轻量修复由定时健康检查与重连流程触发。
func (s *PoolQuoteService) Start(syncFromBlock uint64) error {
	if s.subscriber == nil {
		return fmt.Errorf("subscriber is nil")
	}

	// 启动路径不再使用该参数；保留签名用于兼容上层调用。
	_ = syncFromBlock

	// Step 1: 开启 WebSocket 监听（首连不触发 OnReconnected）。
	if err := s.subscriber.Start(); err != nil {
		return fmt.Errorf("start subscriber: %w", err)
	}

	// 立即发一次回调/指标，便于上层拿到当前（可能来自 DB）的初始状态。
	s.emitPriceUpdate()

	// Step 2: 启动定时健康检查
	s.startHealthCheck()
	return nil
}

// Stop 停止事件订阅、健康检查，并释放连接资源。
func (s *PoolQuoteService) Stop() {
	// 先停健康检查
	if s.healthCheckCron != nil {
		s.healthCheckCron.Stop()
	}

	if s.subscriber != nil {
		s.subscriber.Stop()
	}
}

// ---------------------------------------------------------------------------
// subscriber.EventHandler 接口实现
// ---------------------------------------------------------------------------

func (s *PoolQuoteService) OnSwap(event *pool.SwapEvent) {
	// 全量同步期间：缓存事件，不更新正式内存
	if s.tryBufferEvent(bufferedEvent{swap: event}) {
		s.logger.Debug("swap event buffered",
			"block", event.Raw.BlockNumber,
			"bufferSize", s.bufferLen(),
		)
		return
	}

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

	s.logger.Debug("swap event",
		"block", event.Raw.BlockNumber,
		"amount0", event.Amount0.String(),
		"amount1", event.Amount1.String(),
		"tick", event.Tick,
	)

	s.emitPriceUpdate()
	s.saveSnapshot()
}

func (s *PoolQuoteService) OnMint(event *pool.MintEvent) {
	// 全量同步期间：缓存事件，不更新正式内存
	if s.tryBufferEvent(bufferedEvent{mint: event}) {
		return
	}

	// 仅更新 tick 级流动性地图（总流动性 L 由后续 Swap 事件或 RPC 同步修正）
	s.pool.UpdateTickFromMint(event.TickLower, event.TickUpper, event.Amount)

	s.logger.Debug("mint event",
		"block", event.Raw.BlockNumber,
		"owner", event.Owner.Hex(),
		"tickLower", event.TickLower,
		"tickUpper", event.TickUpper,
		"amount", event.Amount.String(),
		"totalTicks", s.pool.GetTickCount(),
	)
}

func (s *PoolQuoteService) OnBurn(event *pool.BurnEvent) {
	// 全量同步期间：缓存事件，不更新正式内存
	if s.tryBufferEvent(bufferedEvent{burn: event}) {
		return
	}

	// 仅更新 tick 级流动性地图（总流动性 L 由后续 Swap 事件或 RPC 同步修正）
	s.pool.UpdateTickFromBurn(event.TickLower, event.TickUpper, event.Amount)

	s.logger.Debug("burn event",
		"block", event.Raw.BlockNumber,
		"owner", event.Owner.Hex(),
		"tickLower", event.TickLower,
		"tickUpper", event.TickUpper,
		"amount", event.Amount.String(),
		"totalTicks", s.pool.GetTickCount(),
	)
}

// ---------------------------------------------------------------------------
// 事件缓冲（全量同步期间使用）
// ---------------------------------------------------------------------------

// tryBufferEvent 在缓冲模式下将事件追加到缓冲区，并返回是否已缓冲。
func (s *PoolQuoteService) tryBufferEvent(ev bufferedEvent) bool {
	s.eventBufferMu.Lock()
	defer s.eventBufferMu.Unlock()
	if !s.bufferingMode.Load() {
		return false
	}
	s.eventBuffer = append(s.eventBuffer, ev)
	return true
}

// bufferLen 返回当前缓冲区长度（用于日志）。
func (s *PoolQuoteService) bufferLen() int {
	s.eventBufferMu.Lock()
	defer s.eventBufferMu.Unlock()
	return len(s.eventBuffer)
}

// drainAndReplay 消费缓冲区，重放 snapshotStartBlock 及之后的事件，然后关闭缓冲模式。
func (s *PoolQuoteService) drainAndReplay() {
	var replayed, skipped int
	for {
		s.eventBufferMu.Lock()
		// 没有待处理事件时，在锁内关闭缓冲模式，避免“最后一批事件”丢失。
		if len(s.eventBuffer) == 0 {
			s.bufferingMode.Store(false)
			s.eventBufferMu.Unlock()
			break
		}
		buf := s.eventBuffer
		s.eventBuffer = nil
		s.eventBufferMu.Unlock()

		for _, ev := range buf {
			blockNum := ev.blockNumber()
			if blockNum >= s.snapshotStartBlock {
				s.applyBufferedEvent(ev)
				replayed++
			} else {
				skipped++
			}
		}
	}
}

// blockNumber 返回缓冲事件对应的区块号。
func (ev *bufferedEvent) blockNumber() uint64 {
	switch {
	case ev.swap != nil:
		return ev.swap.Raw.BlockNumber
	case ev.mint != nil:
		return ev.mint.Raw.BlockNumber
	case ev.burn != nil:
		return ev.burn.Raw.BlockNumber
	}
	return 0
}

// applyBufferedEvent 将单个缓冲事件应用到正式内存。
func (s *PoolQuoteService) applyBufferedEvent(ev bufferedEvent) {
	switch {
	case ev.swap != nil:
		s.pool.UpdateFromSwap(
			ev.swap.SqrtPriceX96,
			ev.swap.Tick,
			ev.swap.Liquidity,
			ev.swap.Raw.BlockNumber,
		)
	case ev.mint != nil:
		s.pool.UpdateTickFromMint(ev.mint.TickLower, ev.mint.TickUpper, ev.mint.Amount)
	case ev.burn != nil:
		s.pool.UpdateTickFromBurn(ev.burn.TickLower, ev.burn.TickUpper, ev.burn.Amount)
	}
}

func (s *PoolQuoteService) OnError(err error) {
	s.logger.Error("subscriber error", "error", err)
}

// OnReconnected 在 WebSocket 断线重连成功后调用。
//
// 采用与 Start() 相同的快照 + 缓冲重放策略：
//  1. 记录快照起点高度
//  2. 启用缓冲模式（bufferingMode=true）
//  3. 执行全量/轻量同步
//  4. 延迟重放缓冲区事件（等待 runEventLoop 消费 logsCh 积压）
//  5. 切换回实时模式
func (s *PoolQuoteService) OnReconnected() {
	if s.subscriber == nil {
		return
	}

	_, _, _, memBlock := s.pool.GetPrices()
	s.logger.Info("reconnected, syncing state", "pool", s.pool.Address.Hex(), "fromBlock", memBlock)

	// Step 1: 执行全量或轻量同步
	if time.Since(s.lastFullSyncTime) > s.fullSyncMinGap {
		s.logger.Info("full tick rebuild due (last rebuild was %v ago)", "elapsed", time.Since(s.lastFullSyncTime))
		s.beginBufferingWindow("reconnect")
		utils.SafeGo(s.logger, func() {
			// 等待 runEventLoop 将 logsCh 中的积压事件消费到 eventBuffer 后再回放。
			if err := s.runBufferedFullSync(memBlock, 1*time.Second); err != nil {
				s.logger.Error("full sync failed after reconnect", "pool", s.pool.Address.Hex(), "error", err)
			}
		})
	} else {
		s.beginBufferingWindow("reconnect-light")
		utils.SafeGo(s.logger, func() {
			if err := s.runBufferedLightSync(memBlock, 1*time.Second); err != nil {
				s.logger.Error("light sync failed after reconnect", "pool", s.pool.Address.Hex(), "error", err)
			}
		})
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

// QuoteExactInput 精确输入报价：使用内存中的池子状态本地模拟，不访问 RPC。
//
// amountIn - 输入数量（最小单位）
// tokenIn  - 输入代币地址（必须是 token0 或 token1 之一）
func (s *PoolQuoteService) QuoteExactInput(amountIn *big.Int, tokenIn common.Address) (*big.Int, error) {
	poolAddr := s.pool.Address.Hex()

	_, span := tracing.Tracer().Start(context.Background(), "service.quote_exact_input", // Background(): called from gin handler with no propagated ctx
		trace.WithAttributes(
			attribute.String("pool", poolAddr),
			attribute.String("token_in", tokenIn.Hex()),
			attribute.String("amount_in", amountIn.String()),
		),
	)
	defer span.End()

	amountOut, err := s.pool.QuoteExactInput(amountIn, tokenIn)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("local quote for %s: %w", poolAddr, err)
	}
	span.SetAttributes(attribute.String("amount_out", amountOut.String()))
	metrics.QuotesTotal.WithLabelValues(poolAddr, "local").Inc()
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
//
// 竞态保护：如果在全量同步期间收到 Swap 事件：
//   - 价格/流动性：在 tick 重建完成后重新获取 RPC 快照，确保不覆盖并发 Swap 更新
//   - tick 地图：构建到临时 map，验证后原子替换，避免 ClearTicks 和 SetTickLiquidity 之间的窗口
func (s *PoolQuoteService) DoFullSync(syncFromBlock uint64) error {
	if s.subscriber == nil {
		return fmt.Errorf("subscriber is nil")
	}

	// 1. RPC 快照 —— 获取当前链上最新状态（用于判断是否需要重建和定位当前 tick）
	_, _, _, memBlock := s.pool.GetPrices()
	tickCount := s.pool.GetTickCount()
	if memBlock == 0 {
		// 冷启动：先获取一次快照，更新内存中的 tick / sqrtPrice / liquidity，
		// 确保后续 RebuildTickMapFromChain 能从正确的 word 开始扫描。
		initialState, err := s.subscriber.FetchStateViaRPC()
		if err != nil {
			return fmt.Errorf("fetch rpc state: %w", err)
		}
		s.pool.UpdateFromSwap(initialState.SqrtPriceX96, initialState.Tick, initialState.Liquidity, initialState.BlockNumber)
		memBlock = initialState.BlockNumber
	}

	// 需要全量重建的条件：tick 地图为空
	needsRebuild := tickCount == 0

	if needsRebuild {
		release := AcquireFullSyncSlot()
		if err := s.RebuildTickMapFromChain(); err != nil {
			s.logger.Warn("tick map rebuild failed, price/liquidity sync will proceed", "error", err)
		}
		release()
	}

	// 2. 在 tick 重建完成后重新获取 RPC 快照，确保不覆盖重建期间到达的 Swap 事件更新
	// （Swap 事件 → OnSwap → UpdateFromSwap 发生在重建期间，如果用在重建前取的快照就会覆盖）
	rpcState, err := s.subscriber.FetchStateViaRPC()
	if err != nil {
		return fmt.Errorf("fetch rpc state (final): %w", err)
	}

	// 3. 应用 RPC 快照 —— 仅当链上数据比内存新时更新，避免覆盖并发的 Swap 事件
	if rpcState.BlockNumber >= memBlock {
		s.pool.UpdateFromSwap(rpcState.SqrtPriceX96, rpcState.Tick, rpcState.Liquidity, rpcState.BlockNumber)
	}

	s.lastFullSyncTime = time.Now()
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
	tickDataMap := s.fetchTicksConcurrently(allTicks)

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
//
// 使用 Multicall3 批量查询，分窗口逐步向外扩展，避免单次 RPC 调用
// calldata 过大的同时，也避免查询到 maxWord 边界（多数池子活跃 tick 集中
// 在当前 tick 附近）。
func (s *PoolQuoteService) discoverActiveWords(startWord int16, wordRange int32, maxWord int16) ([]int16, map[int16]*big.Int, error) {
	const windowSize int16 = 1000 // 每轮每方向最多查询的 word 数

	var words []int16
	bitmaps := make(map[int16]*big.Int)
	words = append(words, startWord)

	// 两个方向当前窗口边界
	posEdge := startWord // 已扫描到的正方向最远 word
	negEdge := startWord // 已扫描到的负方向最远 word
	posStopped := false
	negStopped := false

	for !posStopped || !negStopped {
		// 收集本窗口的候选 word
		var windowCandidates []int16

		if !posStopped {
			from := posEdge + 1
			to := from + windowSize - 1
			if to > maxWord {
				to = maxWord
			}
			for w := from; w <= to; w++ {
				windowCandidates = append(windowCandidates, w)
			}
		}

		if !negStopped {
			from := negEdge - 1
			to := from - windowSize + 1
			if to < -maxWord {
				to = -maxWord
			}
			for w := from; w >= to; w-- {
				windowCandidates = append(windowCandidates, w)
			}
		}

		if len(windowCandidates) == 0 {
			break
		}

		// 批量查询本窗口所有 word
		batchResults, err := s.subscriber.FetchTickBitmapBatch(windowCandidates)
		if err != nil {
			return nil, nil, fmt.Errorf("batch fetch bitmaps: %w", err)
		}
		for k, v := range batchResults {
			bitmaps[k] = v
		}

		// 正方向：检查连续空 word
		if !posStopped {
			emptyStreak := 0
			var foundWords []int16
			for w := posEdge + 1; w <= maxWord; w++ {
				if _, ok := bitmaps[w]; ok {
					foundWords = append(foundWords, w)
					emptyStreak = 0
				} else {
					emptyStreak++
				}
				posEdge = w
				if emptyStreak >= 3 || w == maxWord {
					break
				}
			}
			words = append(words, foundWords...)
			if emptyStreak >= 3 || posEdge >= maxWord {
				posStopped = true
			}
		}

		// 负方向：检查连续空 word
		if !negStopped {
			emptyStreak := 0
			var foundWords []int16
			for w := negEdge - 1; w >= -maxWord; w-- {
				if _, ok := bitmaps[w]; ok {
					foundWords = append(foundWords, w)
					emptyStreak = 0
				} else {
					emptyStreak++
				}
				negEdge = w
				if emptyStreak >= 3 || w == -maxWord {
					break
				}
			}
			words = append(words, foundWords...)
			if emptyStreak >= 3 || negEdge <= -maxWord {
				negStopped = true
			}
		}
	}

	return words, bitmaps, nil
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

// fetchTicksConcurrently 通过 Multicall3 批量获取所有 tick 的链上数据。
// 一次 RPC 调用（内部自动分块）即可获取所有 tick，避免逐个查询。
// ctx 取消时会提前退出，返回已获取的部分结果。
func (s *PoolQuoteService) fetchTicksConcurrently(ticks []int32) map[int32]*subscriber.TickData {
	if len(ticks) == 0 {
		return nil
	}

	data, err := s.subscriber.FetchTickInfoBatch(ticks)
	if err != nil {
		s.logger.Warn("tick rebuild: batch fetch failed, falling back to empty",
			"total", len(ticks), "error", err)
		return nil
	}

	return data
}

// applyTickData 将从链上获取的 tick 数据原子替换到池子状态中。
// 先构建临时 map，然后一次性替换，避免与并发的 Swap 事件产生竞态。
func (s *PoolQuoteService) applyTickData(data map[int32]*subscriber.TickData) int {
	newTicks := make(map[int32]*pool.TickLiquidity, len(data))
	count := 0
	for tick, d := range data {
		if d.Initialized && d.LiquidityNet.Sign() != 0 {
			newTicks[tick] = &pool.TickLiquidity{LiquidityNet: new(big.Int).Set(d.LiquidityNet)}
			count++
		}
	}
	s.pool.ReplaceTicks(newTicks)
	return count
}

// SyncStateFromRPC 已弃用，请使用 DoFullSync。
// 保留用于向后兼容。
func (s *PoolQuoteService) SyncStateFromRPC() error {
	return s.DoFullSync(0)
}

// startHealthCheck 启动定时健康检查（使用 cron 调度）。
func (s *PoolQuoteService) startHealthCheck() {
	if s.healthCheckInterval <= 0 {
		s.logger.Info("[health] health check disabled (interval = 0)")
		return
	}

	s.healthCheckCron = cron.New(cron.WithSeconds())
	_, err := s.healthCheckCron.AddFunc("@every "+s.healthCheckInterval.String(), func() {
		s.runHealthCheck()
	})
	if err != nil {
		s.logger.Error("failed to schedule health check", "error", err)
		return
	}

	s.healthCheckCron.Start()
	s.logger.Debug("health check started", "interval", s.healthCheckInterval)
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

	metrics.HealthRepairsTotal.WithLabelValues(s.pool.Address.Hex()).Inc()

	// 传入内存中的当前块号作为重放下界，补齐断线期间的缺失事件。
	// 健康修复与启动/重连路径保持一致，统一走「缓冲 + 全量 + 重放」流程。
	s.beginBufferingWindow("health-check")
	if err := s.runBufferedFullSync(memBlock, 0); err != nil {
		s.logger.Error("health check: full sync failed", "error", err)
	}
}

// beginBufferingWindow 进入缓冲窗口：记录快照起点块高并开启 buffering 模式。
func (s *PoolQuoteService) beginBufferingWindow(reason string) {
	startBlock, err := s.subscriber.FetchBlockNumber()
	if err != nil {
		s.logger.Warn("failed to fetch snapshot start block, using 0", "reason", reason, "error", err)
	}
	s.snapshotStartBlock = startBlock
	s.bufferingMode.Store(true)
}

// runBufferedSync 在已开启缓冲窗口的前提下执行同步函数并回放缓冲事件。
// 无论同步是否失败，都会关闭 buffering 并尝试回放，避免服务卡在缓冲模式。
func (s *PoolQuoteService) runBufferedSync(syncFn func() error, replayDelay time.Duration) error {
	err := syncFn()
	if replayDelay > 0 {
		time.Sleep(replayDelay)
	}
	s.drainAndReplay()
	s.emitPriceUpdate()
	s.saveSnapshot()
	return err
}

// runBufferedFullSync 在缓冲窗口中执行全量同步并回放缓冲事件。
func (s *PoolQuoteService) runBufferedFullSync(syncFromBlock uint64, replayDelay time.Duration) error {
	return s.runBufferedSync(func() error {
		return s.DoFullSync(syncFromBlock)
	}, replayDelay)
}

// runBufferedLightSync 在缓冲窗口中执行轻量同步并回放缓冲事件。
func (s *PoolQuoteService) runBufferedLightSync(syncFromBlock uint64, replayDelay time.Duration) error {
	return s.runBufferedSync(func() error {
		return s.DoLightSync(syncFromBlock)
	}, replayDelay)
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
		ChainName:    s.chainName,
		PoolAddress:  state.Address.Hex(),
		BlockNumber:  state.BlockNumber,
		Tick:         state.Tick,
		SqrtPriceX96: state.SqrtPriceX96,
		Liquidity:    state.Liquidity,
		Price0In1:    state.Price0In1,
		Token0Symbol: state.Token0Symbol,
		Token1Symbol: state.Token1Symbol,
		Fee:          state.Fee,
		TickData:     make(map[string]string),
	}
	for tick, tl := range state.Ticks {
		snap.TickData[fmt.Sprintf("%d", tick)] = tl.LiquidityNet.String()
	}

	select {
	case saveSem <- struct{}{}:
		utils.SafeGo(s.logger, func() {
			defer func() { <-saveSem }()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.store.Save(ctx, snap); err != nil {
				s.logger.Warn("failed to save pool state", "pool", snap.PoolAddress, "error", err)
			}
			// 同时写入历史记录（纯追加），失败不影响主流程
			if err := s.store.SaveHistory(ctx, snap); err != nil {
				s.logger.Warn("failed to save pool history", "pool", snap.PoolAddress, "error", err)
			}
		})
	default:
		// 写入太频繁，丢弃本次快照（下一个 swap 会再次触发保存）
		s.logger.Debug("save snapshot skipped (rate limited)", "pool", snap.PoolAddress)
	}
}

// LoadFromStore 从数据库恢复池子状态和元数据，返回上次保存的区块号。
func (s *PoolQuoteService) LoadFromStore(chainName string) (uint64, error) {
	if s.store == nil {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	snap, err := s.store.Load(ctx, chainName, s.pool.Address.Hex())
	if err != nil {
		return 0, err
	}
	if snap == nil {
		return 0, nil
	}
	s.pool.UpdateFromSwap(snap.SqrtPriceX96, snap.Tick, snap.Liquidity, snap.BlockNumber)
	// 从 DB 恢复元数据（token0/token1/fee/symbols 已在 ResolvePoolMetadata 中设置，这里补充符号）
	if snap.Token0Symbol != "" {
		s.pool.Token0Symbol = snap.Token0Symbol
	}
	if snap.Token1Symbol != "" {
		s.pool.Token1Symbol = snap.Token1Symbol
	}
	if snap.Fee != 0 {
		s.pool.Fee = snap.Fee
	}
	// 恢复 tick 地图
	newTicks := make(map[int32]*pool.TickLiquidity)
	for tickStr, liqStr := range snap.TickData {
		var tickVal int64
		fmt.Sscanf(tickStr, "%d", &tickVal)
		liqNet, ok := new(big.Int).SetString(liqStr, 10)
		if ok && liqNet.Sign() != 0 {
			newTicks[int32(tickVal)] = &pool.TickLiquidity{LiquidityNet: liqNet}
		}
	}
	s.pool.ReplaceTicks(newTicks)
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
