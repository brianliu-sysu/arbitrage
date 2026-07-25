package quoteapp

import quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"

// QuoteMode selects exact-input or exact-output quoting.
type QuoteMode = quoteshared.QuoteMode

const (
	QuoteModeExactInput  = quoteshared.QuoteModeExactInput
	QuoteModeExactOutput = quoteshared.QuoteModeExactOutput
)
