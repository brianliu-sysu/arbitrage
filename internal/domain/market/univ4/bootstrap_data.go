package univ4

import "github.com/brianliu-sysu/uniswapv3/internal/domain/market"

// BootstrapData is on-chain V4 pool state read during cold bootstrap.
type BootstrapData struct {
	Key         PoolKey
	State       market.PoolState
	Ticks       market.TickTable
	Bitmap      market.TickBitmap
	BlockNumber uint64
}
