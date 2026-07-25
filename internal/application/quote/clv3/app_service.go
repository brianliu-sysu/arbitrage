package clv3

import (
	"context"

	quotecombined "github.com/brianliu-sysu/uniswapv3/internal/application/quote/combined"
	quoteclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/clv3"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

// AppService adapts the protocol-specific API to the unified snapshot quote core.
type AppService struct {
	combined *quotecombined.AppService
}

func NewAppService(combined *quotecombined.AppService) *AppService {
	return &AppService{combined: combined}
}

func (s *AppService) Quote(ctx context.Context, req Request) (Response, error) {
	response, err := s.combined.Quote(ctx, quotecombined.Request{
		TokenIn: req.TokenIn, TokenOut: req.TokenOut, Mode: req.Mode,
		AmountIn: req.AmountIn, AmountOut: req.AmountOut, PoolAddress: req.PoolAddress,
	})
	if err != nil {
		return Response{}, err
	}
	return clv3Response(response), nil
}

func clv3Response(response quotecombined.Response) Response {
	routeQuotes := make([]RouteQuote, 0, len(response.RouteQuotes))
	for _, candidate := range response.RouteQuotes {
		routeQuotes = append(routeQuotes, RouteQuote{
			Route:    unifiedCLV3Route(candidate.Route),
			AmountIn: candidate.AmountIn, AmountOut: candidate.AmountOut, FeeAmount: candidate.FeeAmount,
		})
	}
	return Response{
		TokenIn: response.TokenIn, TokenOut: response.TokenOut,
		AmountIn: response.AmountIn, AmountOut: response.AmountOut, FeeAmount: response.FeeAmount,
		BestRoute: unifiedCLV3Route(response.BestRoute), RouteQuotes: routeQuotes,
	}
}

func unifiedCLV3Route(route quoteunified.Route) quoteclv3.Route {
	hops := make([]quoteclv3.RouteHop, 0, len(route.Hops))
	for _, hop := range route.Hops {
		hops = append(hops, quoteclv3.RouteHop{
			PoolAddress: clv3PoolAddress(hop),
			TokenIn:     hop.TokenIn, TokenOut: hop.TokenOut,
		})
	}
	return quoteclv3.Route{TokenIn: route.TokenIn, TokenOut: route.TokenOut, Hops: hops}
}

func clv3PoolAddress(hop quoteunified.RouteHop) common.Address {
	switch hop.Version {
	case quoteunified.PoolVersionV3:
		return hop.PoolV3
	case quoteunified.PoolVersionPancakeV3:
		return hop.PoolPancakeV3
	default:
		return hop.PoolQuickSwapV3
	}
}
