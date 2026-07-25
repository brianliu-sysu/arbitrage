package quoteuniv4

import (
	"context"

	"github.com/brianliu-sysu/uniswapv3/internal/application/committedmarket"
	quotecombined "github.com/brianliu-sysu/uniswapv3/internal/application/quote/combined"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	quoteuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ4"
)

// AppService adapts the V4 API to the unified snapshot quote core.
type AppService struct {
	combined *quotecombined.AppService
}

func NewAppService(market committedmarket.Reader, quotes *quoteunified.QuoteService, maxHops int) *AppService {
	return &AppService{combined: quotecombined.NewAppService(market, quotes, maxHops, quoteunified.PoolVersionV4)}
}

func (s *AppService) Quote(ctx context.Context, req Request) (Response, error) {
	response, err := s.combined.Quote(ctx, quotecombined.Request{
		TokenIn: req.TokenIn, TokenOut: req.TokenOut, Mode: req.Mode,
		AmountIn: req.AmountIn, AmountOut: req.AmountOut, PoolID: req.PoolID,
	})
	if err != nil {
		return Response{}, err
	}
	routeQuotes := make([]RouteQuote, 0, len(response.RouteQuotes))
	for _, candidate := range response.RouteQuotes {
		routeQuotes = append(routeQuotes, RouteQuote{
			Route:    unifiedV4Route(candidate.Route),
			AmountIn: candidate.AmountIn, AmountOut: candidate.AmountOut, FeeAmount: candidate.FeeAmount,
		})
	}
	return Response{
		TokenIn: response.TokenIn, TokenOut: response.TokenOut,
		AmountIn: response.AmountIn, AmountOut: response.AmountOut, FeeAmount: response.FeeAmount,
		BestRoute: unifiedV4Route(response.BestRoute), RouteQuotes: routeQuotes,
	}, nil
}

func unifiedV4Route(route quoteunified.Route) quoteuniv4.Route {
	hops := make([]quoteuniv4.RouteHop, 0, len(route.Hops))
	for _, hop := range route.Hops {
		hops = append(hops, quoteuniv4.RouteHop{PoolID: hop.PoolV4, TokenIn: hop.TokenIn, TokenOut: hop.TokenOut})
	}
	return quoteuniv4.Route{TokenIn: route.TokenIn, TokenOut: route.TokenOut, Hops: hops}
}
