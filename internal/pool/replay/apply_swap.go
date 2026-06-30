package replay

import (
	"github.com/brianliu-sysu/arbitrage/internal/pool"
)

// ApplySwap 将 Swap 事件应用到池子状态。
func ApplySwap(p *pool.State, ev *pool.SwapEvent) {
	if p == nil || ev == nil {
		return
	}
	p.UpdateFromSwap(ev.SqrtPriceX96, ev.Tick, ev.Liquidity, ev.Raw.BlockNumber)
}
