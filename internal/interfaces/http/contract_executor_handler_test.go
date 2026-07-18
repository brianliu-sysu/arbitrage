package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	contractapp "github.com/brianliu-sysu/uniswapv3/internal/application/contract"
	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	httpapi "github.com/brianliu-sysu/uniswapv3/internal/interfaces/http"
	"github.com/ethereum/go-ethereum/common"
)

func TestContractExecutorHandlerBroadcastsExecutionPlan(t *testing.T) {
	broadcaster := &fakeContractBroadcaster{txHash: common.HexToHash("0xabc")}
	router := httpapi.NewRouter(httpapi.Handlers{
		ContractExecutor: httpapi.NewContractExecutorHandler(contractapp.NewAppService(broadcaster)),
	})

	body := bytes.NewReader([]byte(`{
		"rpcUrl":"http://127.0.0.1:8545",
		"privateKey":"0xabc123",
		"executor":"0x00000000000000000000000000000000000000aa",
		"execution":{
			"flashLoan":{
				"protocol":"uniswapV3",
				"lender":"0x00000000000000000000000000000000000000bb",
				"token":"0x00000000000000000000000000000000000000cc",
				"amount":"1000",
				"borrowToken0":true
			},
			"routes":[{
				"routerAddress":"0x00000000000000000000000000000000000000dd",
				"value":"0",
				"data":"0x1234"
			}],
			"profitToken":"0x00000000000000000000000000000000000000cc",
			"minProfit":"10",
			"deadline":"999"
		}
	}`))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/arbitrage/execute", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["txHash"] != common.HexToHash("0xabc").Hex() {
		t.Fatalf("unexpected txHash %q", resp["txHash"])
	}
	if broadcaster.req.Plan.Loan.Protocol != domaincontract.FlashLoanProtocolUniswapV3 {
		t.Fatalf("unexpected flash loan protocol %q", broadcaster.req.Plan.Loan.Protocol)
	}
	if broadcaster.req.Plan.Loan.Amount.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("unexpected loan amount %s", broadcaster.req.Plan.Loan.Amount)
	}
	if len(broadcaster.req.Plan.Routes) != 1 || !bytes.Equal(broadcaster.req.Plan.Routes[0].Data, []byte{0x12, 0x34}) {
		t.Fatalf("unexpected routes %#v", broadcaster.req.Plan.Routes)
	}
	if broadcaster.req.SkipEstimate {
		t.Fatalf("expected skipEstimate=false so gas is estimated internally")
	}
	if broadcaster.req.GasLimit != 0 || broadcaster.req.GasPriceWei != nil || broadcaster.req.Nonce != nil {
		t.Fatalf("expected gas/nonce to be left for internal resolution, got limit=%d price=%v nonce=%v",
			broadcaster.req.GasLimit, broadcaster.req.GasPriceWei, broadcaster.req.Nonce)
	}
	if broadcaster.req.SubmitRPCURL != "" {
		t.Fatalf("expected submitRpcUrl empty, got %q", broadcaster.req.SubmitRPCURL)
	}
}

func TestContractExecutorHandlerBroadcastsApprovalAndInterrupts(t *testing.T) {
	broadcaster := &fakeContractBroadcaster{txHash: common.HexToHash("0xdef"), allowance: big.NewInt(0)}
	router := httpapi.NewRouter(httpapi.Handlers{
		ContractExecutor: httpapi.NewContractExecutorHandler(contractapp.NewAppService(broadcaster)),
	})

	body := bytes.NewReader([]byte(`{
		"rpcUrl":"http://127.0.0.1:8545",
		"privateKey":"0xabc123",
		"executor":"0x00000000000000000000000000000000000000aa",
		"execution":{
			"flashLoan":{
				"protocol":"balancer",
				"lender":"0x00000000000000000000000000000000000000bb",
				"token":"0x00000000000000000000000000000000000000cc",
				"amount":"1000"
			},
			"routes":[{
				"routerAddress":"0x00000000000000000000000000000000000000dd",
				"data":"0x1234"
			}],
			"profitToken":"0x00000000000000000000000000000000000000cc",
			"minProfit":"10",
			"approvals":[{
				"token":"0x00000000000000000000000000000000000000cc",
				"spender":"0x00000000000000000000000000000000000000dd",
				"amount":"1000"
			}]
		}
	}`))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/arbitrage/execute", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		TxHash           string   `json:"txHash"`
		ApprovalTxHashes []string `json:"approvalTxHashes"`
		Interrupted      bool     `json:"interrupted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Interrupted {
		t.Fatalf("expected execution to be interrupted")
	}
	if len(resp.ApprovalTxHashes) != 1 || resp.ApprovalTxHashes[0] != common.HexToHash("0xdef").Hex() {
		t.Fatalf("unexpected approval hashes %#v", resp.ApprovalTxHashes)
	}
	if resp.TxHash != "" {
		t.Fatalf("expected no execution tx hash, got %q", resp.TxHash)
	}
	if broadcaster.executeCalls != 0 {
		t.Fatalf("expected no execution calls, got %d", broadcaster.executeCalls)
	}
	if broadcaster.approveCalls != 1 {
		t.Fatalf("expected one approval call, got %d", broadcaster.approveCalls)
	}
}

func TestContractExecutorHandlerRejectsInvalidProtocol(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Handlers{
		ContractExecutor: httpapi.NewContractExecutorHandler(contractapp.NewAppService(&fakeContractBroadcaster{})),
	})

	body := bytes.NewReader([]byte(`{
		"rpcUrl":"http://127.0.0.1:8545",
		"privateKey":"0xabc123",
		"executor":"0x00000000000000000000000000000000000000aa",
		"execution":{
			"flashLoan":{
				"protocol":"bad",
				"lender":"0x00000000000000000000000000000000000000bb",
				"token":"0x00000000000000000000000000000000000000cc",
				"amount":"1000"
			},
			"routes":[{
				"routerAddress":"0x00000000000000000000000000000000000000dd",
				"data":"0x1234"
			}],
			"profitToken":"0x00000000000000000000000000000000000000cc"
		}
	}`))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/arbitrage/execute", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestContractExecutorHandlerRejectsMissingExecution(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Handlers{
		ContractExecutor: httpapi.NewContractExecutorHandler(contractapp.NewAppService(&fakeContractBroadcaster{})),
	})

	body := bytes.NewReader([]byte(`{
		"rpcUrl":"http://127.0.0.1:8545",
		"privateKey":"0xabc123",
		"executor":"0x00000000000000000000000000000000000000aa"
	}`))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/arbitrage/execute", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

type fakeContractBroadcaster struct {
	req          domaincontract.BroadcastRequest
	txHash       common.Hash
	allowance    *big.Int
	executeCalls int
	approveCalls int
}

func (f *fakeContractBroadcaster) BroadcastExecution(
	_ context.Context,
	req domaincontract.BroadcastRequest,
) (domaincontract.BroadcastResponse, error) {
	f.req = req
	f.executeCalls++
	return domaincontract.BroadcastResponse{TxHash: f.txHash}, nil
}

func (f *fakeContractBroadcaster) SimulateExecution(context.Context, domaincontract.BroadcastRequest) error {
	return nil
}

func (f *fakeContractBroadcaster) Allowances(
	_ context.Context,
	_ string,
	_ common.Address,
	approvals []domaincontract.TokenApproval,
) ([]*big.Int, error) {
	allowances := make([]*big.Int, len(approvals))
	for index := range allowances {
		if f.allowance != nil {
			allowances[index] = new(big.Int).Set(f.allowance)
		} else {
			allowances[index] = new(big.Int).Lsh(big.NewInt(1), 255)
		}
	}
	return allowances, nil
}

func (f *fakeContractBroadcaster) BroadcastApprove(
	context.Context,
	domaincontract.BroadcastRequest,
	domaincontract.TokenApproval,
) (domaincontract.BroadcastResponse, error) {
	f.approveCalls++
	return domaincontract.BroadcastResponse{TxHash: f.txHash}, nil
}
