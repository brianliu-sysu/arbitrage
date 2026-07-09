package httpapi

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	contractapp "github.com/brianliu-sysu/uniswapv3/internal/application/contract"
	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
)

type ContractExecutorHandler struct {
	executor *contractapp.AppService
}

func NewContractExecutorHandler(executor *contractapp.AppService) *ContractExecutorHandler {
	return &ContractExecutorHandler{executor: executor}
}

type executeContractHTTPRequest struct {
	RPCURL           string                 `json:"rpcUrl"`
	PrivateKey       string                 `json:"privateKey"`
	Executor         string                 `json:"executor"`
	FlashLoan        flashLoanHTTPRequest   `json:"flashLoan"`
	Routes           []swapRouteHTTPRequest `json:"routes"`
	SettleCurrencies []string               `json:"settleCurrencies,omitempty"`
	ProfitToken      string                 `json:"profitToken"`
	MinProfit        string                 `json:"minProfit,omitempty"`
	Deadline         string                 `json:"deadline,omitempty"`
	GasLimit         uint64                 `json:"gasLimit,omitempty"`
	GasPriceWei      string                 `json:"gasPriceWei,omitempty"`
	Nonce            *uint64                `json:"nonce,omitempty"`
	SkipEstimate     bool                   `json:"skipEstimate,omitempty"`
}

type flashLoanHTTPRequest struct {
	Protocol     string `json:"protocol"`
	Lender       string `json:"lender"`
	Token        string `json:"token"`
	Amount       string `json:"amount"`
	BorrowToken0 bool   `json:"borrowToken0,omitempty"`
}

type swapRouteHTTPRequest struct {
	RouterAddress string `json:"routerAddress"`
	Value         string `json:"value,omitempty"`
	Data          string `json:"data"`
	FillToken     string `json:"fillToken,omitempty"`
	FillOffset    string `json:"fillOffset,omitempty"`
}

type executeContractHTTPResponse struct {
	TxHash string `json:"txHash"`
}

func (h *ContractExecutorHandler) HandleExecute(c *gin.Context) {
	if h == nil || h.executor == nil {
		c.JSON(http.StatusInternalServerError, errorHTTPResponse{Error: "contract executor service is not configured"})
		return
	}

	var payload executeContractHTTPRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: "invalid json body"})
		return
	}

	req, err := toContractBroadcastRequest(payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
		return
	}

	resp, err := h.executor.Execute(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, executeContractHTTPResponse{TxHash: resp.TxHash.Hex()})
}

func toContractBroadcastRequest(payload executeContractHTTPRequest) (domaincontract.BroadcastRequest, error) {
	executor, err := parseAddress(payload.Executor, "executor")
	if err != nil {
		return domaincontract.BroadcastRequest{}, err
	}
	loan, err := toFlashLoan(payload.FlashLoan)
	if err != nil {
		return domaincontract.BroadcastRequest{}, err
	}
	routes, err := toSwapRoutes(payload.Routes)
	if err != nil {
		return domaincontract.BroadcastRequest{}, err
	}
	settleCurrencies, err := toSettleCurrencies(payload.SettleCurrencies)
	if err != nil {
		return domaincontract.BroadcastRequest{}, err
	}
	profitToken, err := parseAddress(payload.ProfitToken, "profitToken")
	if err != nil {
		return domaincontract.BroadcastRequest{}, err
	}
	minProfit, err := parseOptionalNonNegativeAmount(payload.MinProfit, "minProfit")
	if err != nil {
		return domaincontract.BroadcastRequest{}, err
	}
	deadline, err := parseOptionalNonNegativeAmount(payload.Deadline, "deadline")
	if err != nil {
		return domaincontract.BroadcastRequest{}, err
	}
	gasPriceWei, err := parseOptionalPositiveAmount(payload.GasPriceWei, "gasPriceWei")
	if err != nil {
		return domaincontract.BroadcastRequest{}, err
	}

	return domaincontract.BroadcastRequest{
		RPCURL:     strings.TrimSpace(payload.RPCURL),
		PrivateKey: strings.TrimSpace(payload.PrivateKey),
		Executor:   executor,
		Plan: domaincontract.ExecutionPlan{
			Loan:             loan,
			Routes:           routes,
			SettleCurrencies: settleCurrencies,
			ProfitToken:      profitToken,
			MinProfit:        minProfit,
			Deadline:         deadline,
		},
		GasLimit:     payload.GasLimit,
		GasPriceWei:  gasPriceWei,
		Nonce:        payload.Nonce,
		SkipEstimate: payload.SkipEstimate,
	}, nil
}

func toFlashLoan(payload flashLoanHTTPRequest) (domaincontract.FlashLoan, error) {
	lender, err := parseAddress(payload.Lender, "flashLoan.lender")
	if err != nil {
		return domaincontract.FlashLoan{}, err
	}
	protocol, err := parseFlashLoanProtocol(payload.Protocol)
	if err != nil {
		return domaincontract.FlashLoan{}, err
	}
	token, err := parseTokenAddress(payload.Token, "flashLoan.token", protocol == domaincontract.FlashLoanProtocolUniswapV4)
	if err != nil {
		return domaincontract.FlashLoan{}, err
	}
	amount, err := parsePositiveAmount(payload.Amount, "flashLoan.amount")
	if err != nil {
		return domaincontract.FlashLoan{}, err
	}
	return domaincontract.FlashLoan{
		Protocol:     protocol,
		Lender:       lender,
		Token:        token,
		Amount:       amount,
		BorrowToken0: payload.BorrowToken0,
	}, nil
}

func toSwapRoutes(routePayload []swapRouteHTTPRequest) ([]domaincontract.SwapRoute, error) {
	routes := make([]domaincontract.SwapRoute, 0, len(routePayload))
	for _, item := range routePayload {
		routerAddress, err := parseAddress(item.RouterAddress, "routes.routerAddress")
		if err != nil {
			return nil, err
		}
		value, err := parseOptionalNonNegativeAmount(item.Value, "routes.value")
		if err != nil {
			return nil, err
		}
		data, err := parseHexBytes(item.Data, "routes.data")
		if err != nil {
			return nil, err
		}
		fillToken := common.Address{}
		if strings.TrimSpace(item.FillToken) != "" {
			fillToken, err = parseAddress(item.FillToken, "routes.fillToken")
			if err != nil {
				return nil, err
			}
		}
		fillOffset, err := parseOptionalNonNegativeAmount(item.FillOffset, "routes.fillOffset")
		if err != nil {
			return nil, err
		}
		var fillOffsetU64 uint64
		if fillOffset != nil {
			if !fillOffset.IsUint64() {
				return nil, errors.New("routes.fillOffset exceeds uint64")
			}
			fillOffsetU64 = fillOffset.Uint64()
		}
		routes = append(routes, domaincontract.SwapRoute{
			RouterAddress: routerAddress,
			Value:         value,
			Data:          data,
			FillToken:     fillToken,
			FillOffset:    fillOffsetU64,
		})
	}
	return routes, nil
}

func toSettleCurrencies(values []string) ([]common.Address, error) {
	currencies := make([]common.Address, 0, len(values))
	for i, value := range values {
		currency, err := parseTokenAddress(value, fmt.Sprintf("settleCurrencies[%d]", i), true)
		if err != nil {
			return nil, err
		}
		currencies = append(currencies, currency)
	}
	return currencies, nil
}

func parseFlashLoanProtocol(value string) (domaincontract.FlashLoanProtocol, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "balancer":
		return domaincontract.FlashLoanProtocolBalancer, nil
	case "uniswapv3", "univ3", "v3":
		return domaincontract.FlashLoanProtocolUniswapV3, nil
	case "uniswapv4", "univ4", "v4":
		return domaincontract.FlashLoanProtocolUniswapV4, nil
	default:
		return "", errors.New("flashLoan.protocol must be one of balancer, uniswapV3, uniswapV4")
	}
}

func parseOptionalNonNegativeAmount(value, field string) (*big.Int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	amount, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return nil, errors.New(field + " must be a base-10 integer")
	}
	if amount.Sign() < 0 {
		return nil, errors.New(field + " must be non-negative")
	}
	return amount, nil
}

func parseOptionalPositiveAmount(value, field string) (*big.Int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	amount, err := parsePositiveAmount(value, field)
	if err != nil {
		return nil, err
	}
	return amount, nil
}

func parseHexBytes(value, field string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New(field + " is required")
	}
	value = strings.TrimPrefix(value, "0x")
	if len(value)%2 != 0 {
		return nil, errors.New(field + " must be even-length hex")
	}
	data, err := hex.DecodeString(value)
	if err != nil {
		return nil, errors.New(field + " must be valid hex")
	}
	return data, nil
}
