package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	arbitrageapp "github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage"
	contractapp "github.com/brianliu-sysu/uniswapv3/internal/application/contract"
	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
)

type ContractExecutorHandler struct {
	executor         *contractapp.AppService
	executorsByChain map[string]*contractapp.AppService
	chains           chainSelector
}

func NewContractExecutorHandler(executor *contractapp.AppService) *ContractExecutorHandler {
	return &ContractExecutorHandler{executor: executor}
}

func NewContractExecutorChainHandler(chains []ChainInfo, executors map[string]*contractapp.AppService) *ContractExecutorHandler {
	return &ContractExecutorHandler{
		executorsByChain: executors,
		chains:           newChainSelector(chains),
	}
}

// Minimal execute request: gas/nonce are resolved internally via EstimateGas / SuggestGasPrice / PendingNonceAt.
type executeContractHTTPRequest struct {
	RPCURL     string          `json:"rpcUrl"`
	PrivateKey string          `json:"privateKey"`
	Executor   string          `json:"executor"`
	Execution  json.RawMessage `json:"execution"`
}

type executeContractHTTPResponse struct {
	TxHash           string   `json:"txHash,omitempty"`
	ApprovalTxHashes []string `json:"approvalTxHashes,omitempty"`
	Interrupted      bool     `json:"interrupted,omitempty"`
}

func (h *ContractExecutorHandler) HandleExecute(c *gin.Context) {
	executor, ok := h.selectExecutor(c.Query("chain"))
	if !ok {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: chainNotFoundMessage(c.Query("chain"))})
		return
	}
	if executor == nil {
		c.JSON(http.StatusInternalServerError, errorHTTPResponse{Error: "contract executor service is not configured"})
		return
	}

	var payload executeContractHTTPRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: "invalid json body"})
		return
	}

	opportunity, err := toOpportunity(payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
		return
	}
	plan, approvals, err := arbitrageapp.NewPayloadExecutionPlanBuilder().BuildExecutionPlan(c.Request.Context(), opportunity)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
		return
	}

	req, err := toContractBroadcastRequest(payload, plan)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
		return
	}

	approvalResp, err := executor.EnsureApprovals(c.Request.Context(), domaincontract.EnsureApprovalsRequest{
		RPCURL:     req.RPCURL,
		PrivateKey: req.PrivateKey,
		Executor:   req.Executor,
		Approvals:  approvals,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
		return
	}
	if approvalResp.Broadcast {
		c.JSON(http.StatusOK, executeContractHTTPResponse{
			ApprovalTxHashes: hashesToHex(approvalResp.TxHashes),
			Interrupted:      true,
		})
		return
	}

	resp, err := executor.Execute(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, executeContractHTTPResponse{TxHash: resp.TxHash.Hex()})
}

func (h *ContractExecutorHandler) selectExecutor(chain string) (*contractapp.AppService, bool) {
	if h == nil {
		return nil, false
	}
	if h.executor != nil {
		return h.executor, true
	}
	key, ok := h.chains.selectKey(chain)
	if !ok {
		return nil, false
	}
	executor, ok := h.executorsByChain[key]
	return executor, ok
}

func toOpportunity(payload executeContractHTTPRequest) (*domainarb.Opportunity, error) {
	if len(payload.Execution) == 0 {
		return nil, fmt.Errorf("execution is required")
	}
	rawPayload, err := json.Marshal(struct {
		Execution json.RawMessage `json:"execution"`
	}{Execution: payload.Execution})
	if err != nil {
		return nil, fmt.Errorf("encode opportunity payload: %w", err)
	}
	opportunity := &domainarb.Opportunity{Payload: rawPayload}
	if err := opportunity.ApplyPayload(); err != nil {
		return nil, err
	}
	return opportunity, nil
}

func toContractBroadcastRequest(
	payload executeContractHTTPRequest,
	plan domaincontract.ExecutionPlan,
) (domaincontract.BroadcastRequest, error) {
	executor, err := parseAddress(payload.Executor, "executor")
	if err != nil {
		return domaincontract.BroadcastRequest{}, err
	}
	return domaincontract.BroadcastRequest{
		RPCURL:     strings.TrimSpace(payload.RPCURL),
		PrivateKey: strings.TrimSpace(payload.PrivateKey),
		Executor:   executor,
		Plan:       plan,
		// GasLimit / GasPriceWei / Nonce left empty so the broadcaster estimates them.
	}, nil
}

func hashesToHex(hashes []common.Hash) []string {
	values := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		values = append(values, hash.Hex())
	}
	return values
}
