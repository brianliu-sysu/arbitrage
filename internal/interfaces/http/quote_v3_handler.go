package httpapi

import (
	"net/http"
	"strings"

	quoteuniv3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/univ3"
	quoteuniv3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ3"
	"github.com/gin-gonic/gin"
)

// QuoteV3Handler exposes Uniswap V3 quote requests over HTTP.
type QuoteV3Handler struct {
	quotes        *quoteuniv3.AppService
	quotesByChain map[string]*quoteuniv3.AppService
	chains        chainSelector
}

func NewQuoteV3Handler(quotes *quoteuniv3.AppService) *QuoteV3Handler {
	return &QuoteV3Handler{quotes: quotes}
}

func NewQuoteV3ChainHandler(chains []ChainInfo, quotes map[string]*quoteuniv3.AppService) *QuoteV3Handler {
	return &QuoteV3Handler{quotesByChain: quotes, chains: newChainSelector(chains)}
}

// HandleQuote serves POST /api/v1/univ3/quote requests.
func (h *QuoteV3Handler) HandleQuote(c *gin.Context) {
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
		c.JSON(http.StatusInternalServerError, errorHTTPResponse{Error: "v3 quote service is not configured"})
		return
	}

	req, err := toQuoteV3Request(payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
		return
	}

	resp, err := quotes.Quote(c.Request.Context(), req)
	if err != nil {
		c.JSON(quoteStatusCode(err), errorHTTPResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, toQuoteV3HTTPResponse(resp))
}

func (h *QuoteV3Handler) selectQuotes(chain string) (*quoteuniv3.AppService, bool) {
	if h.quotesByChain == nil {
		return h.quotes, true
	}
	key, ok := h.chains.selectKey(chain)
	if !ok {
		return nil, false
	}
	return h.quotesByChain[key], true
}

func toQuoteV3Request(payload quoteHTTPRequest) (quoteuniv3.Request, error) {
	tokenIn, tokenOut, mode, amountIn, amountOut, err := parseQuoteBase(payload)
	if err != nil {
		return quoteuniv3.Request{}, err
	}
	if strings.TrimSpace(payload.PoolID) != "" {
		return quoteuniv3.Request{}, errPoolIDNotSupportedOnV3
	}

	req := quoteuniv3.Request{
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		Mode:      mode,
		AmountIn:  amountIn,
		AmountOut: amountOut,
	}
	if strings.TrimSpace(payload.PoolAddress) != "" {
		poolAddress, err := parseAddress(payload.PoolAddress, "poolAddress")
		if err != nil {
			return quoteuniv3.Request{}, err
		}
		req.PoolAddress = &poolAddress
	}
	return req, nil
}

func toQuoteV3HTTPResponse(resp quoteuniv3.Response) quoteHTTPResponse {
	routeQuotes := make([]routeQuoteHTTPResponse, 0, len(resp.RouteQuotes))
	for _, item := range resp.RouteQuotes {
		routeQuotes = append(routeQuotes, routeQuoteHTTPResponse{
			Route:     toRouteV3HTTPResponse(item.Route),
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
		toRouteV3HTTPResponse(resp.BestRoute),
		routeQuotes,
	)
}

func toRouteV3HTTPResponse(route quoteuniv3domain.Route) routeHTTPResponse {
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
