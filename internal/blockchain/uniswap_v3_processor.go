package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/brianliu-sysu/arbitrage/internal/pool/replay"
	"github.com/brianliu-sysu/arbitrage/internal/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// UniswapV3BlockProcessor 按区块增量同步 Uniswap V3 池子状态。
type UniswapV3BlockProcessor struct {
	chainName string
	cache     *pool.Cache
	fetcher   BlockLogFetcher
	applier   replay.Applier
	poolRepo  storage.PoolRepo
	syncRepo  storage.SyncRepo
	logger    logx.Logger

	mu        sync.RWMutex
	poolAddrs []common.Address
}

// NewUniswapV3BlockProcessor 创建 V3 区块处理器。
func NewUniswapV3BlockProcessor(
	chainName string,
	cache *pool.Cache,
	fetcher BlockLogFetcher,
	applier replay.Applier,
	poolRepo storage.PoolRepo,
	syncRepo storage.SyncRepo,
	logger logx.Logger,
) *UniswapV3BlockProcessor {
	if applier == nil {
		applier = replay.NewDefaultApplier()
	}
	return &UniswapV3BlockProcessor{
		chainName: chainName,
		cache:     cache,
		fetcher:   fetcher,
		applier:   applier,
		poolRepo:  poolRepo,
		syncRepo:  syncRepo,
		logger:    logger,
	}
}

func (p *UniswapV3BlockProcessor) blockApplier() pool.BlockLogApplier {
	return func(st *pool.State, logs []types.Log) error {
		return p.applier.ApplyBlock(st, logs)
	}
}

// SetPoolAddresses 更新跟踪的池子地址列表。
func (p *UniswapV3BlockProcessor) SetPoolAddresses(addrs []common.Address) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.poolAddrs = append([]common.Address(nil), addrs...)
	for _, addr := range addrs {
		if state, ok := p.cache.Get(addr); ok {
			state.SetOnApplied(p.onApplied)
		}
	}
}

func (p *UniswapV3BlockProcessor) onApplied(state *pool.State) {
	if p.poolRepo == nil || state == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.persistPool(ctx, state); err != nil {
		p.logger.Warn("persist pool after apply", "pool", state.Address.Hex(), "error", err)
	}
}

// ChainLastProcessedBlock 返回链级扫块游标（BlockSync 协调用）。
func (p *UniswapV3BlockProcessor) ChainLastProcessedBlock(ctx context.Context) (uint64, error) {
	if p.syncRepo == nil {
		return 0, nil
	}
	return p.syncRepo.GetLastProcessedBlock(ctx, p.chainName)
}

// BackfillPool 拉取 [from, to] 日志：Loading 时 ApplyBlockEventsDirect 同步 apply。
func (p *UniswapV3BlockProcessor) BackfillPool(ctx context.Context, addr common.Address, from, to uint64) error {
	if from > to {
		return nil
	}
	state, ok := p.cache.Get(addr)
	if !ok {
		return fmt.Errorf("pool %s not in cache", addr.Hex())
	}
	apply := p.blockApplier()
	for b := from; b <= to; b++ {
		logs, err := p.fetcher.FetchBlockLogs(ctx, b, []common.Address{addr})
		if err != nil {
			return fmt.Errorf("backfill fetch block %d: %w", b, err)
		}
		if len(logs) == 0 {
			continue
		}
		if err := state.ApplyBlockEventsDirect(b, logs, apply); err != nil {
			return fmt.Errorf("backfill apply block %d pool %s: %w", b, addr.Hex(), err)
		}
	}
	return nil
}

// FinishPoolLoading 回补结束：同步 drain pending buffer。
func (p *UniswapV3BlockProcessor) FinishPoolLoading(ctx context.Context, addr common.Address) error {
	state, ok := p.cache.Get(addr)
	if !ok {
		return fmt.Errorf("pool %s not in cache", addr.Hex())
	}

	if err := state.DrainPendingBlockEvents(ctx, p.blockApplier()); err != nil {
		return fmt.Errorf("drain pending pool %s: %w", addr.Hex(), err)
	}
	if err := p.persistPool(ctx, state); err != nil {
		p.logger.Warn("persist pool after loading complete", "pool", addr.Hex(), "error", err)
	}
	return nil
}

// ProcessBlock 拉取日志并按池子加载状态应用；已加载池同步 apply，加载中池进入 handoff 队列。
func (p *UniswapV3BlockProcessor) ProcessBlock(ctx context.Context, block uint64) error {
	p.mu.RLock()
	addrs := append([]common.Address(nil), p.poolAddrs...)
	p.mu.RUnlock()

	if len(addrs) == 0 {
		return p.commitBlock(ctx, block)
	}

	logs, err := p.fetcher.FetchBlockLogs(ctx, block, addrs)
	if err != nil {
		return err
	}
	if len(logs) > 0 {
		grouped := GroupLogsByPool(logs)
		for addr, poolLogs := range grouped {
			state, ok := p.cache.Get(addr)
			if !ok {
				continue
			}
			if state.ApplyBlockEvents(block, poolLogs) {
				continue
			}
			if err := state.ApplyBlockEventsDirect(block, poolLogs, p.blockApplier()); err != nil {
				return fmt.Errorf("apply block %d pool %s: %w", block, addr.Hex(), err)
			}
		}
	}
	return p.commitBlock(ctx, block)
}

// commitBlock 推进链级扫块游标，仅表示「该区块已对当前跟踪池集合做过 getLogs」，
// 不代表每个池子状态均已更新到该高度（池子真相见 pool_states.block_number）。
func (p *UniswapV3BlockProcessor) commitBlock(ctx context.Context, block uint64) error {
	if p.syncRepo == nil {
		return nil
	}
	return p.syncRepo.SetLastProcessedBlock(ctx, p.chainName, block)
}

func (p *UniswapV3BlockProcessor) persistPool(ctx context.Context, state *pool.State) error {
	if p.poolRepo == nil || state == nil {
		return nil
	}
	copy := state.GetStateCopy()
	snap := stateToSnapshot(p.chainName, copy)
	if err := p.poolRepo.Save(ctx, snap); err != nil {
		return err
	}
	return p.poolRepo.SaveHistory(ctx, snap)
}

func stateToSnapshot(chainName string, s *pool.State) *storage.PoolSnapshot {
	snap := &storage.PoolSnapshot{
		ChainName:    chainName,
		PoolAddress:  s.Address.Hex(),
		BlockNumber:  s.BlockNumber,
		Tick:         s.Tick,
		SqrtPriceX96: new(big.Int).Set(s.SqrtPriceX96),
		Liquidity:    new(big.Int).Set(s.Liquidity),
		Fee:          s.Fee,
		Token0Symbol: s.Token0Symbol,
		Token1Symbol: s.Token1Symbol,
		TickData:     make(map[int32]storage.TickLiquiditySnapshot),
	}
	for tick, tl := range s.Ticks {
		snap.TickData[tick] = storage.TickLiquiditySnapshot{
			LiquidityNet:   new(big.Int).Set(tl.LiquidityNet),
			LiquidityGross: new(big.Int).Set(tl.LiquidityGross),
		}
	}
	return snap
}

// ApplyLogsToPool 将日志应用到指定池子（供 WS 路径复用 replay 层）。
func ApplyLogsToPool(applier replay.Applier, state *pool.State, logs []types.Log) error {
	if applier == nil {
		applier = replay.NewDefaultApplier()
	}
	return applier.ApplyBlock(state, logs)
}
