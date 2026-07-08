package quickswapv3

import (
	"context"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

type (
	EventKind       = clv3.EventKind
	EventMeta       = clv3.EventMeta
	InitializeEvent = clv3.InitializeEvent
	SwapEvent       = clv3.SwapEvent
	MintEvent       = clv3.MintEvent
	BurnEvent       = clv3.BurnEvent
	PoolEvent       = clv3.PoolEvent
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

// Pool is the aggregate root for QuickSwap V3 market state.
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
	return market.PoolRefFromQuickSwapV3(p.Address)
}

// PoolRegistry defines which QuickSwap V3 pools the system should track and sync.
type PoolRegistry interface {
	List(ctx context.Context) ([]common.Address, error)
	Add(ctx context.Context, address common.Address) error
	Remove(ctx context.Context, address common.Address) error
}

// PoolRepository persists QuickSwap V3 pool aggregates keyed by contract address.
type PoolRepository interface {
	Save(ctx context.Context, pool *Pool) error
	Get(ctx context.Context, address common.Address) (*Pool, error)
	Delete(ctx context.Context, address common.Address) error
	AdvanceSyncProgress(ctx context.Context, address common.Address, blockNumber uint64) error
	AdvanceSyncProgressMany(ctx context.Context, addresses []common.Address, blockNumber uint64) error
}
