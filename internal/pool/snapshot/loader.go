// Package snapshot 负责池子状态的冷启动：从数据库恢复或从链上扫描初始化。
package snapshot

import (
	"context"
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/brianliu-sysu/arbitrage/internal/storage"
	"github.com/ethereum/go-ethereum/common"
)

// Loader 从 PostgreSQL 恢复池子快照到内存 State。
type Loader struct {
	repo storage.PoolRepo
}

// NewLoader 创建快照加载器。
func NewLoader(repo storage.PoolRepo) *Loader {
	return &Loader{repo: repo}
}

// Restore 将持久化快照应用到 pool.State，返回上次处理的区块号。
func (l *Loader) Restore(ctx context.Context, chainName string, p *pool.State) (uint64, error) {
	if l.repo == nil || p == nil {
		return 0, nil
	}
	snap, err := l.repo.Load(ctx, chainName, p.Address.Hex())
	if err != nil {
		return 0, err
	}
	if snap == nil {
		return 0, nil
	}
	applySnapshot(p, snap)
	return snap.BlockNumber, nil
}

// RestoreAll 加载指定链的全部池子快照。
func (l *Loader) RestoreAll(ctx context.Context, chainName string) (map[string]*storage.PoolSnapshot, error) {
	if l.repo == nil {
		return nil, nil
	}
	return l.repo.LoadAll(ctx, chainName)
}

func applySnapshot(p *pool.State, snap *storage.PoolSnapshot) {
	p.UpdateFromSwap(snap.SqrtPriceX96, snap.Tick, snap.Liquidity, snap.BlockNumber)
	if snap.Fee != 0 {
		p.Fee = snap.Fee
	}
	if snap.Token0Symbol != "" {
		p.Token0Symbol = snap.Token0Symbol
	}
	if snap.Token1Symbol != "" {
		p.Token1Symbol = snap.Token1Symbol
	}

	newTicks := make(map[int32]*pool.TickLiquidity, len(snap.TickData))
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
	p.ReplaceTicks(newTicks)
}

// ApplySnapshot 将快照应用到已有 State（供 bootstrap 使用）。
func ApplySnapshot(p *pool.State, snap *storage.PoolSnapshot) error {
	if p == nil || snap == nil {
		return fmt.Errorf("pool or snapshot is nil")
	}
	applySnapshot(p, snap)
	return nil
}

// RegisterFromSnapshot 用 DB 快照创建并注册到 cache。
func RegisterFromSnapshot(cache *pool.Cache, snap *storage.PoolSnapshot) *pool.State {
	addr := common.HexToAddress(snap.PoolAddress)
	state := pool.NewState(addr, common.Address{}, common.Address{}, snap.Fee)
	applySnapshot(state, snap)
	cache.Set(addr, state)
	return state
}
