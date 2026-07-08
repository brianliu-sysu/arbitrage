package quotequickswapv3

import (
	quoteappclv3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/clv3"
	quoteclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/clv3"
)

type (
	Request          = quoteappclv3.Request
	Response         = quoteappclv3.Response
	RouteQuote       = quoteappclv3.RouteQuote
	ReadinessChecker = quoteappclv3.ReadinessChecker
	Route            = quoteclv3.Route
)

// AppService orchestrates QuickSwap V3 route discovery and quoting.
type AppService struct {
	*quoteappclv3.AppService
}
