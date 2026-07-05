package univ3

import (
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

type (
	EventKind        = clv3.EventKind
	EventMeta        = clv3.EventMeta
	InitializeEvent  = clv3.InitializeEvent
	SwapEvent        = clv3.SwapEvent
	MintEvent        = clv3.MintEvent
	BurnEvent        = clv3.BurnEvent
	PoolEvent        = clv3.PoolEvent
)

const (
	EventKindInitialize = clv3.EventKindInitialize
	EventKindSwap       = clv3.EventKindSwap
	EventKindMint       = clv3.EventKindMint
	EventKindBurn       = clv3.EventKindBurn
)

var (
	NewInitializeEvent = clv3.NewInitializeEvent
	NewSwapEvent       = clv3.NewSwapEvent
	NewMintEvent       = clv3.NewMintEvent
	NewBurnEvent       = clv3.NewBurnEvent
)

// Pool is the aggregate root for Uniswap V3 market state.
type Pool struct {
	clv3.Pool
}

func NewPool(address, token0, token1 common.Address, fee uint32, tickSpacing int32) *Pool {
	return &Pool{Pool: *clv3.NewPool(address, token0, token1, fee, tickSpacing)}
}

func (p *Pool) Clone() *Pool {
	if p == nil {
		return nil
	}
	cloned := p.Pool.Clone()
	return &Pool{Pool: *cloned}
}

func (p *Pool) Ref() market.PoolRef {
	return market.PoolRefFromUniswapV3(p.Address)
}
