package httpapi

import (
	"net/http"
	"strings"

	quoteuniv4 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/univ4"
	quoteuniv4domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ4"
	"github.com/gin-gonic/gin"
)

// QuoteV4Handler exposes Uniswap V4 quote requests over HTTP.
type QuoteV4Handler struct {
	quotes *quoteuniv4.AppService
}

func NewQuoteV4Handler(quotes *quoteuniv4.AppService) *QuoteV4Handler {
	return &QuoteV4Handler{quotes: quotes}
}

// HandleQuote serves POST /api/v1/univ4/quote requests.
func (h *QuoteV4Handler) HandleQuote(c *gin.Context) {
	if h.quotes == nil {
		c.JSON(http.StatusServiceUnavailable, errorHTTPResponse{Error: "v4 quote service is not configured"})
		return
	}

	var payload quoteHTTPRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: "invalid json body"})
		return
	}

	req, err := toQuoteV4Request(payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
		return
	}

	resp, err := h.quotes.Quote(c.Request.Context(), req)
	if err != nil {
		c.JSON(quoteStatusCode(err), errorHTTPResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, toQuoteV4HTTPResponse(resp))
}

func toQuoteV4Request(payload quoteHTTPRequest) (quoteuniv4.Request, error) {
	tokenIn, tokenOut, mode, amountIn, amountOut, err := parseQuoteBaseAllowNative(payload, true)
	if err != nil {
		return quoteuniv4.Request{}, err
	}
	if strings.TrimSpace(payload.PoolAddress) != "" {
		return quoteuniv4.Request{}, errPoolAddressNotSupportedOnV4
	}

	req := quoteuniv4.Request{
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		Mode:      mode,
		AmountIn:  amountIn,
		AmountOut: amountOut,
	}
	if strings.TrimSpace(payload.PoolID) != "" {
		poolID, err := parsePoolID(payload.PoolID)
		if err != nil {
			return quoteuniv4.Request{}, err
		}
		req.PoolID = &poolID
	}
	return req, nil
}

func toQuoteV4HTTPResponse(resp quoteuniv4.Response) quoteHTTPResponse {
	routeQuotes := make([]routeQuoteHTTPResponse, 0, len(resp.RouteQuotes))
	for _, item := range resp.RouteQuotes {
		routeQuotes = append(routeQuotes, routeQuoteHTTPResponse{
			Route:     toRouteV4HTTPResponse(item.Route),
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
		toRouteV4HTTPResponse(resp.BestRoute),
		routeQuotes,
	)
}

func toRouteV4HTTPResponse(route quoteuniv4domain.Route) routeHTTPResponse {
	hops := make([]routeHopHTTPResponse, 0, len(route.Hops))
	for _, hop := range route.Hops {
		hops = append(hops, routeHopHTTPResponse{
			PoolID:   hop.PoolID.String(),
			TokenIn:  hop.TokenIn.Hex(),
			TokenOut: hop.TokenOut.Hex(),
		})
	}
	return routeHTTPResponse{
		TokenIn:  route.TokenIn.Hex(),
		TokenOut: route.TokenOut.Hex(),
		Hops:     hops,
	}
}
