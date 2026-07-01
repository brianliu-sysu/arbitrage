// Package service 编排池子状态管理与事件订阅，对外暴露统一的报价服务接口。
package service

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/cache"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/metrics"
	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/brianliu-sysu/arbitrage/internal/pool/replay"
	"github.com/brianliu-sysu/arbitrage/internal/store"
	"github.com/brianliu-sysu/arbitrage/internal/blockchain"
	"github.com/brianliu-sysu/arbitrage/internal/tracing"
	"github.com/brianliu-sysu/arbitrage/internal/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/robfig/cron/v3"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// PoolQuoteService 是 Uniswap V3 池子报价服务的顶层封装。
// 增量同步由 BlockSync 负责；PoolClient 仅用于 RPC（health check / full sync）。
type PoolQuoteService struct {
	pool       *pool.State
	poolClient *blockchain.PoolClient
	logger     logx.Logger
	store      store.Storer
	tokenCache cache.TokenCache
	logCache   cache.AppliedLogCache
	chainName  string

	fetchCurrentBlockNumber func() (uint64, error) // 测试注入；nil 时使用 poolClient

	mu            sync.RWMutex
	onPriceUpdate func(poolAddr common.Address, price0In1, price1In0 float64, tick int32)

	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgWG     sync.WaitGroup

	// 健康检查
	healthCheckInterval time.Duration
	healthCheckCron     *cron.Cron // cron 定时调度器

	// 全量同步节流
	lastFullSyncTime       time.Time
	fullSyncMinGap         time.Duration // 两次全量同步的最小间隔，默认 5 分钟
	maxBlockGapForFullSync uint64        // 全量同步最大区块间隔，超过此值才重建 tick 地图

	// 增量同步节流（内存 + DB）：
	// - 事件先按 block 聚合
	// - 每个 block 批量应用一次到内存
	// - 应用后立刻同步落库
	// - 当长时间未观测到新 block 时，按最大间隔兜底触发
	snapshotPersistMu        sync.Mutex
	pendingSnapshotBlock     uint64
	pendingSnapshotEvents    []bufferedEvent
	pendingSnapshotFirstSeen time.Time
	lastFlushedSnapshotBlock uint64
	snapshotMaxWriteInterval time.Duration

	// 快照 + 缓冲重放：在全量同步期间缓存 WebSocket 事件，同步完成后重放。
	// syncWindowMu 保证缓冲同步流程串行执行，避免 light/full 并发污染状态。
	syncWindowMu       sync.Mutex
	fullSyncInProgress atomic.Bool     // true 时禁止增量写入触发（时间驱动/跨块触发）
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
func NewPoolQuoteService(wsURL, rpcURL string, cfg Config, logger logx.Logger, st store.Storer, tokenCache cache.TokenCache, logCache cache.AppliedLogCache) (*PoolQuoteService, error) {
	poolState := pool.NewState(cfg.PoolAddress, common.Address{}, common.Address{}, 0)

	maxGap := cfg.MaxBlockGapForFullSync
	if maxGap == 0 {
		maxGap = 100
	}

	svc := &PoolQuoteService{
		pool:                     poolState,
		logger:                   logger,
		store:                    st,
		tokenCache:               tokenCache,
		logCache:                 logCache,
		chainName:                cfg.ChainName,
		healthCheckInterval:      time.Duration(cfg.HealthCheckIntervalSec) * time.Second,
		fullSyncMinGap:           5 * time.Minute,
		maxBlockGapForFullSync:   maxGap,
		snapshotMaxWriteInterval: 3 * time.Second,
	}
	svc.bgCtx, svc.bgCancel = context.WithCancel(context.Background())

	sub, err := blockchain.NewSubscriber(wsURL, rpcURL, cfg.PoolAddress, svc, cfg.MulticallAddress, cfg.QuoterAddress, logger)
	if err != nil {
		return nil, fmt.Errorf("create pool client: %w", err)
	}
	svc.poolClient = sub

	return svc, nil
}

// ResolvePoolMetadata 通过 RPC 获取池子的 token0 / token1 / fee 并设置到内部状态中。
//
// 必须在 Start 之前调用（Start 中的 DoFullSync 会用到 fee 信息）。
func (s *PoolQuoteService) ResolvePoolMetadata() error {
	if s.poolClient == nil {
		return fmt.Errorf("poolClient is nil")
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
	meta, err := s.poolClient.FetchPoolMetadata()
	if err != nil {
		return fmt.Errorf("fetch pool metadata for %s: %w", s.pool.Address.Hex(), err)
	}

	s.pool.SetTokens(meta.Token0, meta.Token1, meta.Fee)
	return nil
}

// Start 启动池子服务。
//
// 增量同步由 BlockSync 负责（SubscribeNewHead → eth_getLogs → replay.ApplyBlock）。
// 此处不启动 WS 事件订阅，仅可选健康检查与快照兜底 flush。
func (s *PoolQuoteService) Start(_ ...uint64) error {
	if s.poolClient == nil {
		return fmt.Errorf("poolClient is nil")
	}

	if err := s.poolClient.Start(); err != nil {
		return fmt.Errorf("start poolClient: %w", err)
	}

	s.emitPriceUpdate()
	s.startSnapshotFlushLoop()
	s.startHealthCheck()
	return nil
}

// Stop 停止事件订阅、健康检查，并释放连接资源。
func (s *PoolQuoteService) Stop() {
	// 先停健康检查
	if s.healthCheckCron != nil {
		s.healthCheckCron.Stop()
	}
	if s.bgCancel != nil {
		s.bgCancel()
	}

	if s.poolClient != nil {
		s.poolClient.Stop()
	}
	s.bgWG.Wait()
}

// ---------------------------------------------------------------------------
// poolClient.EventHandler 接口实现
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

	s.logger.Debug("swap event",
		"block", event.Raw.BlockNumber,
		"amount0", event.Amount0.String(),
		"amount1", event.Amount1.String(),
		"tick", event.Tick,
	)

	// 不影响主流程，但需要记录已应用日志
	if !s.markLogAppliedIfNew(event.Raw) {
		return
	}
	s.stageLiveEvent(bufferedEvent{swap: event})
}

func (s *PoolQuoteService) OnMint(event *pool.MintEvent) {
	// 全量同步期间：缓存事件，不更新正式内存
	if s.tryBufferEvent(bufferedEvent{mint: event}) {
		return
	}
	s.logger.Debug("mint event",
		"block", event.Raw.BlockNumber,
		"owner", event.Owner.Hex(),
		"tickLower", event.TickLower,
		"tickUpper", event.TickUpper,
		"amount", event.Amount.String(),
	)

	// 不影响主流程，但需要记录已应用日志
	if !s.markLogAppliedIfNew(event.Raw) {
		return
	}
	s.stageLiveEvent(bufferedEvent{mint: event})
}

func (s *PoolQuoteService) OnBurn(event *pool.BurnEvent) {
	// 全量同步期间：缓存事件，不更新正式内存
	if s.tryBufferEvent(bufferedEvent{burn: event}) {
		return
	}
	s.logger.Debug("burn event",
		"block", event.Raw.BlockNumber,
		"owner", event.Owner.Hex(),
		"tickLower", event.TickLower,
		"tickUpper", event.TickUpper,
		"amount", event.Amount.String(),
	)

	// 不影响主流程，但需要记录已应用日志
	if !s.markLogAppliedIfNew(event.Raw) {
		return
	}
	s.stageLiveEvent(bufferedEvent{burn: event})
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

		// 兼容“历史回放 + 实时缓冲”并存场景：按 block 升序重放，避免时序倒退。
		sort.SliceStable(buf, func(i, j int) bool {
			return buf[i].blockNumber() < buf[j].blockNumber()
		})

		for _, ev := range buf {
			blockNum := ev.blockNumber()
			if blockNum >= s.snapshotStartBlock {
				if !s.markLogAppliedIfNew(ev.rawLog()) {
					skipped++
					continue
				}
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

func (ev *bufferedEvent) rawLog() types.Log {
	switch {
	case ev.swap != nil:
		return ev.swap.Raw
	case ev.mint != nil:
		return ev.mint.Raw
	case ev.burn != nil:
		return ev.burn.Raw
	}
	return types.Log{}
}

func (s *PoolQuoteService) markLogAppliedIfNew(vLog types.Log) bool {
	if s.logCache == nil {
		return true
	}
	if vLog.TxHash == (common.Hash{}) {
		// 历史/回放日志理论上都包含 txHash；缺失时降级为不过滤，避免误丢事件。
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	isNew, err := s.logCache.MarkAppliedIfNew(
		ctx,
		s.chainName,
		s.pool.Address.Hex(),
		vLog.BlockNumber,
		vLog.TxHash.Hex(),
		vLog.Index,
	)
	if err != nil {
		// Redis 抖动时 fail-open，主流程继续，避免因为缓存异常丢失事件。
		s.logger.Warn("applied-log cache unavailable, skip dedupe",
			"pool", s.pool.Address.Hex(),
			"block", vLog.BlockNumber,
			"index", vLog.Index,
			"error", err,
		)
		return true
	}
	return isNew
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
		s.pool.UpdateTickFromMint(ev.mint.TickLower, ev.mint.TickUpper, ev.mint.Amount, ev.mint.Raw.BlockNumber)
	case ev.burn != nil:
		s.pool.UpdateTickFromBurn(ev.burn.TickLower, ev.burn.TickUpper, ev.burn.Amount, ev.burn.Raw.BlockNumber)
	}
}

func (s *PoolQuoteService) OnError(err error) {
	s.logger.Error("poolClient error", "error", err)
}

// OnReconnected WS 重连回调。池子增量同步由 BlockSync 负责，此处不做同步。
func (s *PoolQuoteService) OnReconnected() {}

// needsInitialFullSync 判断是否从未成功初始化（无 DB 快照 / 无 tick 地图）。
func (s *PoolQuoteService) needsInitialFullSync() bool {
	return s.pool.BlockNumber == 0 || s.pool.GetTickCount() == 0
}

// EnsureInitialState 要求池子已由 cmd/snapshot 写入 READY 快照；arbitrage 不再执行链上全量同步。
func (s *PoolQuoteService) EnsureInitialState() error {
	if !s.needsInitialFullSync() {
		return nil
	}
	return fmt.Errorf("pool %s has no tick snapshot: run cmd/snapshot first", s.pool.Address.Hex())
}

// PoolState 返回池子运行时状态（快照工具使用）。
func (s *PoolQuoteService) PoolState() *pool.State {
	return s.pool
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
		"blockNumber":  state.BlockNumber,
	}
}

// GetPrice 返回当前池子的价格快照。
func (s *PoolQuoteService) GetPrice() (price0In1, price1In0 float64, tick int32) {
	sqrtPriceX96, tick, _, _ := s.pool.GetRawState()

	sqrtF := new(big.Float).SetInt(sqrtPriceX96)
	x96F := new(big.Float).SetInt(utils.X96)
	ratio := new(big.Float).Quo(sqrtF, x96F)
	price0 := new(big.Float).Mul(ratio, ratio)

	price0In1, _ = price0.Float64()
	if price0In1 > 0 {
		price1In0 = 1 / price0In1
	}
	return price0In1, price1In0, tick
}

// DoFullSync 执行全量状态同步（锚点区块模式）。
//
// 核心思路：先确定一个绝对的同步终点区块（锚点），然后所有 RPC 请求统一查询该
// 区块高度的静态快照，最后将内存原子复位到锚点状态。缓冲区内只有严格大于锚点
// 区块的事件才会被回放，确保不会遗漏同步期间到达的新事件。
//
// syncFromBlock 参数保留用于签名兼容，实际逻辑中不再使用。
func (s *PoolQuoteService) DoFullSync(_ ...uint64) error {
	if s.poolClient == nil {
		return fmt.Errorf("poolClient is nil")
	}

	// 1. 确定锚点区块（同步终点）
	targetBlock, err := s.poolClient.FetchBlockNumber()
	if err != nil {
		return fmt.Errorf("fetch target block: %w", err)
	}

	// 2. 获取 targetBlock 时刻的 RPC 静态快照（slot0 + liquidity）
	rpcState, err := s.poolClient.FetchStateViaRPCAtBlock(targetBlock)
	if err != nil {
		return fmt.Errorf("fetch rpc state at block %d: %w", targetBlock, err)
	}

	// 3. 全量重建 Tick Map（同样基于 targetBlock）
	if err := s.RebuildTickMapFromChainAtBlock(targetBlock); err != nil {
		s.logger.Warn("tick map rebuild failed, price/liquidity sync will proceed", "error", err)
	}

	// 4. 原子复位内存到 targetBlock 快照状态
	s.pool.UpdateFromSwap(rpcState.SqrtPriceX96, rpcState.Tick, rpcState.Liquidity, targetBlock)

	// 5. 设置回放下界：只有严格大于 targetBlock 的缓冲事件才需要回放
	s.snapshotStartBlock = targetBlock + 1

	s.lastFullSyncTime = time.Now()
	return nil
}

// DoLightSync 执行轻量同步：仅做状态快照与事件回放，不重建 tick 地图。
//
// 当提供 syncFromBlock 时，采用“锚点快照 + 历史回放”模式：
//  1. 以 syncFromBlock-1 作为锚点读取静态快照
//  2. 回放 [syncFromBlock, tip] 区间全部事件
//
// 这样可以从断线起始区块补齐数据，而不是只拉取当前 tick 快照。
//
// 未提供 syncFromBlock 时，回退到当前块快照模式（兼容旧行为）。
func (s *PoolQuoteService) DoLightSync() error {
	if s.poolClient == nil {
		return fmt.Errorf("poolClient is nil")
	}

	fromBlock := s.pool.BlockNumber

	if fromBlock <= 0 {
		return fmt.Errorf("need a full sync")
	}

	tipBlock, err := s.poolClient.FetchBlockNumber()
	if err != nil {
		return fmt.Errorf("fetch tip block: %w", err)
	}
	if tipBlock >= fromBlock {
		if err := s.poolClient.SyncHistoricalAll(
			new(big.Int).SetUint64(fromBlock),
			new(big.Int).SetUint64(tipBlock),
		); err != nil {
			return fmt.Errorf("historical replay [%d,%d]: %w", fromBlock, tipBlock, err)
		}
	}
	s.snapshotStartBlock = fromBlock
	s.logger.Info("light sync completed with historical replay",
		"fromBlock", fromBlock,
		"tipBlock", tipBlock,
		"tick", s.pool.Tick,
		"ticks", s.pool.GetTickCount(),
	)

	s.emitPriceUpdate()
	s.saveSnapshot() // 轻量同步后持久化，确保 DB 有最新状态
	return nil
}

// RebuildTickMapFromChain 通过 Tick Bitmap 直接从链上重建完整的 tick 流动性地图。
func (s *PoolQuoteService) RebuildTickMapFromChain() error {
	if s.poolClient == nil {
		return fmt.Errorf("poolClient is nil")
	}

	currentTick := s.pool.Tick
	beforeCount := s.pool.GetTickCount()

	tickSpacing, err := s.poolClient.FetchTickSpacing()
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
		batchResults, err := s.poolClient.FetchTickBitmapBatch(windowCandidates)
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
			bitmap, err = s.poolClient.FetchTickBitmap(wordPos)
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
			if tick >= utils.MinTick && tick <= utils.MaxTick {
				ticks = append(ticks, tick)
			}
		}
	}
	return ticks
}

// fetchTicksConcurrently 通过 Multicall3 批量获取所有 tick 的链上数据。
// 一次 RPC 调用（内部自动分块）即可获取所有 tick，避免逐个查询。
// ctx 取消时会提前退出，返回已获取的部分结果。
func (s *PoolQuoteService) fetchTicksConcurrently(ticks []int32) map[int32]*blockchain.TickData {
	if len(ticks) == 0 {
		return nil
	}

	data, err := s.poolClient.FetchTickInfoBatch(ticks)
	if err != nil {
		s.logger.Warn("tick rebuild: batch fetch failed, falling back to empty",
			"total", len(ticks), "error", err)
		return nil
	}

	return data
}

// applyTickData 将从链上获取的 tick 数据原子替换到池子状态中。
// 先构建临时 map，然后一次性替换，避免与并发的 Swap 事件产生竞态。
func (s *PoolQuoteService) applyTickData(data map[int32]*blockchain.TickData) int {
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

// ---------------------------------------------------------------------------
// 锚点区块模式 — Tick Bitmap 重建（AtBlock 变体）
// ---------------------------------------------------------------------------

// RebuildTickMapFromChainAtBlock 在指定区块高度通过 Tick Bitmap 重建完整的 tick 流动性地图。
// 与 RebuildTickMapFromChain 不同，所有 RPC 查询统一使用相同的 targetBlock，确保一致性快照。
func (s *PoolQuoteService) RebuildTickMapFromChainAtBlock(targetBlock uint64) error {
	if s.poolClient == nil {
		return fmt.Errorf("poolClient is nil")
	}

	currentTick := s.pool.Tick
	beforeCount := s.pool.GetTickCount()

	tickSpacing, err := s.poolClient.FetchTickSpacing()
	if err != nil {
		return fmt.Errorf("fetch tick spacing: %w", err)
	}
	wordRange := int32(256) * tickSpacing
	startWord := tickToWord(currentTick, wordRange)

	// 1. 发现所有活跃 word（基于 targetBlock）
	words, wordBitmaps, err := s.discoverActiveWordsAtBlock(startWord, wordRange, 8192, targetBlock)
	if err != nil {
		return fmt.Errorf("discover words at block %d: %w", targetBlock, err)
	}

	// 2. 从已缓存的 bitmap 中提取活跃 tick 索引
	allTicks := s.collectTicksFromWordsCachedAtBlock(words, wordRange, tickSpacing, wordBitmaps, targetBlock)

	// 3. 获取 tick 的链上数据（基于 targetBlock）
	tickDataMap := s.fetchTicksConcurrentlyAtBlock(allTicks, targetBlock)

	// 4. 应用结果
	totalTicks := s.applyTickData(tickDataMap)

	s.logger.Info("tick rebuild complete at block",
		"ticks", totalTicks, "before", beforeCount,
		"tickSpacing", tickSpacing, "currentTick", currentTick, "block", targetBlock)
	return nil
}

// discoverActiveWordsAtBlock 在指定区块高度扫描活跃 tick bitmap word。
func (s *PoolQuoteService) discoverActiveWordsAtBlock(startWord int16, wordRange int32, maxWord int16, blockNumber uint64) ([]int16, map[int16]*big.Int, error) {
	const windowSize int16 = 1000

	var words []int16
	bitmaps := make(map[int16]*big.Int)
	words = append(words, startWord)

	posEdge := startWord
	negEdge := startWord
	posStopped := false
	negStopped := false

	for !posStopped || !negStopped {
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

		batchResults, err := s.poolClient.FetchTickBitmapBatchAtBlock(windowCandidates, blockNumber)
		if err != nil {
			return nil, nil, fmt.Errorf("batch fetch bitmaps at block %d: %w", blockNumber, err)
		}
		for k, v := range batchResults {
			bitmaps[k] = v
		}

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

// collectTicksFromWordsCachedAtBlock 遍历 word bitmap 提取活跃 tick，缺失时通过 at-block RPC 补查。
func (s *PoolQuoteService) collectTicksFromWordsCachedAtBlock(words []int16, wordRange, tickSpacing int32, bitmaps map[int16]*big.Int, blockNumber uint64) []int32 {
	var ticks []int32
	for _, wordPos := range words {
		bitmap, ok := bitmaps[wordPos]
		if !ok {
			var err error
			bitmap, err = s.poolClient.FetchTickBitmapAtBlock(wordPos, blockNumber)
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
			if tick >= utils.MinTick && tick <= utils.MaxTick {
				ticks = append(ticks, tick)
			}
		}
	}
	return ticks
}

// fetchTicksConcurrentlyAtBlock 在指定区块高度批量获取 tick 的链上数据。
func (s *PoolQuoteService) fetchTicksConcurrentlyAtBlock(ticks []int32, blockNumber uint64) map[int32]*blockchain.TickData {
	if len(ticks) == 0 {
		return nil
	}

	data, err := s.poolClient.FetchTickInfoBatchAtBlock(ticks, blockNumber)
	if err != nil {
		s.logger.Warn("tick rebuild: batch fetch at block failed, falling back to empty",
			"total", len(ticks), "block", blockNumber, "error", err)
		return nil
	}

	return data
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

// runHealthCheck 对比链上 slot0 与内存；仅首次无数据时触发全量同步，否则由 BlockSync 增量修复。
func (s *PoolQuoteService) runHealthCheck() {
	if s.poolClient == nil {
		return
	}
	rpcState, err := s.poolClient.FetchStateViaRPC()
	if err != nil {
		s.logger.Warn("health check: RPC fetch failed", "error", err)
		return
	}

	memSqrtPrice, memTick, memLiquidity, _ := s.pool.GetRawState()

	if memTick == rpcState.Tick &&
		memSqrtPrice.Cmp(rpcState.SqrtPriceX96) == 0 &&
		memLiquidity.Cmp(rpcState.Liquidity) == 0 &&
		s.pool.GetTickCount() > 0 {
		return
	}

	if s.needsInitialFullSync() {
		s.logger.Warn("health check: pool missing snapshot, run cmd/snapshot",
			"pool", s.pool.Address.Hex())
		return
	}

	s.logger.Warn("pool state drift detected; block sync should repair via incremental logs",
		"pool", s.pool.Address.Hex(),
		"memBlock", s.pool.BlockNumber,
		"tick", fmt.Sprintf("[%v:%v]", rpcState.Tick, memTick),
		"liquidity", fmt.Sprintf("[%v:%v]", rpcState.Liquidity, memLiquidity),
		"price", fmt.Sprintf("[%v:%v]", rpcState.SqrtPriceX96, memSqrtPrice),
		"tickCount", s.pool.GetTickCount())
}

// beginBufferingWindow 进入缓冲窗口：记录快照起点块高并开启 buffering 模式。
func (s *PoolQuoteService) beginBufferingWindow(reason string) {
	if s.poolClient == nil {
		s.logger.Warn("poolClient is nil, buffering from block 0", "reason", reason)
		s.snapshotStartBlock = 0
		s.bufferingMode.Store(true)
		return
	}

	startBlock, err := s.poolClient.FetchBlockNumber()
	if err != nil {
		s.logger.Warn("failed to fetch snapshot start block, using 0", "reason", reason, "error", err)
	}
	s.snapshotStartBlock = startBlock
	s.bufferingMode.Store(true)
}

// runBufferedSyncWithWindow 将「开启缓冲窗口 + 同步 + 回放」串行化执行，避免 light/full 并发污染状态。
func (s *PoolQuoteService) runBufferedSyncWithWindow(reason string, replayDelay time.Duration, syncFn func() error) error {
	s.syncWindowMu.Lock()
	defer s.syncWindowMu.Unlock()

	s.beginBufferingWindow(reason)
	return s.runBufferedSync(syncFn, replayDelay)
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

// maybeFlushSnapshotByTime 纯时间驱动的快照持久化触发器。
// 当 pending block 在最大写入间隔内一直未落库时，执行一次写入。
func (s *PoolQuoteService) maybeFlushSnapshotByTime() {
	if s.snapshotMaxWriteInterval <= 0 {
		return
	}
	s.flushPendingSnapshotIfDue(0, true)
}

// stageLiveEvent 将实时事件按 block 聚合；当观测到新区块时提交上一个 block。
func (s *PoolQuoteService) stageLiveEvent(ev bufferedEvent) {
	blockNum := ev.blockNumber()
	if blockNum == 0 {
		return
	}

	s.snapshotPersistMu.Lock()
	now := time.Now()
	if s.pendingSnapshotBlock == 0 {
		s.pendingSnapshotBlock = blockNum
		s.pendingSnapshotFirstSeen = now
		s.pendingSnapshotEvents = append(s.pendingSnapshotEvents, ev)
		s.snapshotPersistMu.Unlock()
		return
	}
	if blockNum == s.pendingSnapshotBlock {
		s.pendingSnapshotEvents = append(s.pendingSnapshotEvents, ev)
		s.snapshotPersistMu.Unlock()
		return
	}
	// 进入新区块：先提交上一个 block，再开始聚合当前 block。
	s.snapshotPersistMu.Unlock()
	s.flushPendingSnapshotIfDue(blockNum, false)

	s.snapshotPersistMu.Lock()
	if s.pendingSnapshotBlock == 0 {
		s.pendingSnapshotBlock = blockNum
		s.pendingSnapshotFirstSeen = now
	}
	s.pendingSnapshotEvents = append(s.pendingSnapshotEvents, ev)
	s.snapshotPersistMu.Unlock()
}

// flushPendingSnapshotIfDue 提交 pending block 的批量事件（内存 + DB 同步更新）。
// whenByTime=true 表示时间驱动检查；否则表示事件驱动（通过 nextBlock 判断 block 切换）。
func (s *PoolQuoteService) flushPendingSnapshotIfDue(nextBlock uint64, whenByTime bool) {
	if s.fullSyncInProgress.Load() {
		// 全量同步过程中，不进行定时写入和跨块触发写入。
		return
	}

	var (
		flushEvents []bufferedEvent
		flushBlock  uint64
		rpcBlock    uint64
		hasRPCBlock bool
	)

	if whenByTime {
		var err error
		rpcBlock, err = s.currentBlockNumber()
		if err != nil {
			s.logger.Warn("snapshot flush: fetch current block failed", "error", err)
			return
		}
		hasRPCBlock = true
	}

	s.snapshotPersistMu.Lock()
	if s.pendingSnapshotBlock == 0 || len(s.pendingSnapshotEvents) == 0 {
		s.snapshotPersistMu.Unlock()
		return
	}

	shouldFlush := false
	if whenByTime {
		shouldFlush = hasRPCBlock && rpcBlock != s.pendingSnapshotBlock
	} else {
		shouldFlush = nextBlock != 0 && nextBlock != s.pendingSnapshotBlock
	}

	if shouldFlush {
		flushBlock = s.pendingSnapshotBlock
		flushEvents = append(flushEvents, s.pendingSnapshotEvents...)
		s.pendingSnapshotBlock = 0
		s.pendingSnapshotEvents = nil
		s.pendingSnapshotFirstSeen = time.Time{}
		s.lastFlushedSnapshotBlock = flushBlock
	}
	s.snapshotPersistMu.Unlock()

	if !shouldFlush {
		return
	}

	// 块内按 logIndex 保序应用，确保状态确定性。
	sort.SliceStable(flushEvents, func(i, j int) bool {
		return flushEvents[i].rawLog().Index < flushEvents[j].rawLog().Index
	})
	s.applyEventsBatchAtomically(flushEvents)
	s.emitPriceUpdate()
	s.saveSnapshot()
}

func (s *PoolQuoteService) currentBlockNumber() (uint64, error) {
	if s.fetchCurrentBlockNumber != nil {
		return s.fetchCurrentBlockNumber()
	}
	if s.poolClient == nil {
		return 0, fmt.Errorf("poolClient is nil")
	}
	return s.poolClient.FetchBlockNumber()
}

// startSnapshotFlushLoop 启动后台时间驱动 flush 任务。
func (s *PoolQuoteService) startSnapshotFlushLoop() {
	if s.snapshotMaxWriteInterval <= 0 {
		return
	}

	interval := s.snapshotMaxWriteInterval / 2
	if interval < 200*time.Millisecond {
		interval = 200 * time.Millisecond
	}

	s.runAsync(func(ctx context.Context) {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.maybeFlushSnapshotByTime()
			}
		}
	})
}

// flushPendingSnapshotNow 无条件提交当前 pending block（若存在）。
func (s *PoolQuoteService) flushPendingSnapshotNow() {
	var (
		flushEvents []bufferedEvent
	)
	s.snapshotPersistMu.Lock()
	if len(s.pendingSnapshotEvents) == 0 {
		s.snapshotPersistMu.Unlock()
		return
	}
	flushEvents = append(flushEvents, s.pendingSnapshotEvents...)
	s.pendingSnapshotBlock = 0
	s.pendingSnapshotEvents = nil
	s.pendingSnapshotFirstSeen = time.Time{}
	s.snapshotPersistMu.Unlock()

	sort.SliceStable(flushEvents, func(i, j int) bool {
		return flushEvents[i].rawLog().Index < flushEvents[j].rawLog().Index
	})
	s.applyEventsBatchAtomically(flushEvents)
	s.emitPriceUpdate()
	s.saveSnapshot()
}

// applyEventsBatchAtomically 在临时状态上批量回放事件，再一次性替换正式内存状态。
func (s *PoolQuoteService) applyEventsBatchAtomically(events []bufferedEvent) {
	if len(events) == 0 {
		return
	}

	next := s.pool.GetStateCopy()
	for _, ev := range events {
		applyBufferedEventToState(next, ev)
	}
	s.pool.ReplaceFromState(next)
}

func applyBufferedEventToState(state *pool.State, ev bufferedEvent) {
	if state == nil {
		return
	}
	switch {
	case ev.swap != nil:
		replay.ApplySwap(state, ev.swap)
	case ev.mint != nil:
		replay.ApplyMint(state, ev.mint)
	case ev.burn != nil:
		replay.ApplyBurn(state, ev.burn)
	}
}

// runBufferedFullSync 在缓冲窗口中执行全量同步并回放缓冲事件。
func (s *PoolQuoteService) runBufferedFullSync(replayDelay time.Duration, _ ...uint64) error {
	s.fullSyncInProgress.Store(true)
	defer s.fullSyncInProgress.Store(false)

	return s.runBufferedSyncWithWindow("full-sync", replayDelay, func() error {
		return s.DoFullSync()
	})
}

// runBufferedLightSync 在缓冲窗口中执行轻量同步并回放缓冲事件。
func (s *PoolQuoteService) runBufferedLightSync(replayDelay time.Duration) error {
	return s.runBufferedSyncWithWindow("light-sync", replayDelay, func() error {
		return s.DoLightSync()
	})
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
		Fee:          state.Fee,
		TickData:     make(map[int32]store.TickLiquiditySnapshot),
	}
	for tick, tl := range state.Ticks {
		snap.TickData[tick] = store.TickLiquiditySnapshot{
			LiquidityNet:   new(big.Int).Set(tl.LiquidityNet),
			LiquidityGross: new(big.Int).Set(tl.LiquidityGross),
		}
	}

	select {
	case saveSem <- struct{}{}:
		s.runAsync(func(ctx context.Context) {
			defer func() { <-saveSem }()
			saveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := s.store.Save(saveCtx, snap); err != nil {
				s.logger.Warn("failed to save pool state", "pool", snap.PoolAddress, "error", err)
			}
			// 同时写入历史记录（纯追加），失败不影响主流程
			if err := s.store.SaveHistory(saveCtx, snap); err != nil {
				s.logger.Warn("failed to save pool history", "pool", snap.PoolAddress, "error", err)
			}
		})
	default:
		// 写入太频繁，丢弃本次快照（下一个 swap 会再次触发保存）
		s.logger.Debug("save snapshot skipped (rate limited)", "pool", snap.PoolAddress)
	}
}

func (s *PoolQuoteService) runAsync(fn func(ctx context.Context)) {
	if s.bgCtx == nil {
		s.bgCtx, s.bgCancel = context.WithCancel(context.Background())
	}
	s.bgWG.Add(1)
	utils.SafeGo(s.logger, func() {
		defer s.bgWG.Done()
		select {
		case <-s.bgCtx.Done():
			return
		default:
			fn(s.bgCtx)
		}
	})
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

	if snap.Fee != 0 {
		s.pool.Fee = snap.Fee
	}
	// 恢复 tick 地图
	newTicks := make(map[int32]*pool.TickLiquidity)
	for tick, tickSnap := range snap.TickData {
		liqNet := tickSnap.LiquidityNet
		if liqNet == nil || liqNet.Sign() == 0 {
			continue
		}
		liqGross := tickSnap.LiquidityGross
		if liqGross == nil || liqGross.Sign() < 0 {
			continue
		}
		newTicks[tick] = &pool.TickLiquidity{
			LiquidityNet:   new(big.Int).Set(liqNet),
			LiquidityGross: new(big.Int).Set(liqGross),
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

	// 更新 Prometheus 指标（小数位调整后的可读价格）
	poolAddr := s.pool.Address.Hex()
	metrics.Price.WithLabelValues(poolAddr).Set(float64(s.pool.Tick))
	metrics.BlockNumber.WithLabelValues(poolAddr).Set(float64(s.pool.BlockNumber))

	if fn == nil {
		return
	}

	p0, p1, tick := s.GetPrice()
	fn(s.pool.Address, p0, p1, tick)
}
