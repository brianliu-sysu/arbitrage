package quotequickswapv3

import (
	"github.com/brianliu-sysu/uniswapv3/internal/application/committedmarket"
	quoteappclv3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/clv3"
	quotecombined "github.com/brianliu-sysu/uniswapv3/internal/application/quote/combined"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
)

func NewAppService(market committedmarket.Reader, quotes *quoteunified.QuoteService, maxHops int) *AppService {
	core := quotecombined.NewAppService(market, quotes, maxHops, quoteunified.PoolVersionQuickSwapV3)
	return &AppService{AppService: quoteappclv3.NewAppService(core)}
}
