package quoteapp

import (
	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	quotecombined "github.com/brianliu-sysu/uniswapv3/internal/application/quote/combined"
	quoteuniv3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/univ3"
)

type (
	// QuoteMode selects exact-input or exact-output quoting.
	QuoteMode = quoteshared.QuoteMode

	// QuoteRequest is the unified quote use case input.
	QuoteRequest = quotecombined.Request

	// QuoteResponse is the unified quote use case output.
	QuoteResponse = quotecombined.Response

	// RouteQuote captures the quote outcome for a single route candidate.
	RouteQuote = quotecombined.RouteQuote

	// QuoteV3AppService orchestrates V3-only route discovery and quoting.
	QuoteV3AppService = quoteuniv3.AppService
)

const (
	QuoteModeExactInput  = quoteshared.QuoteModeExactInput
	QuoteModeExactOutput = quoteshared.QuoteModeExactOutput
)

// QuoteAppService orchestrates unified V3/V4 route discovery and quoting.
type QuoteAppService = quotecombined.AppService

// NewQuoteAppService creates a unified quote application service.
var NewQuoteAppService = quotecombined.NewAppService

// NewQuoteV3AppService creates a V3-only quote application service.
var NewQuoteV3AppService = quoteuniv3.NewAppService
