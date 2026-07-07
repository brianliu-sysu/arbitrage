package quotepancakev3

import (
	"context"

	quoteappclv3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/clv3"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	quotepancakev3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/pancakev3"
	"github.com/ethereum/go-ethereum/common"
)

func NewAppService(
	pools marketpancake.PoolRepository,
	registry marketpancake.PoolRegistry,
	quotes *quotepancakev3domain.QuoteService,
	readiness ReadinessChecker,
	maxHops int,
) *AppService {
	engine := quotepancakev3domain.NewQuoteService()
	if quotes != nil {
		engine = quotes
	}
	return &AppService{AppService: quoteappclv3.NewAppService(
		clv3PoolRepo{pools},
		registry,
		engine.Engine(),
		readiness,
		maxHops,
	)}
}

type clv3PoolRepo struct {
	inner marketpancake.PoolRepository
}

func (r clv3PoolRepo) Get(ctx context.Context, address common.Address) (*marketclv3.Pool, error) {
	pool, err := r.inner.Get(ctx, address)
	if pool == nil {
		return nil, err
	}
	return pool.Pool.Clone(), nil
}
