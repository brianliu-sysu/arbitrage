package v4

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// EventKind identifies PoolManager log types for a V4 pool.
type EventKind int

const (
	EventKindInitialize EventKind = iota + 1
	EventKindSwap
	EventKindModifyLiquidity
)

// EventMeta holds common metadata for every PoolManager pool log.
type EventMeta struct {
	PoolID      PoolID
	BlockNumber uint64
	TxIndex     uint
	LogIndex    uint
}

// InitializeEvent is emitted when a pool is initialized in PoolManager.
type InitializeEvent struct {
	SqrtPriceX96 *big.Int
	Tick         int32
}

// SwapEvent is emitted when a swap changes pool price and active liquidity.
type SwapEvent struct {
	Sender       common.Address
	Amount0      *big.Int
	Amount1      *big.Int
	SqrtPriceX96 *big.Int
	Liquidity    *big.Int
	Tick         int32
	Fee          uint32
}

// ModifyLiquidityEvent is emitted when liquidity is added or removed.
type ModifyLiquidityEvent struct {
	Sender         common.Address
	TickLower      int32
	TickUpper      int32
	LiquidityDelta *big.Int
	Salt           common.Hash
}

// PoolEvent is an immutable on-chain fact for a V4 pool.
type PoolEvent struct {
	Meta EventMeta
	Kind EventKind

	Initialize       *InitializeEvent
	Swap             *SwapEvent
	ModifyLiquidity  *ModifyLiquidityEvent
}

func NewInitializeEvent(meta EventMeta, sqrtPriceX96 *big.Int, tick int32) PoolEvent {
	return PoolEvent{
		Meta: meta,
		Kind: EventKindInitialize,
		Initialize: &InitializeEvent{
			SqrtPriceX96: cloneInt(sqrtPriceX96),
			Tick:         tick,
		},
	}
}

func NewSwapEvent(
	meta EventMeta,
	sender common.Address,
	amount0, amount1, sqrtPriceX96, liquidity *big.Int,
	tick int32,
	fee uint32,
) PoolEvent {
	return PoolEvent{
		Meta: meta,
		Kind: EventKindSwap,
		Swap: &SwapEvent{
			Sender:       sender,
			Amount0:      cloneInt(amount0),
			Amount1:      cloneInt(amount1),
			SqrtPriceX96: cloneInt(sqrtPriceX96),
			Liquidity:    cloneInt(liquidity),
			Tick:         tick,
			Fee:          fee,
		},
	}
}

func NewModifyLiquidityEvent(
	meta EventMeta,
	sender common.Address,
	tickLower, tickUpper int32,
	liquidityDelta *big.Int,
	salt common.Hash,
) PoolEvent {
	return PoolEvent{
		Meta: meta,
		Kind: EventKindModifyLiquidity,
		ModifyLiquidity: &ModifyLiquidityEvent{
			Sender:         sender,
			TickLower:      tickLower,
			TickUpper:      tickUpper,
			LiquidityDelta: cloneInt(liquidityDelta),
			Salt:           salt,
		},
	}
}

func cloneInt(v *big.Int) *big.Int {
	if v == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(v)
}
