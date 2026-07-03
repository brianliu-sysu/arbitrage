package quoteapp

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// QuoteMode selects exact-input or exact-output quoting.
type QuoteMode int

const (
	QuoteModeExactInput QuoteMode = iota + 1
	QuoteModeExactOutput
)

// QuoteRequest is the application-layer quote use case input.
type QuoteRequest struct {
	TokenIn     common.Address
	TokenOut    common.Address
	Mode        QuoteMode
	AmountIn    *big.Int
	AmountOut   *big.Int
	PoolAddress *common.Address
}

func (r QuoteRequest) IsExactInput() bool {
	return r.Mode == QuoteModeExactInput
}

func (r QuoteRequest) IsExactOutput() bool {
	return r.Mode == QuoteModeExactOutput
}
