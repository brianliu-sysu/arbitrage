package clv3

import (
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

// BootstrapData is on-chain V3 pool state read during cold bootstrap.
type BootstrapData struct {
	Token0      common.Address
	Token1      common.Address
	Fee         uint32
	TickSpacing int32
	State       market.PoolState
	Ticks       market.TickTable
	Bitmap      market.TickBitmap
}
