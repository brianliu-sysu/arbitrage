package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
)

// OpportunityHandler exposes discovered arbitrage opportunities over HTTP.
type OpportunityHandler struct {
	repo domainarb.OpportunityRepository
}

func NewOpportunityHandler(repo domainarb.OpportunityRepository) *OpportunityHandler {
	return &OpportunityHandler{repo: repo}
}

type opportunityHTTPResponse struct {
	ID          string `json:"id"`
	StrategyID  string `json:"strategyId,omitempty"`
	Status      string `json:"status,omitempty"`
	PoolAddress string `json:"poolAddress,omitempty"`
	BlockNumber uint64 `json:"blockNumber"`
	AmountIn    string `json:"amountIn,omitempty"`
	AmountOut   string `json:"amountOut,omitempty"`
	GrossProfit string `json:"grossProfit,omitempty"`
	GasCost     string `json:"gasCost,omitempty"`
	NetProfit   string `json:"netProfit,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

type opportunitiesHTTPResponse struct {
	Items []opportunityHTTPResponse `json:"items"`
	Count int                       `json:"count"`
}

// HandleList serves GET /api/v1/opportunities.
func (h *OpportunityHandler) HandleList(c *gin.Context) {
	if h.repo == nil {
		c.JSON(http.StatusInternalServerError, errorHTTPResponse{Error: "opportunity repository is not configured"})
		return
	}

	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: "limit must be a non-negative integer"})
			return
		}
		limit = parsed
	}

	items, err := h.repo.List(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorHTTPResponse{Error: err.Error()})
		return
	}

	response := opportunitiesHTTPResponse{
		Items: make([]opportunityHTTPResponse, 0, len(items)),
		Count: len(items),
	}
	for _, item := range items {
		response.Items = append(response.Items, toOpportunityHTTPResponse(item))
	}
	c.JSON(http.StatusOK, response)
}

func toOpportunityHTTPResponse(item *domainarb.Opportunity) opportunityHTTPResponse {
	if item == nil {
		return opportunityHTTPResponse{}
	}

	resp := opportunityHTTPResponse{
		ID:          item.ID,
		StrategyID:  item.StrategyID,
		Status:      string(item.Status),
		BlockNumber: item.BlockNumber,
		CreatedAt:   item.CreatedAt.UTC().Format(timeRFC3339Nano),
	}
	if item.PoolAddress != (common.Address{}) {
		resp.PoolAddress = item.PoolAddress.Hex()
	}
	if item.AmountIn != nil {
		resp.AmountIn = item.AmountIn.String()
	}
	if item.AmountOut != nil {
		resp.AmountOut = item.AmountOut.String()
	}
	if item.GrossProfit != nil {
		resp.GrossProfit = item.GrossProfit.String()
	}
	if item.GasCost != nil {
		resp.GasCost = item.GasCost.String()
	}
	if item.NetProfit != nil {
		resp.NetProfit = item.NetProfit.String()
	}
	return resp
}

const timeRFC3339Nano = "2006-01-02T15:04:05.999999999Z07:00"
