package httpapi

import (
	"net/http"
	"strings"

	quotepancakev3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/pancakev3"
	quotepancakev3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/pancakev3"
	"github.com/gin-gonic/gin"
)

// QuotePancakeV3Handler exposes PancakeSwap V3 quote requests over HTTP.
type QuotePancakeV3Handler struct {
	quotes        *quotepancakev3.AppService
	quotesByChain map[string]*quotepancakev3.AppService
	chains        chainSelector
}

func NewQuotePancakeV3Handler(quotes *quotepancakev3.AppService) *QuotePancakeV3Handler {
	return &QuotePancakeV3Handler{quotes: quotes}
}

func NewQuotePancakeV3ChainHandler(chains []ChainInfo, quotes map[string]*quotepancakev3.AppService) *QuotePancakeV3Handler {
	return &QuotePancakeV3Handler{quotesByChain: quotes, chains: newChainSelector(chains)}
}

// HandleQuote serves POST /api/v1/pancakev3/quote requests.
func (h *QuotePancakeV3Handler) HandleQuote(c *gin.Context) {
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
		c.JSON(http.StatusServiceUnavailable, errorHTTPResponse{Error: "pancakev3 quote service is not configured"})
		return
	}

	req, err := toQuotePancakeV3Request(payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
		return
	}

	resp, err := quotes.Quote(c.Request.Context(), req)
	if err != nil {
		c.JSON(quoteStatusCode(err), errorHTTPResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, toQuotePancakeV3HTTPResponse(resp))
}

func (h *QuotePancakeV3Handler) selectQuotes(chain string) (*quotepancakev3.AppService, bool) {
	if h.quotesByChain == nil {
		return h.quotes, true
	}
	key, ok := h.chains.selectKey(chain)
	if !ok {
		return nil, false
	}
	return h.quotesByChain[key], true
}

func toQuotePancakeV3Request(payload quoteHTTPRequest) (quotepancakev3.Request, error) {
	tokenIn, tokenOut, mode, amountIn, amountOut, err := parseQuoteBase(payload)
	if err != nil {
		return quotepancakev3.Request{}, err
	}
	if strings.TrimSpace(payload.PoolID) != "" {
		return quotepancakev3.Request{}, errPoolIDNotSupportedOnV3
	}

	req := quotepancakev3.Request{
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		Mode:      mode,
		AmountIn:  amountIn,
		AmountOut: amountOut,
	}
	if strings.TrimSpace(payload.PoolAddress) != "" {
		poolAddress, err := parseAddress(payload.PoolAddress, "poolAddress")
		if err != nil {
			return quotepancakev3.Request{}, err
		}
		req.PoolAddress = &poolAddress
	}
	return req, nil
}

func toQuotePancakeV3HTTPResponse(resp quotepancakev3.Response) quoteHTTPResponse {
	routeQuotes := make([]routeQuoteHTTPResponse, 0, len(resp.RouteQuotes))
	for _, item := range resp.RouteQuotes {
		routeQuotes = append(routeQuotes, routeQuoteHTTPResponse{
			Route:     toRoutePancakeV3HTTPResponse(item.Route),
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
		toRoutePancakeV3HTTPResponse(resp.BestRoute),
		routeQuotes,
	)
}

func toRoutePancakeV3HTTPResponse(route quotepancakev3domain.Route) routeHTTPResponse {
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
