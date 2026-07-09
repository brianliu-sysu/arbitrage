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
		"deadline":"999",
		"gasLimit":500000,
		"gasPriceWei":"2000000000",
		"skipEstimate":true
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
}

func TestContractExecutorHandlerRejectsInvalidProtocol(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Handlers{
		ContractExecutor: httpapi.NewContractExecutorHandler(contractapp.NewAppService(&fakeContractBroadcaster{})),
	})

	body := bytes.NewReader([]byte(`{
		"rpcUrl":"http://127.0.0.1:8545",
		"privateKey":"0xabc123",
		"executor":"0x00000000000000000000000000000000000000aa",
		"flashLoan":{
			"protocol":"bad",
			"lender":"0x00000000000000000000000000000000000000bb",
			"token":"0x00000000000000000000000000000000000000cc",
			"amount":"1000"
		},
		"profitToken":"0x00000000000000000000000000000000000000cc"
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
	req    domaincontract.BroadcastRequest
	txHash common.Hash
}

func (f *fakeContractBroadcaster) BroadcastExecution(
	_ context.Context,
	req domaincontract.BroadcastRequest,
) (domaincontract.BroadcastResponse, error) {
	f.req = req
	return domaincontract.BroadcastResponse{TxHash: f.txHash}, nil
}
