package clv3

import (
	"math/big"

	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	quoteclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/clv3"
	"github.com/ethereum/go-ethereum/common"
)

// Request is the CLV3 quote use case input.
type Request struct {
	TokenIn     common.Address
	TokenOut    common.Address
	Mode        quoteshared.QuoteMode
	AmountIn    *big.Int
	AmountOut   *big.Int
	PoolAddress *common.Address
}

func (r Request) IsExactInput() bool {
	return r.Mode == quoteshared.QuoteModeExactInput
}

func (r Request) IsExactOutput() bool {
	return r.Mode == quoteshared.QuoteModeExactOutput
}

// RouteQuote captures the quote outcome for a single route candidate.
type RouteQuote struct {
	Route     quoteclv3.Route
	AmountIn  *big.Int
	AmountOut *big.Int
	FeeAmount *big.Int
}

// Response is the CLV3 quote use case output.
type Response struct {
	TokenIn     common.Address
	TokenOut    common.Address
	AmountIn    *big.Int
	AmountOut   *big.Int
	FeeAmount   *big.Int
	BestRoute   quoteclv3.Route
	RouteQuotes []RouteQuote
}
