package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"sync"

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
	fetcher   *LogFetcher
	applier   replay.Applier
	poolRepo  storage.PoolRepo
	syncRepo  storage.SyncRepo
	logger    logx.Logger

	mu          sync.RWMutex
	poolAddrs   []common.Address
}

// NewUniswapV3BlockProcessor 创建 V3 区块处理器。
func NewUniswapV3BlockProcessor(
	chainName string,
	cache *pool.Cache,
	fetcher *LogFetcher,
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

// SetPoolAddresses 更新跟踪的池子地址列表。
func (p *UniswapV3BlockProcessor) SetPoolAddresses(addrs []common.Address) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.poolAddrs = append([]common.Address(nil), addrs...)
}

// ChainLastProcessedBlock 返回链级扫块游标（BlockSync 协调用）。
func (p *UniswapV3BlockProcessor) ChainLastProcessedBlock(ctx context.Context) (uint64, error) {
	if p.syncRepo == nil {
		return 0, nil
	}
	return p.syncRepo.GetLastProcessedBlock(ctx, p.chainName)
}

// BackfillPool 回放 [from, to] 内单池日志并持久化，不推进链级游标。
// 动态加池时在加入 getLogs 过滤列表之前调用，避免与 ProcessBlock 并发写同一状态。
func (p *UniswapV3BlockProcessor) BackfillPool(ctx context.Context, addr common.Address, from, to uint64) error {
	if from > to {
		return nil
	}
	state, ok := p.cache.Get(addr)
	if !ok {
		return fmt.Errorf("pool %s not in cache", addr.Hex())
	}
	for b := from; b <= to; b++ {
		logs, err := p.fetcher.FetchBlockLogs(ctx, b, []common.Address{addr})
		if err != nil {
			return fmt.Errorf("backfill fetch block %d: %w", b, err)
		}
		if len(logs) == 0 {
			continue
		}
		if err := p.applier.ApplyBlock(state, logs); err != nil {
			return fmt.Errorf("backfill apply block %d pool %s: %w", b, addr.Hex(), err)
		}
		if err := p.persistPool(ctx, state); err != nil {
			p.logger.Warn("backfill persist pool", "pool", addr.Hex(), "block", b, "error", err)
		}
	}
	return nil
}

// ProcessBlock 拉取日志、回放状态、持久化并推进链级扫块游标。
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
	if len(logs) == 0 {
		return p.commitBlock(ctx, block)
	}

	grouped := GroupLogsByPool(logs)
	for addr, poolLogs := range grouped {
		state, ok := p.cache.Get(addr)
		if !ok {
			continue
		}
		if err := p.applier.ApplyBlock(state, poolLogs); err != nil {
			return fmt.Errorf("apply block %d pool %s: %w", block, addr.Hex(), err)
		}
		if err := p.persistPool(ctx, state); err != nil {
			p.logger.Warn("persist pool after block", "pool", addr.Hex(), "block", block, "error", err)
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
