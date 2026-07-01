package service

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/arbitrage"
	"github.com/brianliu-sysu/arbitrage/internal/blockchain"
	"github.com/brianliu-sysu/arbitrage/internal/cache"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/brianliu-sysu/arbitrage/internal/quote"
	"github.com/brianliu-sysu/arbitrage/internal/router"
	"github.com/brianliu-sysu/arbitrage/internal/storage"
	"github.com/brianliu-sysu/arbitrage/internal/store"
	"github.com/brianliu-sysu/arbitrage/internal/subgraph"
	"github.com/ethereum/go-ethereum/common"
)

// MultiPoolService 管理多个 Uniswap V3 池子的报价服务。
//
// 增量同步由 BlockSync 负责；池子 RPC 客户端不启用 WS。
type MultiPoolService struct {
	chainName              string
	wsEndpoint             string
	rpcEndpoint            string
	multicallAddr          common.Address
	quoterAddr             common.Address
	services               map[common.Address]*PoolQuoteService
	poolCache              *pool.Cache
	pathFinder             *router.PathFinder
	crossQuoter            *arbitrage.CrossQuoter
	poolUpdater            blockchain.PoolAddressUpdater
	poolBackfiller         blockchain.PoolBackfiller
	poolRepo               storage.PoolRepo
	maxHops                int
	baseTokens             []common.Address // 基础代币列表（跨池报价中间代币 + 自动发现基础代币）
	store                  store.Storer
	tokenCache             cache.TokenCache
	logCache               cache.AppliedLogCache
	maxBlockGapForFullSync uint64
	factoryAddr            common.Address
	logger                 logx.Logger

	mu            sync.RWMutex
	onPriceUpdate func(poolAddr common.Address, price0In1, price1In0 float64, tick int32)
}

// NewMultiPoolService 创建一个多池子报价服务。
//
// wsEndpoint 为 WebSocket 端点地址，将被所有池子的事件订阅共享。
// rpcEndpoint 为 HTTP RPC 端点，用于 eth_call / eth_getLogs 等只读调用。
// maxHops 为跨池报价最大跳数，baseTokens 为基础代币白名单。
func NewMultiPoolService(chainName, wsEndpoint, rpcEndpoint string, maxHops int, baseTokens []common.Address, maxBlockGapForFullSync uint64, factoryAddr, multicallAddr, quoterAddr common.Address, logger logx.Logger, st store.Storer, tokenCache cache.TokenCache, logCache cache.AppliedLogCache, poolCache *pool.Cache) *MultiPoolService {
	if poolCache == nil {
		poolCache = pool.NewCache()
	}
	return &MultiPoolService{
		chainName:              chainName,
		wsEndpoint:             wsEndpoint,
		rpcEndpoint:            rpcEndpoint,
		multicallAddr:          multicallAddr,
		quoterAddr:             quoterAddr,
		services:               make(map[common.Address]*PoolQuoteService),
		poolCache:              poolCache,
		maxHops:                maxHops,
		baseTokens:             baseTokens,
		maxBlockGapForFullSync: maxBlockGapForFullSync,
		factoryAddr:            factoryAddr,
		store:                  st,
		tokenCache:             tokenCache,
		logCache:               logCache,
		logger:                 logger,
	}
}

// PoolEntry 批量添加池子时的单个条目。
type PoolEntry struct {
	PoolAddress            common.Address
	HealthCheckIntervalSec int
	SyncFromBlock          uint64

	PoolSnapshot *store.PoolSnapshot
}

// AddPoolsBatch 批量添加池子。
//
// 所有池子添加完成后只重建一次路径搜索器，避免 O(n) 次 PathFinder 重建。
func (m *MultiPoolService) AddPoolsBatch(entries []PoolEntry) error {
	added := 0
	for _, pc := range entries {
		if err := m.addPool(pc.PoolAddress, pc.HealthCheckIntervalSec, pc.SyncFromBlock, pc.PoolSnapshot); err != nil {
			return fmt.Errorf("add pool %s: %w", pc.PoolAddress.Hex(), err)
		}
		added++
	}

	if added > 0 {
		m.rebuildPathFinder()
		m.notifyPoolAddresses()
	}
	return nil
}

// addPool 核心逻辑：创建/恢复/启动一个池子，不重建路径搜索器（由调用方负责）。
// preloaded 可为 nil。
func (m *MultiPoolService) addPool(poolAddress common.Address, healthCheckIntervalSec int, syncFromBlock uint64, preloaded *store.PoolSnapshot) error {
	addr := poolAddress
	cfg := Config{
		ChainName:              m.chainName,
		PoolAddress:            addr,
		HealthCheckIntervalSec: healthCheckIntervalSec,
		MaxBlockGapForFullSync: m.maxBlockGapForFullSync,
		MulticallAddress:       m.multicallAddr,
		QuoterAddress:          m.quoterAddr,
	}

	m.mu.Lock()
	if _, exists := m.services[addr]; exists {
		m.mu.Unlock()
		return fmt.Errorf("pool %s already added", addr.Hex())
	}
	m.mu.Unlock()

	if preloaded == nil && m.poolRepo != nil {
		loadCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		snap, err := m.poolRepo.Load(loadCtx, m.chainName, strings.ToLower(addr.Hex()))
		cancel()
		if err != nil {
			return fmt.Errorf("load pool %s: %w", addr.Hex(), err)
		}
		if snap == nil || snap.SnapshotStatus != storage.SnapshotReady {
			return fmt.Errorf("pool %s is not READY (run cmd/snapshot first)", addr.Hex())
		}
		preloaded = storageSnapshotToStore(snap)
	}

	svc, err := NewPoolQuoteService(m.wsEndpoint, m.rpcEndpoint, cfg, m.logger, m.store, m.tokenCache, m.logCache)
	if err != nil {
		return fmt.Errorf("create pool service for %s: %w", addr.Hex(), err)
	}

	// 恢复持久化状态
	if preloaded != nil {
		// 批量模式：直接用预加载的快照恢复，跳过 DB 查询
		svc.pool.UpdateFromSwap(preloaded.SqrtPriceX96, preloaded.Tick, preloaded.Liquidity, preloaded.BlockNumber)
		svc.pool.Fee = preloaded.Fee
		if len(preloaded.TickData) > 0 {
			newTicks := make(map[int32]*pool.TickLiquidity, len(preloaded.TickData))
			for tick, tickSnap := range preloaded.TickData {
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
			svc.pool.ReplaceTicks(newTicks)
		}
		if preloaded.BlockNumber > syncFromBlock {
			syncFromBlock = preloaded.BlockNumber
		}
	}

	// 通过 RPC 获取 token0 / token1 / fee（Start 前必须）
	if err := svc.ResolvePoolMetadata(); err != nil {
		return fmt.Errorf("resolve pool metadata for %s: %w", addr.Hex(), err)
	}

	// 仅首次无任何持久化状态时全量同步；之后由 BlockSync 按区块增量补齐。
	if err := svc.EnsureInitialState(); err != nil {
		return fmt.Errorf("initial state for %s: %w", addr.Hex(), err)
	}

	// 注册价格更新回调
	m.mu.RLock()
	fn := m.onPriceUpdate
	m.mu.RUnlock()
	if fn != nil {
		svc.SetOnPriceUpdate(fn)
	}

	// 不在锁内执行 Start（会阻塞等待 RPC 返回并启动订阅 goroutine）
	if err := svc.Start(); err != nil {
		return fmt.Errorf("start pool %s: %w", addr.Hex(), err)
	}

	// 初始化成功后再注册，避免 map 中出现半初始化实例。
	m.mu.Lock()
	if _, exists := m.services[addr]; exists {
		m.mu.Unlock()
		svc.Stop()
		return fmt.Errorf("pool %s already added", addr.Hex())
	}
	m.services[addr] = svc
	svc.pool.BeginLoading()
	m.poolCache.Set(addr, svc.pool)
	m.mu.Unlock()

	if err := m.catchUpAndRegisterPool(addr); err != nil {
		m.RemovePool(addr)
		return fmt.Errorf("register pool %s for block sync: %w", addr.Hex(), err)
	}

	return nil
}

// SetPoolRepo 设置池子状态仓库（用于 READY 状态轮询）。
func (m *MultiPoolService) SetPoolRepo(repo storage.PoolRepo) {
	m.mu.Lock()
	m.poolRepo = repo
	m.mu.Unlock()
}

// StartPoolStatusWatcher 定期根据 pool_states.snapshot_status 增删跟踪池子。
func (m *MultiPoolService) StartPoolStatusWatcher(ctx context.Context, interval time.Duration, healthCheckIntervalSec int) {
	if m.poolRepo == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			syncCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			if err := m.SyncPoolRegistry(syncCtx, healthCheckIntervalSec); err != nil {
				m.logger.Warn("pool status sync failed", "chain", m.chainName, "error", err)
			}
			cancel()
		}
	}
}

// SyncPoolRegistry 将内存跟踪池子与 DB 中 READY 状态对齐。
func (m *MultiPoolService) SyncPoolRegistry(ctx context.Context, healthCheckIntervalSec int) error {
	if m.poolRepo == nil {
		return nil
	}

	statuses, err := m.poolRepo.ListSnapshotStatuses(ctx, m.chainName)
	if err != nil {
		return err
	}

	readySnaps, err := m.poolRepo.LoadAllByStatus(ctx, m.chainName, storage.SnapshotReady)
	if err != nil {
		return err
	}

	var changed bool
	for _, addr := range m.TrackedPoolAddresses() {
		key := strings.ToLower(addr.Hex())
		if statuses[key] != storage.SnapshotReady {
			if m.RemovePool(addr) {
				changed = true
				m.logger.Info("pool removed (not READY)", "pool", addr.Hex(), "status", statuses[key])
			}
		}
	}

	for addrStr, snap := range readySnaps {
		addr := common.HexToAddress(addrStr)
		m.mu.RLock()
		_, exists := m.services[addr]
		m.mu.RUnlock()
		if exists {
			continue
		}
		if err := m.addPool(addr, healthCheckIntervalSec, snap.BlockNumber, storageSnapshotToStore(snap)); err != nil {
			m.logger.Warn("add READY pool failed", "pool", addrStr, "error", err)
			continue
		}
		changed = true
		m.logger.Info("pool added (READY)", "pool", addrStr)
	}

	if changed {
		m.rebuildPathFinder()
		m.notifyPoolAddresses()
	}
	return nil
}

// RemovePool 停止并移除池子（状态非 READY 时由轮询调用）。
func (m *MultiPoolService) RemovePool(addr common.Address) bool {
	m.mu.Lock()
	svc, ok := m.services[addr]
	if ok {
		delete(m.services, addr)
	}
	m.mu.Unlock()
	if !ok {
		return false
	}
	m.notifyPoolAddresses()
	svc.Stop()
	m.poolCache.Delete(addr)
	return true
}

func storageSnapshotToStore(s *storage.PoolSnapshot) *store.PoolSnapshot {
	if s == nil {
		return nil
	}
	tickData := make(map[int32]store.TickLiquiditySnapshot, len(s.TickData))
	for tick, t := range s.TickData {
		tickData[tick] = store.TickLiquiditySnapshot{
			LiquidityNet:   t.LiquidityNet,
			LiquidityGross: t.LiquidityGross,
		}
	}
	return &store.PoolSnapshot{
		ChainName:    s.ChainName,
		PoolAddress:  s.PoolAddress,
		BlockNumber:  s.BlockNumber,
		Tick:         s.Tick,
		SqrtPriceX96: s.SqrtPriceX96,
		Liquidity:    s.Liquidity,
		Price0In1:    s.Price0In1,
		Token0Symbol: s.Token0Symbol,
		Token1Symbol: s.Token1Symbol,
		Fee:          s.Fee,
		TickData:     tickData,
	}
}

// SetPoolUpdater 设置 BlockProcessor 池子地址更新器（BlockSync 注册后调用）。
func (m *MultiPoolService) SetPoolUpdater(u blockchain.PoolAddressUpdater) {
	m.mu.Lock()
	m.poolUpdater = u
	if bf, ok := u.(blockchain.PoolBackfiller); ok {
		m.poolBackfiller = bf
	}
	m.mu.Unlock()
	m.notifyPoolAddresses()
}

// catchUpAndRegisterPool 按 per-pool 加载协议：notify → backfill（Direct apply）→ drain pending。
// 增量事件经 ApplyBlockEvents 写入 pending buffer，FinishPoolLoading 同步消费。
func (m *MultiPoolService) catchUpAndRegisterPool(addr common.Address) error {
	m.mu.RLock()
	bf := m.poolBackfiller
	m.mu.RUnlock()

	state, ok := m.poolCache.Get(addr)
	if !ok {
		return fmt.Errorf("pool %s not in cache", addr.Hex())
	}
	if !state.Loading() {
		state.BeginLoading()
	}

	// 1. 先注册到 getLogs 列表（Loaded=true 时 ProcessBlock 只缓冲）
	m.notifyPoolAddresses()

	if bf == nil {
		state.CompleteLoading()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	poolBlock := m.poolBlockNumber(addr)
	chainLast, err := bf.ChainLastProcessedBlock(ctx)
	if err != nil {
		return fmt.Errorf("read chain cursor: %w", err)
	}
	backfilledTo := poolBlock

	// 2. 回补历史（Loaded=true 时 ApplyBlockEventsDirect 同步 apply，live 事件进 pending buffer）
	if poolBlock < chainLast {
		m.logger.Info("pool catch-up backfill", "pool", addr.Hex(), "from", poolBlock+1, "to", chainLast)
		if err := bf.BackfillPool(ctx, addr, poolBlock+1, chainLast); err != nil {
			return fmt.Errorf("backfill [%d,%d]: %w", poolBlock+1, chainLast, err)
		}
		backfilledTo = chainLast
	}

	// 回填期间链级游标可能继续前进；加载完成前再补一段，重复日志会在消费队列时按 BlockNumber 丢弃。
	latestChainLast, err := bf.ChainLastProcessedBlock(ctx)
	if err != nil {
		return fmt.Errorf("read latest chain cursor: %w", err)
	}
	if backfilledTo < latestChainLast {
		m.logger.Info("pool catch-up final backfill", "pool", addr.Hex(), "from", backfilledTo+1, "to", latestChainLast)
		if err := bf.BackfillPool(ctx, addr, backfilledTo+1, latestChainLast); err != nil {
			return fmt.Errorf("final backfill [%d,%d]: %w", backfilledTo+1, latestChainLast, err)
		}
	}

	// 3. 同步消费 pending buffer（block <= BlockNumber 丢弃）
	loader, ok := bf.(blockchain.PoolLoader)
	if !ok {
		state.CompleteLoading()
		return nil
	}
	if err := loader.FinishPoolLoading(ctx, addr); err != nil {
		return fmt.Errorf("finish pool loading: %w", err)
	}
	return nil
}

func (m *MultiPoolService) poolBlockNumber(addr common.Address) uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if st, ok := m.poolCache.Get(addr); ok {
		return st.BlockNumber
	}
	return 0
}

// catchUpPool 将单池从 poolBlock+1 追到链级游标（仅回填，不 notify）。
func (m *MultiPoolService) catchUpPool(addr common.Address, _ uint64) {
	m.mu.RLock()
	bf := m.poolBackfiller
	m.mu.RUnlock()
	if bf == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	poolBlock := m.poolBlockNumber(addr)
	chainLast, err := bf.ChainLastProcessedBlock(ctx)
	if err != nil {
		m.logger.Warn("pool catch-up failed", "pool", addr.Hex(), "error", err)
		return
	}
	if poolBlock >= chainLast {
		return
	}
	if err := bf.BackfillPool(ctx, addr, poolBlock+1, chainLast); err != nil {
		m.logger.Warn("pool catch-up failed", "pool", addr.Hex(), "error", err)
	}
}

func (m *MultiPoolService) notifyPoolAddresses() {
	m.mu.RLock()
	u := m.poolUpdater
	m.mu.RUnlock()
	if u != nil {
		u.SetPoolAddresses(m.TrackedPoolAddresses())
	}
}

// PoolCache 返回链级池子缓存（Quote / BlockProcessor 共享）。
func (m *MultiPoolService) PoolCache() *pool.Cache {
	return m.poolCache
}

// TrackedPoolAddresses 返回当前跟踪的池子地址。
func (m *MultiPoolService) TrackedPoolAddresses() []common.Address {
	m.mu.RLock()
	defer m.mu.RUnlock()
	addrs := make([]common.Address, 0, len(m.services))
	for addr := range m.services {
		addrs = append(addrs, addr)
	}
	return addrs
}

// StopAll 停止所有池子。
func (m *MultiPoolService) StopAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for addr, svc := range m.services {
		svc.Stop()
		m.logger.Info("stopped pool", "pool", addr.Hex())
	}

	m.logger.Info("all pools stopped")
}

// SetOnPriceUpdate 设置价格更新回调，同时传播到所有已添加的池子。
func (m *MultiPoolService) SetOnPriceUpdate(fn func(poolAddr common.Address, price0In1, price1In0 float64, tick int32)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.onPriceUpdate = fn

	for _, svc := range m.services {
		svc.SetOnPriceUpdate(fn)
	}
}

// QuoteExactInput 对指定池子执行报价。
func (m *MultiPoolService) QuoteExactInput(poolAddr common.Address, amountIn *big.Int, tokenIn common.Address) (*big.Int, error) {
	m.mu.RLock()
	svc, ok := m.services[poolAddr]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("pool %s not found", poolAddr.Hex())
	}
	return svc.QuoteExactInput(amountIn, tokenIn)
}

// GetPrice 获取指定池子的当前现货价格。
func (m *MultiPoolService) GetPrice(poolAddr common.Address) (price0In1, price1In0 float64, tick int32, ok bool) {
	m.mu.RLock()
	svc, exists := m.services[poolAddr]
	m.mu.RUnlock()

	if !exists {
		return 0, 0, 0, false
	}

	p0, p1, t := svc.GetPrice()
	return p0, p1, t, true
}

// GetAllPoolInfo 获取所有池子的基本信息，用于调试与监控。
func (m *MultiPoolService) GetAllPoolInfo() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]map[string]interface{}, 0, len(m.services))
	for _, svc := range m.services {
		result = append(result, svc.GetPoolInfo())
	}
	return result
}

// ---------------------------------------------------------------------------
// 跨池报价
// ---------------------------------------------------------------------------

// CrossQuote 执行跨池报价（arbitrage 层）。
func (m *MultiPoolService) CrossQuote(amountIn *big.Int, tokenIn, tokenOut common.Address) (*quote.Result, error) {
	m.mu.RLock()
	cq := m.crossQuoter
	m.mu.RUnlock()
	if cq == nil {
		return nil, fmt.Errorf("cross quoter not initialized (no pools added)")
	}
	return cq.Quote(amountIn, tokenIn, tokenOut)
}

// rebuildPathFinder 根据 pool cache 重建路径搜索器。
func (m *MultiPoolService) rebuildPathFinder() {
	pf := router.NewPathFinder(m.poolCache, m.maxHops, m.baseTokens)
	m.pathFinder = pf
	m.crossQuoter = arbitrage.NewCrossQuoter(m.poolCache, pf, m.logger)
}

// AutoDiscoverPools 通过 Subgraph 查询 Top N 池子并自动添加到监控。
// 已存在的池子自动跳过。新增池子后单次重建路径搜索器。
func (m *MultiPoolService) AutoDiscoverPools(subgraphURL string, orderBy string, minTVLUSD, minVolumeUSD, maxPools int) int {
	client := subgraph.NewClient(subgraphURL)
	pools, err := client.FetchTopPools(orderBy, minTVLUSD, minVolumeUSD, maxPools)
	if err != nil {
		m.logger.Error("auto-discover: subgraph query failed", "error", err)
		return 0
	}

	added := 0
	for _, sp := range pools {
		poolAddr := common.HexToAddress(sp.Address)

		m.mu.RLock()
		_, exists := m.services[poolAddr]
		m.mu.RUnlock()
		if exists {
			continue
		}

		if err := m.addPool(poolAddr, 30, 0, nil); err != nil {
			m.logger.Warn("auto-discover: failed to add pool",
				"pool", sp.Address, "token0", sp.Token0.Symbol, "token1", sp.Token1.Symbol, "error", err)
			continue
		}
		added++
	}

	if added > 0 {
		m.rebuildPathFinder()
		m.notifyPoolAddresses()
	}
	m.logger.Info("auto-discover complete", "total", len(pools), "added", added)
	return added
}

// ---------------------------------------------------------------------------
// 动态池子发现
// ---------------------------------------------------------------------------

// EnsurePoolForToken 通过 Uniswap V3 Factory 查找 token 与 WETH/USDC/USDT 的所有池子。
// 一个 token 可以有多个池子（不同 base token、不同 fee tier），全部发现并添加。
// AddPool 内部会检查 pool address 是否已存在，避免重复添加。
func (m *MultiPoolService) EnsurePoolForToken(tokenAddr common.Address) {
	m.discoverAndAddPools(tokenAddr)
}

// discoverAndAddPools 遍历 token × {WETH,USDC,USDT} × {500,3000,10000}，
// 通过 Factory 查询所有存在的池子并动态添加。
func (m *MultiPoolService) discoverAndAddPools(tokenAddr common.Address) {
	if tokenAddr == (common.Address{}) {
		return
	}

	var client *blockchain.PoolClient
	m.mu.RLock()
	for _, svc := range m.services {
		if svc.poolClient != nil {
			client = svc.poolClient
			break
		}
	}
	m.mu.RUnlock()

	if client == nil {
		var err error
		client, err = blockchain.NewSubscriber(m.wsEndpoint, m.rpcEndpoint, common.Address{}, nil, common.Address{}, m.quoterAddr, m.logger)
		if err != nil {
			m.logger.Warn("cannot create temporary subscriber for pool discovery", "error", err)
			return
		}
	}

	added := 0
	for _, base := range m.baseTokens {
		if base == tokenAddr {
			continue
		}
		for _, fee := range commonFeeTiers {
			poolAddr, err := client.FetchPoolFromFactory(m.factoryAddr, tokenAddr, base, fee)
			if err != nil || poolAddr == (common.Address{}) {
				continue
			}

			// addPool 内部会检查是否已存在，安全幂等
			if err := m.addPool(poolAddr, 30, 0, nil); err != nil {
				// "already added" 不是错误，其他错误打日志
				m.logger.Warn("failed to add discovered pool", "pool", poolAddr.Hex(), "error", err)
				continue
			}

			m.logger.Info("discovered and added pool via factory",
				"pool", poolAddr.Hex(), "token", tokenAddr.Hex(),
				"baseToken", base.Hex(), "fee", fee.String())
			added++
		}
	}

	if added > 0 {
		m.rebuildPathFinder()
		m.notifyPoolAddresses()
	}
	if added == 0 {
		m.logger.Info("no new pools discovered for token", "token", tokenAddr.Hex())
	} else {
		m.logger.Info("pool discovery complete", "token", tokenAddr.Hex(), "added", added)
	}
}

var commonFeeTiers = []*big.Int{
	big.NewInt(500),
	big.NewInt(3000),
	big.NewInt(10000),
}
