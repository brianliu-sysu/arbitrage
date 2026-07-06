package httpapi

import (
	"errors"
	"math/big"
	"net/http"
	"strings"

	quoteapp "github.com/brianliu-sysu/uniswapv3/internal/application/quote"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

var (
	errPoolIDNotSupportedOnV3      = errors.New("poolId is not supported on univ3/quote; use poolAddress")
	errPoolAddressNotSupportedOnV4 = errors.New("poolAddress is not supported on univ4/quote; use poolId")
)

type quoteHTTPRequest struct {
	TokenIn     string `json:"tokenIn" binding:"required"`
	TokenOut    string `json:"tokenOut" binding:"required"`
	AmountIn    string `json:"amountIn,omitempty"`
	AmountOut   string `json:"amountOut,omitempty"`
	PoolAddress string `json:"poolAddress,omitempty"`
	PoolID      string `json:"poolId,omitempty"`
}

type routeHopHTTPResponse struct {
	PoolAddress string `json:"poolAddress,omitempty"`
	PoolID      string `json:"poolId,omitempty"`
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

func parseQuoteBase(payload quoteHTTPRequest) (common.Address, common.Address, quoteapp.QuoteMode, *big.Int, *big.Int, error) {
	return parseQuoteBaseAllowNative(payload, false)
}

func parseQuoteBaseAllowNative(payload quoteHTTPRequest, allowNativeETH bool) (common.Address, common.Address, quoteapp.QuoteMode, *big.Int, *big.Int, error) {
	tokenIn, err := parseTokenAddress(payload.TokenIn, "tokenIn", allowNativeETH)
	if err != nil {
		return common.Address{}, common.Address{}, 0, nil, nil, err
	}
	tokenOut, err := parseTokenAddress(payload.TokenOut, "tokenOut", allowNativeETH)
	if err != nil {
		return common.Address{}, common.Address{}, 0, nil, nil, err
	}

	hasAmountIn := strings.TrimSpace(payload.AmountIn) != ""
	hasAmountOut := strings.TrimSpace(payload.AmountOut) != ""
	if hasAmountIn == hasAmountOut {
		return common.Address{}, common.Address{}, 0, nil, nil, errors.New("exactly one of amountIn or amountOut must be provided")
	}

	var mode quoteapp.QuoteMode
	var amountIn, amountOut *big.Int
	if hasAmountIn {
		mode = quoteapp.QuoteModeExactInput
		amountIn, err = parsePositiveAmount(payload.AmountIn, "amountIn")
		if err != nil {
			return common.Address{}, common.Address{}, 0, nil, nil, err
		}
	} else {
		mode = quoteapp.QuoteModeExactOutput
		amountOut, err = parsePositiveAmount(payload.AmountOut, "amountOut")
		if err != nil {
			return common.Address{}, common.Address{}, 0, nil, nil, err
		}
	}

	return tokenIn, tokenOut, mode, amountIn, amountOut, nil
}

func parsePoolID(value string) (marketv4.PoolID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return marketv4.PoolID{}, errors.New("poolId is required")
	}
	if !strings.HasPrefix(value, "0x") {
		value = "0x" + value
	}
	if len(value) != 66 {
		return marketv4.PoolID{}, errors.New("poolId must be a 32-byte hex hash")
	}
	hash := common.HexToHash(value)
	if hash == (common.Hash{}) {
		return marketv4.PoolID{}, errors.New("poolId must be non-zero")
	}
	return marketv4.PoolID(hash), nil
}

func parseTokenAddress(value, field string, allowNativeETH bool) (common.Address, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return common.Address{}, errors.New(field + " is required")
	}
	if !common.IsHexAddress(value) {
		return common.Address{}, errors.New(field + " must be a valid hex address")
	}
	address := common.HexToAddress(value)
	if address == (common.Address{}) && !allowNativeETH {
		return common.Address{}, errors.New(field + " must be non-zero")
	}
	return address, nil
}

func parseAddress(value, field string) (common.Address, error) {
	return parseTokenAddress(value, field, false)
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

func quoteHTTPResponseFromAmounts(tokenIn, tokenOut common.Address, amountIn, amountOut, feeAmount *big.Int, bestRoute routeHTTPResponse, routeQuotes []routeQuoteHTTPResponse) quoteHTTPResponse {
	return quoteHTTPResponse{
		TokenIn:     tokenIn.Hex(),
		TokenOut:    tokenOut.Hex(),
		AmountIn:    amountIn.String(),
		AmountOut:   amountOut.String(),
		FeeAmount:   feeAmount.String(),
		BestRoute:   bestRoute,
		RouteQuotes: routeQuotes,
	}
}
