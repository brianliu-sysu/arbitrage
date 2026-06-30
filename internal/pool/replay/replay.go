package replay

import (
	"fmt"

	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/ethereum/go-ethereum/core/types"
)

// Applier 将链上日志应用到池子状态机。
type Applier interface {
	ApplyBlock(p *pool.State, logs []types.Log) error
}

// DefaultApplier Uniswap V3 默认事件回放器。
type DefaultApplier struct{}

// NewDefaultApplier 创建 V3 回放器。
func NewDefaultApplier() *DefaultApplier {
	return &DefaultApplier{}
}

// ApplyBlock 按日志顺序更新池子状态。
func (a *DefaultApplier) ApplyBlock(p *pool.State, logs []types.Log) error {
	if p == nil {
		return fmt.Errorf("pool state is nil")
	}
	for _, lg := range logs {
		if len(lg.Topics) == 0 {
			continue
		}
		if err := a.applyLog(p, lg); err != nil {
			return fmt.Errorf("apply log tx=%s index=%d: %w", lg.TxHash.Hex(), lg.Index, err)
		}
	}
	return nil
}

func (a *DefaultApplier) applyLog(p *pool.State, lg types.Log) error {
	switch lg.Topics[0] {
	case pool.SwapEventSignature:
		ev, err := pool.ParseSwapEvent(lg)
		if err != nil {
			return err
		}
		ApplySwap(p, ev)
	case pool.MintEventSignature:
		ev, err := pool.ParseMintEvent(lg)
		if err != nil {
			return err
		}
		ApplyMint(p, ev)
	case pool.BurnEventSignature:
		ev, err := pool.ParseBurnEvent(lg)
		if err != nil {
			return err
		}
		ApplyBurn(p, ev)
	default:
		// 忽略非 V3 pool 事件
	}
	return nil
}

// ApplyBlock 便捷函数：使用默认回放器应用区块日志。
func ApplyBlock(p *pool.State, logs []types.Log) error {
	return NewDefaultApplier().ApplyBlock(p, logs)
}
