package httpapi

import (
	"math/big"
	"net/http"
	"strconv"
	"strings"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
)

// OpportunityHandler exposes discovered arbitrage opportunities over HTTP.
type OpportunityHandler struct {
	repo        domainarb.OpportunityRepository
	repoByChain map[string]domainarb.OpportunityRepository
	chains      chainSelector
}

func NewOpportunityHandler(repo domainarb.OpportunityRepository) *OpportunityHandler {
	return &OpportunityHandler{repo: repo}
}

func NewOpportunityChainHandler(chains []ChainInfo, repos map[string]domainarb.OpportunityRepository) *OpportunityHandler {
	return &OpportunityHandler{repoByChain: repos, chains: newChainSelector(chains)}
}

type opportunityHTTPResponse struct {
	ID          string                 `json:"id"`
	StrategyID  string                 `json:"strategyId,omitempty"`
	Status      string                 `json:"status,omitempty"`
	PoolAddress string                 `json:"poolAddress,omitempty"`
	BlockNumber uint64                 `json:"blockNumber"`
	AmountIn    string                 `json:"amountIn,omitempty"`
	AmountOut   string                 `json:"amountOut,omitempty"`
	GrossProfit string                 `json:"grossProfit,omitempty"`
	GasCost     string                 `json:"gasCost,omitempty"`
	FlashLoan   *flashLoanHTTPResponse `json:"flashLoan,omitempty"`
	NetProfit   string                 `json:"netProfit,omitempty"`
	CreatedAt   string                 `json:"createdAt"`
}

type flashLoanHTTPResponse struct {
	Protocol string `json:"protocol,omitempty"`
	PoolRef  string `json:"poolRef,omitempty"`
	Amount   string `json:"amount,omitempty"`
	Fee      string `json:"fee,omitempty"`
	FeePPM   string `json:"feePpm,omitempty"`
}

type opportunitiesHTTPResponse struct {
	Items []opportunityHTTPResponse `json:"items"`
	Count int                       `json:"count"`
}

// HandleList serves GET /api/v1/opportunities.
func (h *OpportunityHandler) HandleList(c *gin.Context) {
	repo, ok := h.selectRepo(c.Query("chain"))
	if !ok {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: chainNotFoundMessage(c.Query("chain"))})
		return
	}
	if repo == nil {
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

	items, err := repo.List(c.Request.Context(), limit)
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

func (h *OpportunityHandler) selectRepo(chain string) (domainarb.OpportunityRepository, bool) {
	if h.repoByChain == nil {
		return h.repo, true
	}
	key, ok := h.chains.selectKey(chain)
	if !ok {
		return nil, false
	}
	return h.repoByChain[key], true
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
	if item.FlashLoan.Protocol != "" {
		resp.FlashLoan = &flashLoanHTTPResponse{
			Protocol: string(item.FlashLoan.Protocol),
			PoolRef:  item.FlashLoan.PoolRef.Key(),
			Amount:   bigIntString(item.FlashLoan.Amount),
			Fee:      bigIntString(item.FlashLoan.Fee),
			FeePPM:   bigIntString(item.FlashLoan.FeePPM),
		}
	}
	if item.NetProfit != nil {
		resp.NetProfit = item.NetProfit.String()
	}
	return resp
}

func bigIntString(value *big.Int) string {
	if value == nil {
		return ""
	}
	return value.String()
}

const timeRFC3339Nano = "2006-01-02T15:04:05.999999999Z07:00"
