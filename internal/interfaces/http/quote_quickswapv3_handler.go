package httpapi

import (
	"net/http"
	"strings"

	quotequickswapv3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/quickswapv3"
	quotequickswapv3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/quickswapv3"
	"github.com/gin-gonic/gin"
)

// QuoteQuickSwapV3Handler exposes QuickSwap V3 quote requests over HTTP.
type QuoteQuickSwapV3Handler struct {
	quotes        *quotequickswapv3.AppService
	quotesByChain map[string]*quotequickswapv3.AppService
	chains        chainSelector
}

func NewQuoteQuickSwapV3Handler(quotes *quotequickswapv3.AppService) *QuoteQuickSwapV3Handler {
	return &QuoteQuickSwapV3Handler{quotes: quotes}
}

func NewQuoteQuickSwapV3ChainHandler(chains []ChainInfo, quotes map[string]*quotequickswapv3.AppService) *QuoteQuickSwapV3Handler {
	return &QuoteQuickSwapV3Handler{quotesByChain: quotes, chains: newChainSelector(chains)}
}

// HandleQuote serves POST /api/v1/quickswapv3/quote requests.
func (h *QuoteQuickSwapV3Handler) HandleQuote(c *gin.Context) {
	var payload quoteHTTPRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: "invalid json body"})
		return
	}

	quotes, ok := h.selectQuotes(quoteChainParam(c, payload))
	if !ok {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: chainNotFoundMessage(quoteChainParam(c, payload))})
		return
	}
	if quotes == nil {
		c.JSON(http.StatusServiceUnavailable, errorHTTPResponse{Error: "quickswapv3 quote service is not configured"})
		return
	}

	req, err := toQuoteQuickSwapV3Request(payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
		return
	}

	resp, err := quotes.Quote(c.Request.Context(), req)
	if err != nil {
		c.JSON(quoteStatusCode(err), errorHTTPResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, toQuoteQuickSwapV3HTTPResponse(resp))
}

func (h *QuoteQuickSwapV3Handler) selectQuotes(chain string) (*quotequickswapv3.AppService, bool) {
	if h.quotesByChain == nil {
		return h.quotes, true
	}
	key, ok := h.chains.selectKey(chain)
	if !ok {
		return nil, false
	}
	return h.quotesByChain[key], true
}

func toQuoteQuickSwapV3Request(payload quoteHTTPRequest) (quotequickswapv3.Request, error) {
	tokenIn, tokenOut, mode, amountIn, amountOut, err := parseQuoteBase(payload)
	if err != nil {
		return quotequickswapv3.Request{}, err
	}
	if strings.TrimSpace(payload.PoolID) != "" {
		return quotequickswapv3.Request{}, errPoolIDNotSupportedOnV3
	}

	req := quotequickswapv3.Request{
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		Mode:      mode,
		AmountIn:  amountIn,
		AmountOut: amountOut,
	}
	if strings.TrimSpace(payload.PoolAddress) != "" {
		poolAddress, err := parseAddress(payload.PoolAddress, "poolAddress")
		if err != nil {
			return quotequickswapv3.Request{}, err
		}
		req.PoolAddress = &poolAddress
	}
	return req, nil
}

func toQuoteQuickSwapV3HTTPResponse(resp quotequickswapv3.Response) quoteHTTPResponse {
	routeQuotes := make([]routeQuoteHTTPResponse, 0, len(resp.RouteQuotes))
	for _, item := range resp.RouteQuotes {
		routeQuotes = append(routeQuotes, routeQuoteHTTPResponse{
			Route:     toRouteQuickSwapV3HTTPResponse(item.Route),
			AmountIn:  item.AmountIn.String(),
			AmountOut: item.AmountOut.String(),
			FeeAmount: item.FeeAmount.String(),
		})
	}

	return quoteHTTPResponseFromAmounts(
		resp.TokenIn,
		resp.TokenOut,
		resp.AmountIn,
		resp.AmountOut,
		resp.FeeAmount,
		toRouteQuickSwapV3HTTPResponse(resp.BestRoute),
		routeQuotes,
	)
}

func toRouteQuickSwapV3HTTPResponse(route quotequickswapv3domain.Route) routeHTTPResponse {
	hops := make([]routeHopHTTPResponse, 0, len(route.Hops))
	for _, hop := range route.Hops {
		hops = append(hops, routeHopHTTPResponse{
			PoolAddress: hop.PoolAddress.Hex(),
			TokenIn:     hop.TokenIn.Hex(),
			TokenOut:    hop.TokenOut.Hex(),
		})
	}
	return routeHTTPResponse{
		TokenIn:  route.TokenIn.Hex(),
		TokenOut: route.TokenOut.Hex(),
		Hops:     hops,
	}
}
