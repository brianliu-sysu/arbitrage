package quoteuniv4

import (
	"math/big"

	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	quoteuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ4"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// Request is the V4 quote use case input.
type Request struct {
	TokenIn  common.Address
	TokenOut common.Address
	Mode     quoteshared.QuoteMode
	AmountIn    *big.Int
	AmountOut   *big.Int
	PoolID      *marketv4.PoolID
}

func (r Request) IsExactInput() bool {
	return r.Mode == quoteshared.QuoteModeExactInput
}

func (r Request) IsExactOutput() bool {
	return r.Mode == quoteshared.QuoteModeExactOutput
}

// RouteQuote captures the quote outcome for a single route candidate.
type RouteQuote struct {
	Route     quoteuniv4.Route
	AmountIn  *big.Int
	AmountOut *big.Int
	FeeAmount *big.Int
}

// Response is the V4 quote use case output.
type Response struct {
	TokenIn     common.Address
	TokenOut    common.Address
	AmountIn    *big.Int
	AmountOut   *big.Int
	FeeAmount   *big.Int
	BestRoute   quoteuniv4.Route
	RouteQuotes []RouteQuote
}
