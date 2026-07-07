package balancer

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// EventKind identifies Balancer Vault/pool logs that mutate local pool state.
type EventKind int

const (
	EventKindPoolBalanceChanged EventKind = iota + 1
	EventKindSwap
	EventKindSwapFeePercentageChanged
	EventKindAmplificationUpdated
	EventKindLiquidityAdded
	EventKindLiquidityRemoved
	EventKindPoolPausedStateChanged
)

func (k EventKind) String() string {
	switch k {
	case EventKindPoolBalanceChanged:
		return "pool_balance_changed"
	case EventKindSwap:
		return "swap"
	case EventKindSwapFeePercentageChanged:
		return "swap_fee_percentage_changed"
	case EventKindAmplificationUpdated:
		return "amplification_updated"
	case EventKindLiquidityAdded:
		return "liquidity_added"
	case EventKindLiquidityRemoved:
		return "liquidity_removed"
	case EventKindPoolPausedStateChanged:
		return "pool_paused_state_changed"
	default:
		return "unknown"
	}
}

// EventMeta holds common metadata for every Balancer pool log.
type EventMeta struct {
	PoolID      PoolID
	BlockNumber uint64
	TxIndex     uint
	LogIndex    uint
}

// PoolBalanceChangedEvent applies signed token balance deltas from the Vault.
type PoolBalanceChangedEvent struct {
	Tokens []common.Address
	Deltas []*big.Int
}

// SwapEvent applies Vault swap balance movement.
type SwapEvent struct {
	TokenIn   common.Address
	TokenOut  common.Address
	AmountIn  *big.Int
	AmountOut *big.Int
}

// SwapFeePercentageChangedEvent updates the pool swap fee scaled by 1e18.
type SwapFeePercentageChangedEvent struct {
	SwapFeePercentage *big.Int
}

// AmplificationUpdatedEvent updates a stable pool amplification parameter.
type AmplificationUpdatedEvent struct {
	Amplification *big.Int
}

// LiquidityAddedEvent applies positive token balance deltas in pool registration order.
type LiquidityAddedEvent struct {
	Amounts []*big.Int
}

// LiquidityRemovedEvent applies negative token balance deltas in pool registration order.
type LiquidityRemovedEvent struct {
	Amounts []*big.Int
}

// PoolPausedStateChangedEvent updates on-chain pool pause status.
type PoolPausedStateChangedEvent struct {
	Paused bool
}

// PoolEvent is an immutable on-chain fact for a Balancer pool.
type PoolEvent struct {
	Meta EventMeta
	Kind EventKind

	PoolBalanceChanged         *PoolBalanceChangedEvent
	Swap                       *SwapEvent
	SwapFeePercentageChanged   *SwapFeePercentageChangedEvent
	AmplificationUpdated       *AmplificationUpdatedEvent
	LiquidityAdded             *LiquidityAddedEvent
	LiquidityRemoved           *LiquidityRemovedEvent
	PoolPausedStateChanged     *PoolPausedStateChangedEvent
}

func NewPoolBalanceChangedEvent(meta EventMeta, tokens []common.Address, deltas []*big.Int) PoolEvent {
	return PoolEvent{
		Meta: meta,
		Kind: EventKindPoolBalanceChanged,
		PoolBalanceChanged: &PoolBalanceChangedEvent{
			Tokens: cloneAddresses(tokens),
			Deltas: cloneInts(deltas),
		},
	}
}

func NewSwapEvent(meta EventMeta, tokenIn, tokenOut common.Address, amountIn, amountOut *big.Int) PoolEvent {
	return PoolEvent{
		Meta: meta,
		Kind: EventKindSwap,
		Swap: &SwapEvent{
			TokenIn:   tokenIn,
			TokenOut:  tokenOut,
			AmountIn:  cloneInt(amountIn),
			AmountOut: cloneInt(amountOut),
		},
	}
}

func NewSwapFeePercentageChangedEvent(meta EventMeta, swapFeePercentage *big.Int) PoolEvent {
	return PoolEvent{
		Meta: meta,
		Kind: EventKindSwapFeePercentageChanged,
		SwapFeePercentageChanged: &SwapFeePercentageChangedEvent{
			SwapFeePercentage: cloneInt(swapFeePercentage),
		},
	}
}

func NewAmplificationUpdatedEvent(meta EventMeta, amplification *big.Int) PoolEvent {
	return PoolEvent{
		Meta: meta,
		Kind: EventKindAmplificationUpdated,
		AmplificationUpdated: &AmplificationUpdatedEvent{
			Amplification: cloneInt(amplification),
		},
	}
}

func NewLiquidityAddedEvent(meta EventMeta, amounts []*big.Int) PoolEvent {
	return PoolEvent{
		Meta: meta,
		Kind: EventKindLiquidityAdded,
		LiquidityAdded: &LiquidityAddedEvent{
			Amounts: cloneInts(amounts),
		},
	}
}

func NewLiquidityRemovedEvent(meta EventMeta, amounts []*big.Int) PoolEvent {
	return PoolEvent{
		Meta: meta,
		Kind: EventKindLiquidityRemoved,
		LiquidityRemoved: &LiquidityRemovedEvent{
			Amounts: cloneInts(amounts),
		},
	}
}

func NewPoolPausedStateChangedEvent(meta EventMeta, paused bool) PoolEvent {
	return PoolEvent{
		Meta: meta,
		Kind: EventKindPoolPausedStateChanged,
		PoolPausedStateChanged: &PoolPausedStateChangedEvent{
			Paused: paused,
		},
	}
}
