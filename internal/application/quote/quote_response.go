package quoteapp

import (
	"math/big"

	domainquote "github.com/brianliu-sysu/uniswapv3/internal/domain/quote"
	"github.com/ethereum/go-ethereum/common"
)

// RouteQuote captures the quote outcome for a single route candidate.
type RouteQuote struct {
	Route     domainquote.Route
	AmountIn  *big.Int
	AmountOut *big.Int
	FeeAmount *big.Int
}

// QuoteResponse is the application-layer quote use case output.
type QuoteResponse struct {
	TokenIn     common.Address
	TokenOut    common.Address
	AmountIn    *big.Int
	AmountOut   *big.Int
	FeeAmount   *big.Int
	BestRoute   domainquote.Route
	RouteQuotes []RouteQuote
}
