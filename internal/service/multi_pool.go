package service

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/store"
	"github.com/brianliu-sysu/arbitrage/internal/subgraph"
	"github.com/brianliu-sysu/arbitrage/internal/subscriber"
	"github.com/ethereum/go-ethereum/common"
)

// fullSyncSem 全局全量同步信号量（容量 1）：确保同时只有一个池子在执行昂贵的 Tick Bitmap 重建。
// 启动批量添加池子时避免 RPC 限流。持有者完成后停留 2 秒再释放，给 RPC 提供喘息时间。
var fullSyncSem = make(chan struct{}, 1)

// AcquireFullSyncSlot 获取全量同步槽位（阻塞直到可用）。返回释放函数。
func AcquireFullSyncSlot() func() {
	fullSyncSem <- struct{}{}
	return func() {
		time.Sleep(2 * time.Second) // 释放前等待 2 秒，避免下一个全量同步立即发起
		<-fullSyncSem
	}
}

// MultiPoolService 管理多个 Uniswap V3 池子的报价服务。
//
// 每个池子拥有独立的 WebSocket 连接和事件订阅，互不影响。
// 池子的 token0 / token1 / fee 会在添加时自动通过 RPC 查询，无需手动配置。
type MultiPoolService struct {
	wsEndpoint             string
	rpcEndpoint            string
	services               map[common.Address]*PoolQuoteService
	pathFinder             *PathFinder
	maxHops                int
	bridgeAddrs            []common.Address
	store                  store.Storer
	maxBlockGapForFullSync uint64
	factoryAddr            common.Address
	baseTokenAddrs         []common.Address
	logger                 logx.Logger

	mu            sync.RWMutex
	onPriceUpdate func(poolAddr common.Address, price0In1, price1In0 float64, tick int32)
}

// NewMultiPoolService 创建一个多池子报价服务。
//
// wsEndpoint 为 WebSocket 端点地址，将被所有池子的事件订阅共享。
// rpcEndpoint 为 HTTP RPC 端点，用于 eth_call / eth_getLogs 等只读调用。
// maxHops 为跨池报价最大跳数，bridgeTokens 为中间代币白名单。
func NewMultiPoolService(wsEndpoint, rpcEndpoint string, maxHops int, bridgeTokens []common.Address, maxBlockGapForFullSync uint64, factoryAddr common.Address, baseTokenAddrs []common.Address, logger logx.Logger, st store.Storer) *MultiPoolService {
	return &MultiPoolService{
		wsEndpoint:             wsEndpoint,
		rpcEndpoint:            rpcEndpoint,
		services:               make(map[common.Address]*PoolQuoteService),
		maxHops:                maxHops,
		bridgeAddrs:            bridgeTokens,
		maxBlockGapForFullSync: maxBlockGapForFullSync,
		factoryAddr:            factoryAddr,
		baseTokenAddrs:         baseTokenAddrs,
		store:                  st,
		logger:                 logger,
	}
}

// AddPool 向多池子服务中添加一个池子并立即启动。
//
// poolAddress 为池子合约地址（token0 / token1 / fee 会自动通过 RPC 查询）。
// syncFromBlock 为该池子的历史同步起始区块号，0 表示跳过历史同步。
func (m *MultiPoolService) AddPool(poolAddress common.Address, healthCheckIntervalSec int, syncFromBlock uint64) error {
	addr := poolAddress
	cfg := Config{
		PoolAddress:            addr,
		HealthCheckIntervalSec: healthCheckIntervalSec,
		MaxBlockGapForFullSync: m.maxBlockGapForFullSync,
	}

	m.mu.Lock()
	if _, exists := m.services[addr]; exists {
		m.mu.Unlock()
		return fmt.Errorf("pool %s already added", addr.Hex())
	}

	svc, err := NewPoolQuoteService(m.wsEndpoint, m.rpcEndpoint, cfg, m.logger, m.store)
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("create pool service for %s: %w", addr.Hex(), err)
	}

	m.services[addr] = svc
	m.mu.Unlock()

	// 尝试从数据库恢复状态
	storedBlock, loadErr := svc.LoadFromStore()
	if loadErr == nil && storedBlock > 0 {
		if storedBlock > syncFromBlock {
			syncFromBlock = storedBlock
		}
		m.logger.Info("restored state from database", "pool", addr.Hex(), "block", storedBlock)
	}

	// 通过 RPC 获取 token0 / token1 / fee（Start 前必须）
	if err := svc.ResolvePoolMetadata(); err != nil {
		return fmt.Errorf("resolve pool metadata for %s: %w", addr.Hex(), err)
	}

	// 注册价格更新回调
	m.mu.RLock()
	fn := m.onPriceUpdate
	m.mu.RUnlock()
	if fn != nil {
		svc.SetOnPriceUpdate(fn)
	}

	// 不在锁内执行 Start（会阻塞等待 RPC 返回并启动订阅 goroutine）
	if err := svc.Start(syncFromBlock); err != nil {
		return fmt.Errorf("start pool %s: %w", addr.Hex(), err)
	}

	m.logger.Info("pool added and started", "pool", addr.Hex())

	// 重建路径搜索器，使其包含新添加的池子
	m.rebuildPathFinder()

	return nil
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
	price0In1, price1In0, tick = svc.GetPrice()
	return price0In1, price1In0, tick, true
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

// CrossQuote 执行跨池报价：搜索从 tokenIn 到 tokenOut 的最优路径并报价。
//
// 返回 QuoteResult（包含路径和输出量），如果没有有效路径则返回 nil。
func (m *MultiPoolService) CrossQuote(amountIn *big.Int, tokenIn, tokenOut common.Address) (*QuoteResult, error) {
	m.mu.RLock()
	pf := m.pathFinder
	m.mu.RUnlock()

	if pf == nil {
		return nil, fmt.Errorf("path finder not initialized (no pools added)")
	}

	paths := pf.FindPaths(tokenIn, tokenOut)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no swap path found from %s to %s", tokenIn.Hex(), tokenOut.Hex())
	}

	// 对每条路径报价，选最优
	var bestResult *QuoteResult
	for _, path := range paths {
		currentAmount := new(big.Int).Set(amountIn)
		valid := true

		for _, hop := range path.Hops {
			out, err := hop.Pool.QuoteExactInput(currentAmount, hop.TokenIn)
			if err != nil {
				m.logger.Error("cross-quote hop failed",
					"pool", hop.Pool.pool.Address.Hex(),
					"tokenIn", hop.TokenIn.Hex(),
					"tokenOut", hop.TokenOut.Hex(),
					"err", err)
				valid = false
				break
			}
			currentAmount = out
		}

		if !valid {
			continue
		}

		// 转换为可序列化的 QuoteHop 列表
		hops := make([]QuoteHop, len(path.Hops))
		for i, h := range path.Hops {
			hops[i] = QuoteHop{
				Pool:     h.Pool.pool.Address,
				TokenIn:  h.TokenIn,
				TokenOut: h.TokenOut,
			}
		}

		if bestResult == nil || currentAmount.Cmp(bestResult.AmountOut) > 0 {
			bestResult = &QuoteResult{
				Hops:      hops,
				AmountIn:  new(big.Int).Set(amountIn),
				AmountOut: currentAmount,
				TokenIn:   tokenIn,
				TokenOut:  tokenOut,
			}
		}
	}

	if bestResult == nil {
		return nil, fmt.Errorf("all paths failed to produce a quote")
	}

	m.logger.Info("cross-quote best path",
		"hops", len(bestResult.Hops),
		"amountIn", amountIn.String(),
		"amountOut", bestResult.AmountOut.String())

	return bestResult, nil
}

// rebuildPathFinder 根据当前已注册的池子重建路径搜索器。
func (m *MultiPoolService) rebuildPathFinder() {
	m.pathFinder = NewPathFinder(m.services, m.maxHops, m.bridgeAddrs)
	m.logger.Info("path finder rebuilt", "pools", len(m.services), "maxHops", m.maxHops)
}

// AutoDiscoverPools 通过 Subgraph 查询 Top N 池子并自动添加到监控。
// 会自动跳过已存在的池子。返回新增的池子数量。
func (m *MultiPoolService) AutoDiscoverPools(subgraphURL string, orderBy string, minTVLUSD, minVolumeUSD, maxPools int) int {
	m.logger.Info("auto-discover: querying subgraph for top pools",
		"subgraph", subgraphURL, "orderBy", orderBy, "minTVLUSD", minTVLUSD, "minVolumeUSD", minVolumeUSD, "maxPools", maxPools)

	client := subgraph.NewClient(subgraphURL)
	pools, err := client.FetchTopPools(orderBy, minTVLUSD, minVolumeUSD, maxPools)
	if err != nil {
		m.logger.Error("auto-discover: subgraph query failed", "error", err)
		return 0
	}

	m.logger.Info("auto-discover: subgraph returned pools", "count", len(pools))

	added := 0
	for _, sp := range pools {
		poolAddr := common.HexToAddress(sp.Address)

		m.mu.RLock()
		_, exists := m.services[poolAddr]
		m.mu.RUnlock()
		if exists {
			continue
		}

		// 解析 fee（subgraph 返回的是字符串如 "3000"）
		_ = sp.FeeTier

		if err := m.AddPool(poolAddr, 30, 0); err != nil {
			m.logger.Warn("auto-discover: failed to add pool",
				"pool", sp.Address, "token0", sp.Token0.Symbol, "token1", sp.Token1.Symbol, "error", err)
			continue
		}

		added++
		m.logger.Info("auto-discover: added pool",
			"pool", sp.Address,
			"token0", sp.Token0.Symbol+"("+sp.Token0.ID+")",
			"token1", sp.Token1.Symbol+"("+sp.Token1.ID+")",
			"tvlUSD", sp.TVLUSD, "volumeUSD", sp.VolumeUSD)
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

	var sub *subscriber.Subscriber
	m.mu.RLock()
	for _, svc := range m.services {
		if svc.subscriber != nil {
			sub = svc.subscriber
			break
		}
	}
	m.mu.RUnlock()

	if sub == nil {
		var err error
		sub, err = subscriber.NewSubscriber(m.wsEndpoint, m.rpcEndpoint, common.Address{}, nil, m.logger)
		if err != nil {
			m.logger.Warn("cannot create temporary subscriber for pool discovery", "error", err)
			return
		}
	}

	added := 0
	for _, base := range m.baseTokenAddrs {
		if base == tokenAddr {
			continue
		}
		for _, fee := range commonFeeTiers {
			poolAddr, err := sub.FetchPoolFromFactory(m.factoryAddr, tokenAddr, base, fee)
			if err != nil || poolAddr == (common.Address{}) {
				continue
			}

			// AddPool 内部会检查是否已存在，安全幂等
			if err := m.AddPool(poolAddr, 30, 0); err != nil {
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
