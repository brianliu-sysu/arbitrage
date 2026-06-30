package store

import (
	"context"

	"github.com/brianliu-sysu/arbitrage/internal/storage"
)

// storerAdapter 将 storage.PoolRepo 适配为 legacy Storer 接口。
type storerAdapter struct {
	repo storage.PoolRepo
}

func newStorerAdapter(repo storage.PoolRepo) Storer {
	if repo == nil {
		return NewNoopStore()
	}
	return &storerAdapter{repo: repo}
}

func (a *storerAdapter) Save(ctx context.Context, s *PoolSnapshot) error {
	return a.repo.Save(ctx, toStorageSnapshot(s))
}

func (a *storerAdapter) SaveHistory(ctx context.Context, s *PoolSnapshot) error {
	return a.repo.SaveHistory(ctx, toStorageSnapshot(s))
}

func (a *storerAdapter) Load(ctx context.Context, chainName, poolAddress string) (*PoolSnapshot, error) {
	snap, err := a.repo.Load(ctx, chainName, poolAddress)
	if err != nil || snap == nil {
		return fromStorageSnapshot(snap), err
	}
	return fromStorageSnapshot(snap), nil
}

func (a *storerAdapter) LoadAll(ctx context.Context, chainName string) (map[string]*PoolSnapshot, error) {
	all, err := a.repo.LoadAll(ctx, chainName)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*PoolSnapshot, len(all))
	for k, v := range all {
		out[k] = fromStorageSnapshot(v)
	}
	return out, nil
}

func (a *storerAdapter) LoadTokenMetadata(ctx context.Context, chainName, tokenAddress string) (*TokenMetadata, error) {
	meta, err := a.repo.LoadTokenMetadata(ctx, chainName, tokenAddress)
	if err != nil || meta == nil {
		return nil, err
	}
	return &TokenMetadata{
		ChainName:    meta.ChainName,
		TokenAddress: meta.TokenAddress,
		Symbol:       meta.Symbol,
		Decimals:     meta.Decimals,
	}, nil
}

func (a *storerAdapter) SaveTokenMetadata(ctx context.Context, meta *TokenMetadata) error {
	return a.repo.SaveTokenMetadata(ctx, &storage.TokenMetadata{
		ChainName:    meta.ChainName,
		TokenAddress: meta.TokenAddress,
		Symbol:       meta.Symbol,
		Decimals:     meta.Decimals,
	})
}

func (a *storerAdapter) Close() {
	a.repo.Close()
}

func toStorageSnapshot(s *PoolSnapshot) *storage.PoolSnapshot {
	if s == nil {
		return nil
	}
	tickData := make(map[int32]storage.TickLiquiditySnapshot, len(s.TickData))
	for tick, t := range s.TickData {
		tickData[tick] = storage.TickLiquiditySnapshot{
			LiquidityNet:   t.LiquidityNet,
			LiquidityGross: t.LiquidityGross,
		}
	}
	return &storage.PoolSnapshot{
		ChainName:    s.ChainName,
		PoolAddress:  s.PoolAddress,
		BlockNumber:  s.BlockNumber,
		Tick:         s.Tick,
		SqrtPriceX96: s.SqrtPriceX96,
		Liquidity:    s.Liquidity,
		Price0In1:    s.Price0In1,
		Token0Symbol: s.Token0Symbol,
		Token1Symbol: s.Token1Symbol,
		Fee:          s.Fee,
		TickData:     tickData,
	}
}

func fromStorageSnapshot(s *storage.PoolSnapshot) *PoolSnapshot {
	if s == nil {
		return nil
	}
	tickData := make(map[int32]TickLiquiditySnapshot, len(s.TickData))
	for tick, t := range s.TickData {
		tickData[tick] = TickLiquiditySnapshot{
			LiquidityNet:   t.LiquidityNet,
			LiquidityGross: t.LiquidityGross,
		}
	}
	return &PoolSnapshot{
		ChainName:    s.ChainName,
		PoolAddress:  s.PoolAddress,
		BlockNumber:  s.BlockNumber,
		Tick:         s.Tick,
		SqrtPriceX96: s.SqrtPriceX96,
		Liquidity:    s.Liquidity,
		Price0In1:    s.Price0In1,
		Token0Symbol: s.Token0Symbol,
		Token1Symbol: s.Token1Symbol,
		Fee:          s.Fee,
		TickData:     tickData,
	}
}
