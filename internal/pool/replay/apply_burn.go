package replay

import (
	"github.com/brianliu-sysu/arbitrage/internal/pool"
)

// ApplyBurn 将 Burn 事件应用到池子 tick 地图。
func ApplyBurn(p *pool.State, ev *pool.BurnEvent) {
	if p == nil || ev == nil {
		return
	}
	p.UpdateTickFromBurn(ev.TickLower, ev.TickUpper, ev.Amount, ev.Raw.BlockNumber)
}
