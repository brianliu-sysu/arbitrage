package univ3

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// EventKind identifies the type of on-chain V3 pool log.
type EventKind int

const (
	EventKindInitialize EventKind = iota + 1
	EventKindSwap
	EventKindMint
	EventKindBurn
)

// EventMeta holds common metadata for every V3 pool log.
type EventMeta struct {
	PoolAddress common.Address
	BlockNumber uint64
	TxIndex     uint
	LogIndex    uint
}

// InitializeEvent is emitted when a pool is created on-chain.
type InitializeEvent struct {
	SqrtPriceX96 *big.Int
	Tick         int32
}

// SwapEvent is emitted when a swap changes pool price and liquidity.
type SwapEvent struct {
	Sender       common.Address
	Recipient    common.Address
	Amount0      *big.Int
	Amount1      *big.Int
	SqrtPriceX96 *big.Int
	Liquidity    *big.Int
	Tick         int32
}

// MintEvent is emitted when liquidity is added to a position.
type MintEvent struct {
	Sender    common.Address
	Owner     common.Address
	TickLower int32
	TickUpper int32
	Amount    *big.Int
	Amount0   *big.Int
	Amount1   *big.Int
}

// BurnEvent is emitted when liquidity is removed from a position.
type BurnEvent struct {
	Owner     common.Address
	TickLower int32
	TickUpper int32
	Amount    *big.Int
	Amount0   *big.Int
	Amount1   *big.Int
}

// PoolEvent is an immutable on-chain fact. It never mutates pool state by itself.
type PoolEvent struct {
	Meta EventMeta
	Kind EventKind

	Initialize *InitializeEvent
	Swap       *SwapEvent
	Mint       *MintEvent
	Burn       *BurnEvent
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

func NewSwapEvent(meta EventMeta, sender, recipient common.Address, amount0, amount1, sqrtPriceX96, liquidity *big.Int, tick int32) PoolEvent {
	return PoolEvent{
		Meta: meta,
		Kind: EventKindSwap,
		Swap: &SwapEvent{
			Sender:       sender,
			Recipient:    recipient,
			Amount0:      cloneInt(amount0),
			Amount1:      cloneInt(amount1),
			SqrtPriceX96: cloneInt(sqrtPriceX96),
			Liquidity:    cloneInt(liquidity),
			Tick:         tick,
		},
	}
}

func NewMintEvent(meta EventMeta, sender, owner common.Address, tickLower, tickUpper int32, amount, amount0, amount1 *big.Int) PoolEvent {
	return PoolEvent{
		Meta: meta,
		Kind: EventKindMint,
		Mint: &MintEvent{
			Sender:    sender,
			Owner:     owner,
			TickLower: tickLower,
			TickUpper: tickUpper,
			Amount:    cloneInt(amount),
			Amount0:   cloneInt(amount0),
			Amount1:   cloneInt(amount1),
		},
	}
}

func NewBurnEvent(meta EventMeta, owner common.Address, tickLower, tickUpper int32, amount, amount0, amount1 *big.Int) PoolEvent {
	return PoolEvent{
		Meta: meta,
		Kind: EventKindBurn,
		Burn: &BurnEvent{
			Owner:     owner,
			TickLower: tickLower,
			TickUpper: tickUpper,
			Amount:    cloneInt(amount),
			Amount0:   cloneInt(amount0),
			Amount1:   cloneInt(amount1),
		},
	}
}

func cloneInt(v *big.Int) *big.Int {
	if v == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(v)
}
