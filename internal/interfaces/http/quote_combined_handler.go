package httpapi

import (
	"errors"
	"net/http"
	"strings"

	quotecombined "github.com/brianliu-sysu/uniswapv3/internal/application/quote/combined"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/gin-gonic/gin"
)

// QuoteCombinedHandler exposes cross-protocol V3/V4 quote requests over HTTP.
type QuoteCombinedHandler struct {
	quotes *quotecombined.AppService
}

func NewQuoteCombinedHandler(quotes *quotecombined.AppService) *QuoteCombinedHandler {
	return &QuoteCombinedHandler{quotes: quotes}
}

type routeHopCombinedHTTPResponse struct {
	Version     string `json:"version"`
	PoolAddress string `json:"poolAddress,omitempty"`
	PoolID      string `json:"poolId,omitempty"`
	TokenIn     string `json:"tokenIn"`
	TokenOut    string `json:"tokenOut"`
}

type routeCombinedHTTPResponse struct {
	TokenIn  string                           `json:"tokenIn"`
	TokenOut string                           `json:"tokenOut"`
	Hops     []routeHopCombinedHTTPResponse `json:"hops"`
}

type routeQuoteCombinedHTTPResponse struct {
	Route     routeCombinedHTTPResponse `json:"route"`
	AmountIn  string                    `json:"amountIn"`
	AmountOut string                    `json:"amountOut"`
	FeeAmount string                    `json:"feeAmount"`
}

type quoteCombinedHTTPResponse struct {
	TokenIn     string                           `json:"tokenIn"`
	TokenOut    string                           `json:"tokenOut"`
	AmountIn    string                           `json:"amountIn"`
	AmountOut   string                           `json:"amountOut"`
	FeeAmount   string                           `json:"feeAmount"`
	BestRoute   routeCombinedHTTPResponse        `json:"bestRoute"`
	RouteQuotes []routeQuoteCombinedHTTPResponse `json:"routeQuotes,omitempty"`
}

// HandleQuote serves POST /api/v1/quote cross-pool quote requests.
func (h *QuoteCombinedHandler) HandleQuote(c *gin.Context) {
	if h.quotes == nil {
		c.JSON(http.StatusInternalServerError, errorHTTPResponse{Error: "cross-pool quote service is not configured"})
		return
	}

	var payload quoteHTTPRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: "invalid json body"})
		return
	}

	req, err := toQuoteCombinedRequest(payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
		return
	}

	resp, err := h.quotes.Quote(c.Request.Context(), req)
	if err != nil {
		c.JSON(quoteStatusCode(err), errorHTTPResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, toQuoteCombinedHTTPResponse(resp))
}

func toQuoteCombinedRequest(payload quoteHTTPRequest) (quotecombined.Request, error) {
	tokenIn, tokenOut, mode, amountIn, amountOut, err := parseQuoteBaseAllowNative(payload, true)
	if err != nil {
		return quotecombined.Request{}, err
	}

	hasPoolAddress := strings.TrimSpace(payload.PoolAddress) != ""
	hasPoolID := strings.TrimSpace(payload.PoolID) != ""
	if hasPoolAddress && hasPoolID {
		return quotecombined.Request{}, errors.New("only one of poolAddress or poolId may be provided")
	}

	req := quotecombined.Request{
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		Mode:      mode,
		AmountIn:  amountIn,
		AmountOut: amountOut,
	}
	if hasPoolAddress {
		poolAddress, err := parseAddress(payload.PoolAddress, "poolAddress")
		if err != nil {
			return quotecombined.Request{}, err
		}
		req.PoolAddress = &poolAddress
	}
	if hasPoolID {
		poolID, err := parsePoolID(payload.PoolID)
		if err != nil {
			return quotecombined.Request{}, err
		}
		req.PoolID = &poolID
	}
	return req, nil
}

func toQuoteCombinedHTTPResponse(resp quotecombined.Response) quoteCombinedHTTPResponse {
	routeQuotes := make([]routeQuoteCombinedHTTPResponse, 0, len(resp.RouteQuotes))
	for _, item := range resp.RouteQuotes {
		routeQuotes = append(routeQuotes, routeQuoteCombinedHTTPResponse{
			Route:     toRouteCombinedHTTPResponse(item.Route),
			AmountIn:  item.AmountIn.String(),
			AmountOut: item.AmountOut.String(),
			FeeAmount: item.FeeAmount.String(),
		})
	}

	return quoteCombinedHTTPResponse{
		TokenIn:     resp.TokenIn.Hex(),
		TokenOut:    resp.TokenOut.Hex(),
		AmountIn:    resp.AmountIn.String(),
		AmountOut:   resp.AmountOut.String(),
		FeeAmount:   resp.FeeAmount.String(),
		BestRoute:   toRouteCombinedHTTPResponse(resp.BestRoute),
		RouteQuotes: routeQuotes,
	}
}

func toRouteCombinedHTTPResponse(route quoteunified.Route) routeCombinedHTTPResponse {
	hops := make([]routeHopCombinedHTTPResponse, 0, len(route.Hops))
	for _, hop := range route.Hops {
		item := routeHopCombinedHTTPResponse{
			Version:  hop.Version.String(),
			TokenIn:  hop.TokenIn.Hex(),
			TokenOut: hop.TokenOut.Hex(),
		}
		switch hop.Version {
		case quoteunified.PoolVersionV3:
			item.PoolAddress = hop.PoolV3.Hex()
		case quoteunified.PoolVersionPancakeV3:
			item.PoolAddress = hop.PoolPancakeV3.Hex()
		case quoteunified.PoolVersionV4:
			item.PoolID = hop.PoolV4.String()
		}
		hops = append(hops, item)
	}
	return routeCombinedHTTPResponse{
		TokenIn:  route.TokenIn.Hex(),
		TokenOut: route.TokenOut.Hex(),
		Hops:     hops,
	}
}
