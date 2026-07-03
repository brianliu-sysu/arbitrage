package httpapi

import (
	"errors"
	"math/big"
	"net/http"
	"strings"

	quoteapp "github.com/brianliu-sysu/uniswapv3/internal/application/quote"
	domainquote "github.com/brianliu-sysu/uniswapv3/internal/domain/quote"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
)

// QuoteHandler exposes the quote use case over HTTP.
type QuoteHandler struct {
	quotes *quoteapp.QuoteAppService
}

func NewQuoteHandler(quotes *quoteapp.QuoteAppService) *QuoteHandler {
	return &QuoteHandler{quotes: quotes}
}

type quoteHTTPRequest struct {
	TokenIn     string `json:"tokenIn" binding:"required"`
	TokenOut    string `json:"tokenOut" binding:"required"`
	AmountIn    string `json:"amountIn,omitempty"`
	AmountOut   string `json:"amountOut,omitempty"`
	PoolAddress string `json:"poolAddress,omitempty"`
}

type routeHopHTTPResponse struct {
	PoolAddress string `json:"poolAddress"`
	TokenIn     string `json:"tokenIn"`
	TokenOut    string `json:"tokenOut"`
}

type routeHTTPResponse struct {
	TokenIn  string                 `json:"tokenIn"`
	TokenOut string                 `json:"tokenOut"`
	Hops     []routeHopHTTPResponse `json:"hops"`
}

type routeQuoteHTTPResponse struct {
	Route     routeHTTPResponse `json:"route"`
	AmountIn  string            `json:"amountIn"`
	AmountOut string            `json:"amountOut"`
	FeeAmount string            `json:"feeAmount"`
}

type quoteHTTPResponse struct {
	TokenIn     string                   `json:"tokenIn"`
	TokenOut    string                   `json:"tokenOut"`
	AmountIn    string                   `json:"amountIn"`
	AmountOut   string                   `json:"amountOut"`
	FeeAmount   string                   `json:"feeAmount"`
	BestRoute   routeHTTPResponse        `json:"bestRoute"`
	RouteQuotes []routeQuoteHTTPResponse `json:"routeQuotes,omitempty"`
}

type errorHTTPResponse struct {
	Error string `json:"error"`
}

// HandleQuote serves POST /quote requests.
func (h *QuoteHandler) HandleQuote(c *gin.Context) {
	if h.quotes == nil {
		c.JSON(http.StatusInternalServerError, errorHTTPResponse{Error: "quote service is not configured"})
		return
	}

	var payload quoteHTTPRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: "invalid json body"})
		return
	}

	req, err := toQuoteRequest(payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
		return
	}

	resp, err := h.quotes.Quote(c.Request.Context(), req)
	if err != nil {
		c.JSON(quoteStatusCode(err), errorHTTPResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, toQuoteHTTPResponse(resp))
}

func toQuoteRequest(payload quoteHTTPRequest) (quoteapp.QuoteRequest, error) {
	tokenIn, err := parseAddress(payload.TokenIn, "tokenIn")
	if err != nil {
		return quoteapp.QuoteRequest{}, err
	}
	tokenOut, err := parseAddress(payload.TokenOut, "tokenOut")
	if err != nil {
		return quoteapp.QuoteRequest{}, err
	}

	hasAmountIn := strings.TrimSpace(payload.AmountIn) != ""
	hasAmountOut := strings.TrimSpace(payload.AmountOut) != ""
	if hasAmountIn == hasAmountOut {
		return quoteapp.QuoteRequest{}, errors.New("exactly one of amountIn or amountOut must be provided")
	}

	req := quoteapp.QuoteRequest{
		TokenIn:  tokenIn,
		TokenOut: tokenOut,
	}
	if hasAmountIn {
		amountIn, err := parsePositiveAmount(payload.AmountIn, "amountIn")
		if err != nil {
			return quoteapp.QuoteRequest{}, err
		}
		req.Mode = quoteapp.QuoteModeExactInput
		req.AmountIn = amountIn
	} else {
		amountOut, err := parsePositiveAmount(payload.AmountOut, "amountOut")
		if err != nil {
			return quoteapp.QuoteRequest{}, err
		}
		req.Mode = quoteapp.QuoteModeExactOutput
		req.AmountOut = amountOut
	}

	if strings.TrimSpace(payload.PoolAddress) != "" {
		poolAddress, err := parseAddress(payload.PoolAddress, "poolAddress")
		if err != nil {
			return quoteapp.QuoteRequest{}, err
		}
		req.PoolAddress = &poolAddress
	}

	return req, nil
}

func parseAddress(value, field string) (common.Address, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return common.Address{}, errors.New(field + " is required")
	}
	if !common.IsHexAddress(value) {
		return common.Address{}, errors.New(field + " must be a valid hex address")
	}
	address := common.HexToAddress(value)
	if address == (common.Address{}) {
		return common.Address{}, errors.New(field + " must be non-zero")
	}
	return address, nil
}

func parsePositiveAmount(value, field string) (*big.Int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New(field + " is required")
	}
	amount, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return nil, errors.New(field + " must be a base-10 integer")
	}
	if amount.Sign() <= 0 {
		return nil, errors.New(field + " must be positive")
	}
	return amount, nil
}

func toQuoteHTTPResponse(resp quoteapp.QuoteResponse) quoteHTTPResponse {
	routeQuotes := make([]routeQuoteHTTPResponse, 0, len(resp.RouteQuotes))
	for _, item := range resp.RouteQuotes {
		routeQuotes = append(routeQuotes, routeQuoteHTTPResponse{
			Route:     toRouteHTTPResponse(item.Route),
			AmountIn:  item.AmountIn.String(),
			AmountOut: item.AmountOut.String(),
			FeeAmount: item.FeeAmount.String(),
		})
	}

	return quoteHTTPResponse{
		TokenIn:     resp.TokenIn.Hex(),
		TokenOut:    resp.TokenOut.Hex(),
		AmountIn:    resp.AmountIn.String(),
		AmountOut:   resp.AmountOut.String(),
		FeeAmount:   resp.FeeAmount.String(),
		BestRoute:   toRouteHTTPResponse(resp.BestRoute),
		RouteQuotes: routeQuotes,
	}
}

func toRouteHTTPResponse(route domainquote.Route) routeHTTPResponse {
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

func quoteStatusCode(err error) int {
	message := err.Error()
	switch {
	case strings.Contains(message, "not ready"):
		return http.StatusServiceUnavailable
	case strings.Contains(message, "not found"), strings.Contains(message, "no route found"), strings.Contains(message, "no quotable route"):
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}
