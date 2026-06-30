package replay

import (
	"github.com/brianliu-sysu/arbitrage/internal/pool"
)

// ApplyMint 将 Mint 事件应用到池子 tick 地图。
func ApplyMint(p *pool.State, ev *pool.MintEvent) {
	if p == nil || ev == nil {
		return
	}
	p.UpdateTickFromMint(ev.TickLower, ev.TickUpper, ev.Amount, ev.Raw.BlockNumber)
}
