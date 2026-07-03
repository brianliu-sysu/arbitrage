package market

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Pool is the aggregate root for Uniswap V3 market state.
type Pool struct {
	Address     common.Address
	Token0      common.Address
	Token1      common.Address
	Fee         uint32
	TickSpacing int32

	State  PoolState
	Status PoolStatus
	Ticks  TickTable
	Bitmap TickBitmap

	LastBlockNumber uint64
}

func NewPool(address, token0, token1 common.Address, fee uint32, tickSpacing int32) *Pool {
	return &Pool{
		Address:     address,
		Token0:      token0,
		Token1:      token1,
		Fee:         fee,
		TickSpacing: tickSpacing,
		State:       NewPoolState(),
		Status:      PoolStatusUnknown,
		Ticks:       NewTickTable(),
		Bitmap:      NewTickBitmap(),
	}
}

func (p *Pool) Clone() *Pool {
	if p == nil {
		return nil
	}
	return &Pool{
		Address:         p.Address,
		Token0:          p.Token0,
		Token1:          p.Token1,
		Fee:             p.Fee,
		TickSpacing:     p.TickSpacing,
		State:           p.State.Clone(),
		Status:          p.Status,
		Ticks:           p.Ticks.Clone(),
		Bitmap:          p.Bitmap.Clone(),
		LastBlockNumber: p.LastBlockNumber,
	}
}

// Apply is the sole entry point for mutating pool market state from chain events.
func (p *Pool) Apply(event PoolEvent) error {
	if event.Meta.PoolAddress != (common.Address{}) && event.Meta.PoolAddress != p.Address {
		return fmt.Errorf("event pool %s does not match pool %s", event.Meta.PoolAddress.Hex(), p.Address.Hex())
	}
	if event.Meta.BlockNumber < p.LastBlockNumber {
		return fmt.Errorf("event block %d is before pool last block %d", event.Meta.BlockNumber, p.LastBlockNumber)
	}

	var err error
	switch event.Kind {
	case EventKindInitialize:
		err = p.applyInitialize(event)
	case EventKindSwap:
		err = p.applySwap(event)
	case EventKindMint:
		err = p.applyMint(event)
	case EventKindBurn:
		err = p.applyBurn(event)
	default:
		return fmt.Errorf("unsupported event kind %d", event.Kind)
	}
	if err != nil {
		return err
	}

	if event.Meta.BlockNumber > p.LastBlockNumber {
		p.LastBlockNumber = event.Meta.BlockNumber
	}
	return nil
}

func (p *Pool) applyInitialize(event PoolEvent) error {
	if event.Initialize == nil {
		return fmt.Errorf("initialize event payload is nil")
	}
	if p.State.IsInitialized() {
		return fmt.Errorf("pool already initialized")
	}
	if event.Initialize.SqrtPriceX96 == nil || event.Initialize.SqrtPriceX96.Sign() <= 0 {
		return fmt.Errorf("initialize sqrt price must be positive")
	}
	if err := validateTick(event.Initialize.Tick); err != nil {
		return err
	}

	p.State.SqrtPriceX96 = cloneInt(event.Initialize.SqrtPriceX96)
	p.State.Tick = event.Initialize.Tick
	if p.Status == PoolStatusUnknown || p.Status == PoolStatusBootstrapping {
		p.Status = PoolStatusSyncing
	}
	return nil
}

func (p *Pool) applySwap(event PoolEvent) error {
	if event.Swap == nil {
		return fmt.Errorf("swap event payload is nil")
	}
	if !p.State.IsInitialized() {
		return fmt.Errorf("pool is not initialized")
	}
	if event.Swap.SqrtPriceX96 == nil || event.Swap.Liquidity == nil {
		return fmt.Errorf("swap event missing price or liquidity")
	}
	if err := validateTick(event.Swap.Tick); err != nil {
		return err
	}

	p.State.SqrtPriceX96 = cloneInt(event.Swap.SqrtPriceX96)
	p.State.Tick = event.Swap.Tick
	p.State.Liquidity = cloneInt(event.Swap.Liquidity)
	return nil
}

func (p *Pool) applyMint(event PoolEvent) error {
	if event.Mint == nil {
		return fmt.Errorf("mint event payload is nil")
	}
	if !p.State.IsInitialized() {
		return fmt.Errorf("pool is not initialized")
	}
	return p.modifyPosition(event.Mint.TickLower, event.Mint.TickUpper, event.Mint.Amount)
}

func (p *Pool) applyBurn(event PoolEvent) error {
	if event.Burn == nil {
		return fmt.Errorf("burn event payload is nil")
	}
	if !p.State.IsInitialized() {
		return fmt.Errorf("pool is not initialized")
	}
	if event.Burn.Amount == nil || event.Burn.Amount.Sign() <= 0 {
		return fmt.Errorf("burn amount must be positive")
	}

	negated := new(big.Int).Neg(event.Burn.Amount)
	return p.modifyPosition(event.Burn.TickLower, event.Burn.TickUpper, negated)
}

func (p *Pool) modifyPosition(tickLower, tickUpper int32, liquidityDelta *big.Int) error {
	if liquidityDelta == nil || liquidityDelta.Sign() == 0 {
		return fmt.Errorf("liquidity delta must be non-zero")
	}
	if err := validateTickSpacing(tickLower, p.TickSpacing); err != nil {
		return err
	}
	if err := validateTickSpacing(tickUpper, p.TickSpacing); err != nil {
		return err
	}
	if tickLower >= tickUpper {
		return fmt.Errorf("tickLower %d must be less than tickUpper %d", tickLower, tickUpper)
	}

	flippedLower, err := p.Ticks.Update(tickLower, liquidityDelta, false)
	if err != nil {
		return fmt.Errorf("update lower tick: %w", err)
	}
	if flippedLower {
		if err := p.Bitmap.FlipTick(tickLower, p.TickSpacing); err != nil {
			return fmt.Errorf("flip lower tick bitmap: %w", err)
		}
	}

	flippedUpper, err := p.Ticks.Update(tickUpper, liquidityDelta, true)
	if err != nil {
		return fmt.Errorf("update upper tick: %w", err)
	}
	if flippedUpper {
		if err := p.Bitmap.FlipTick(tickUpper, p.TickSpacing); err != nil {
			return fmt.Errorf("flip upper tick bitmap: %w", err)
		}
	}

	if p.State.Tick >= tickLower && p.State.Tick < tickUpper {
		p.State.Liquidity = new(big.Int).Add(p.State.Liquidity, liquidityDelta)
		if p.State.Liquidity.Sign() < 0 {
			return fmt.Errorf("pool liquidity underflow")
		}
	}
	return nil
}

func validateTickSpacing(tick, tickSpacing int32) error {
	if err := validateTick(tick); err != nil {
		return err
	}
	if tick%tickSpacing != 0 {
		return fmt.Errorf("tick %d is not aligned to spacing %d", tick, tickSpacing)
	}
	return nil
}
