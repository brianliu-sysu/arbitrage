package replay

import (
	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/ethereum/go-ethereum/core/types"
)

// FlashEventSignature Uniswap V3 Flash 事件（预留，当前不影响 slot0/liquidity）。
var FlashEventSignature = [32]byte{} // 占位，V3 Flash 不改变 slot0

// ApplyFlash 处理 Flash 事件。Uniswap V3 Flash 不修改 slot0/tick/liquidity，此处为扩展预留。
func ApplyFlash(_ *pool.State, _ types.Log) {
	// V3 Flash 事件仅记录 fee 支付，不影响报价所需的核心状态。
}
