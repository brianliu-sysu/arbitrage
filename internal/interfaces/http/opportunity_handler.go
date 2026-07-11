package httpapi

import (
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	arbitrageapp "github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage"
	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
)

// OpportunityHandler exposes discovered arbitrage opportunities over HTTP.
type OpportunityHandler struct {
	repo            domainarb.OpportunityRepository
	repoByChain     map[string]domainarb.OpportunityRepository
	executor        *arbitrageapp.OpportunityExecutor
	executorByChain map[string]*arbitrageapp.OpportunityExecutor
	chains          chainSelector
}

func NewOpportunityHandler(repo domainarb.OpportunityRepository) *OpportunityHandler {
	return &OpportunityHandler{repo: repo}
}

func NewOpportunityChainHandler(
	chains []ChainInfo,
	repos map[string]domainarb.OpportunityRepository,
	executors map[string]*arbitrageapp.OpportunityExecutor,
) *OpportunityHandler {
	return &OpportunityHandler{
		repoByChain:     repos,
		executorByChain: executors,
		chains:          newChainSelector(chains),
	}
}

type opportunityHTTPResponse struct {
	ID          string                  `json:"id"`
	StrategyID  string                  `json:"strategyId,omitempty"`
	Status      string                  `json:"status,omitempty"`
	PoolAddress string                  `json:"poolAddress,omitempty"`
	BlockNumber uint64                  `json:"blockNumber"`
	AmountIn    string                  `json:"amountIn,omitempty"`
	AmountOut   string                  `json:"amountOut,omitempty"`
	GrossProfit string                  `json:"grossProfit,omitempty"`
	GasCost     string                  `json:"gasCost,omitempty"`
	FlashLoan   *flashLoanHTTPResponse  `json:"flashLoan,omitempty"`
	NetProfit   string                  `json:"netProfit,omitempty"`
	QuoteSteps  []quoteStepHTTPResponse `json:"quoteSteps,omitempty"`
	CreatedAt   string                  `json:"createdAt"`
}

type quoteStepHTTPResponse struct {
	Index     int    `json:"index"`
	Version   string `json:"version,omitempty"`
	TokenIn   string `json:"tokenIn"`
	TokenOut  string `json:"tokenOut"`
	AmountIn  string `json:"amountIn"`
	AmountOut string `json:"amountOut"`
	FeeAmount string `json:"feeAmount,omitempty"`
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

type opportunityExecuteHTTPResponse struct {
	OpportunityID    string          `json:"opportunityId"`
	TxHash           string          `json:"txHash,omitempty"`
	ApprovalTxHashes []string        `json:"approvalTxHashes,omitempty"`
	Interrupted      bool            `json:"interrupted,omitempty"`
	Execution        json.RawMessage `json:"execution"`
}

type opportunityExecuteHTTPRequest struct {
	Confirm        bool   `json:"confirm"`
	BroadcastToken string `json:"broadcastToken,omitempty"`
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

func (h *OpportunityHandler) HandleExecute(c *gin.Context) {
	opportunityID := strings.TrimSpace(c.Param("opportunityID"))
	executor, ok := h.selectExecutor(c.Query("chain"))
	if !ok {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: chainNotFoundMessage(c.Query("chain"))})
		return
	}
	if executor == nil {
		c.JSON(http.StatusInternalServerError, errorHTTPResponse{Error: "opportunity executor is not configured"})
		return
	}

	var payload opportunityExecuteHTTPRequest
	if c.Request.Body != nil {
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: "invalid json body"})
			return
		}
	}

	result, err := executor.Execute(c.Request.Context(), arbitrageapp.OpportunityExecuteRequest{
		OpportunityID: opportunityID,
		Confirm:       payload.Confirm,
		AuthToken:     executionAuthToken(c, payload.BroadcastToken),
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, domainarb.ErrOpportunityNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, errorHTTPResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, opportunityExecuteHTTPResponse{
		OpportunityID:    result.OpportunityID,
		TxHash:           hashHex(result.TxHash),
		ApprovalTxHashes: hashesToHex(result.ApprovalTxHashes),
		Interrupted:      result.Interrupted,
		Execution:        result.ExecutionJSON,
	})
}

func executionAuthToken(c *gin.Context, fallback string) string {
	auth := strings.TrimSpace(c.GetHeader("Authorization"))
	const prefix = "Bearer "
	if strings.HasPrefix(auth, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(auth, prefix))
	}
	if auth != "" {
		return auth
	}
	return strings.TrimSpace(fallback)
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

func (h *OpportunityHandler) selectExecutor(chain string) (*arbitrageapp.OpportunityExecutor, bool) {
	if h.executorByChain == nil {
		return h.executor, true
	}
	key, ok := h.chains.selectKey(chain)
	if !ok {
		return nil, false
	}
	return h.executorByChain[key], true
}

func toOpportunityHTTPResponse(item *domainarb.Opportunity) opportunityHTTPResponse {
	if item == nil {
		return opportunityHTTPResponse{}
	}
	_ = item.ApplyPayload()

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
	resp.QuoteSteps = quoteStepsHTTPResponse(item.QuoteSteps)
	return resp
}

func quoteStepsHTTPResponse(steps []domainarb.OpportunityQuoteStep) []quoteStepHTTPResponse {
	if len(steps) == 0 {
		return nil
	}
	out := make([]quoteStepHTTPResponse, 0, len(steps))
	for _, step := range steps {
		out = append(out, quoteStepHTTPResponse{
			Index:     step.Index,
			Version:   step.Version,
			TokenIn:   step.TokenIn.Hex(),
			TokenOut:  step.TokenOut.Hex(),
			AmountIn:  bigIntString(step.AmountIn),
			AmountOut: bigIntString(step.AmountOut),
			FeeAmount: bigIntString(step.FeeAmount),
		})
	}
	return out
}

func bigIntString(value *big.Int) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func hashHex(hash common.Hash) string {
	if hash == (common.Hash{}) {
		return ""
	}
	return hash.Hex()
}

const timeRFC3339Nano = "2006-01-02T15:04:05.999999999Z07:00"
