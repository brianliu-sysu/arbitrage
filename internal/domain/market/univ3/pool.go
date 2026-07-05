package univ3

import (
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

// Pool is the aggregate root for Uniswap V3 market state.
type Pool struct {
	Address     common.Address
	Token0      common.Address
	Token1      common.Address
	Fee         uint32
	TickSpacing int32

	State  market.PoolState
	Status market.PoolStatus
	Ticks  market.TickTable
	Bitmap market.TickBitmap

	LastBlockNumber uint64
}

func NewPool(address, token0, token1 common.Address, fee uint32, tickSpacing int32) *Pool {
	return &Pool{
		Address:     address,
		Token0:      token0,
		Token1:      token1,
		Fee:         fee,
		TickSpacing: tickSpacing,
		State:       market.NewPoolState(),
		Status:      market.PoolStatusUnknown,
		Ticks:       market.NewTickTable(),
		Bitmap:      market.NewTickBitmap(),
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

func (p *Pool) Ref() market.PoolRef {
	return market.PoolRefFromV3(p.Address)
}

// Apply is the sole entry point for mutating pool market state from chain events.
func (p *Pool) Apply(event PoolEvent) error {
	if event.Meta.PoolAddress != (common.Address{}) && event.Meta.PoolAddress != p.Address {
		return fmt.Errorf("event pool %s does not match pool %s", event.Meta.PoolAddress.Hex(), p.Address.Hex())
	}
	if event.Meta.BlockNumber < p.LastBlockNumber {
		return nil
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
	if err := market.ValidateTick(event.Initialize.Tick); err != nil {
		return err
	}

	p.State.SqrtPriceX96 = cloneInt(event.Initialize.SqrtPriceX96)
	p.State.Tick = event.Initialize.Tick
	if p.Status == market.PoolStatusUnknown || p.Status == market.PoolStatusBootstrapping {
		p.Status = market.PoolStatusSyncing
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
	if err := market.ValidateTick(event.Swap.Tick); err != nil {
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
		return nil
	}

	negated := new(big.Int).Neg(event.Burn.Amount)
	return p.modifyPosition(event.Burn.TickLower, event.Burn.TickUpper, negated)
}

func (p *Pool) modifyPosition(tickLower, tickUpper int32, liquidityDelta *big.Int) error {
	return market.ModifyLiquidity(
		p.TickSpacing,
		&p.State,
		&p.Ticks,
		&p.Bitmap,
		tickLower,
		tickUpper,
		liquidityDelta,
	)
}
