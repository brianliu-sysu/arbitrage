package poolsapp

import (
	"context"
	"fmt"

	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

type Univ4Adapter struct {
	pools    marketuniv4.PoolRepository
	registry marketuniv4.PoolRegistry
	reader   V4BaseStateReader
}

func NewUniv4Adapter(
	pools marketuniv4.PoolRepository,
	registry marketuniv4.PoolRegistry,
	reader V4BaseStateReader,
) *Univ4Adapter {
	return &Univ4Adapter{pools: pools, registry: registry, reader: reader}
}

func (a *Univ4Adapter) Type() string { return PoolTypeUniv4 }

func (a *Univ4Adapter) List(ctx context.Context) ([]PoolInfo, error) {
	if a.registry == nil || a.pools == nil {
		return nil, nil
	}
	poolIDs, err := a.registry.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list univ4 pools: %w", err)
	}
	items := make([]PoolInfo, 0, len(poolIDs))
	for _, poolID := range poolIDs {
		pool, err := a.pools.Get(ctx, poolID)
		if err != nil {
			return nil, fmt.Errorf("load univ4 pool %s: %w", poolID.String(), err)
		}
		if pool == nil {
			continue
		}
		hooks := ""
		if pool.Key.Hooks != (common.Address{}) {
			hooks = pool.Key.Hooks.Hex()
		}
		items = append(items, PoolInfo{
			PoolID: poolID.String(), PoolType: PoolTypeUniv4,
			Token0: tokenInfoFromAddress(pool.Key.Currency0),
			Token1: tokenInfoFromAddress(pool.Key.Currency1),
			Fee:    pool.Key.Fee, Hooks: hooks,
		})
	}
	return items, nil
}

func (a *Univ4Adapter) Diagnostics(
	ctx context.Context,
	req DiagnosticsRequest,
	head uint64,
	resolve TokenMetadataResolver,
) (*DiagnosticsResponse, error) {
	return a.diagnostics(ctx, req.PoolID, head, resolve)
}

func (a *Univ4Adapter) AppendMismatches(
	ctx context.Context,
	head uint64,
	resolve TokenMetadataResolver,
	items *[]DiagnosticsResponse,
) error {
	return a.appendMismatches(ctx, head, resolve, items)
}
