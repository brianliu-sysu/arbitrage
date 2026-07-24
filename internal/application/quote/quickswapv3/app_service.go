package quotequickswapv3

import (
	"context"

	quoteappclv3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/clv3"
	quotecontract "github.com/brianliu-sysu/uniswapv3/internal/application/quote/contract"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	quotequickswapv3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/quickswapv3"
	"github.com/ethereum/go-ethereum/common"
)

func NewAppService(
	pools marketquick.PoolRepository,
	registry quotecontract.PoolRegistry[common.Address],
	quotes *quotequickswapv3domain.QuoteService,
	readiness quotecontract.PoolReadiness[common.Address],
	maxHops int,
) *AppService {
	engine := quotequickswapv3domain.NewQuoteService()
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
	inner marketquick.PoolRepository
}

func (r clv3PoolRepo) Get(ctx context.Context, address common.Address) (*marketclv3.Pool, error) {
	pool, err := r.inner.Get(ctx, address)
	if pool == nil {
		return nil, err
	}
	return pool.Pool.Clone(), nil
}
