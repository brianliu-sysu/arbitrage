package api

import (
	"encoding/json"
	"fmt"
	"github.com/brianliu-sysu/arbitrage/internal/arbitrage"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/quote"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// ---- Mock QuoteProvider ----

type mockQuoteProvider struct {
	pools       []map[string]interface{}
	prices      map[common.Address][3]interface{}
	quoteResult *big.Int
	quoteErr    error
	crossResult *quote.Result
	crossErr    error
	triangles   []arbitrage.TriangleOpportunity
	triangleErr error
}

func (m *mockQuoteProvider) GetAllPoolInfo() []map[string]interface{} {
	return m.pools
}

func (m *mockQuoteProvider) GetPrice(chain string, poolAddr common.Address) (float64, float64, int32, bool) {
	v, ok := m.prices[poolAddr]
	if !ok {
		return 0, 0, 0, false
	}
	return v[0].(float64), v[1].(float64), v[2].(int32), true
}

func (m *mockQuoteProvider) QuoteExactInput(chain string, poolAddr common.Address, amountIn *big.Int, tokenIn common.Address) (*big.Int, error) {
	if m.quoteErr != nil {
		return nil, m.quoteErr
	}
	return m.quoteResult, nil
}

func (m *mockQuoteProvider) CrossQuote(chain string, amountIn *big.Int, tokenIn, tokenOut common.Address) (*quote.Result, error) {
	if m.crossErr != nil {
		return nil, m.crossErr
	}
	return m.crossResult, nil
}

func (m *mockQuoteProvider) TriangleOpportunities(chain string) ([]arbitrage.TriangleOpportunity, error) {
	if m.triangleErr != nil {
		return nil, m.triangleErr
	}
	return m.triangles, nil
}

// ---- Helpers ----

func newTestServer(mock *mockQuoteProvider) *httptest.Server {
	svc := &Server{svc: mock}
	router := svc.SetupRouter()
	return httptest.NewServer(router)
}

func readJSON(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func mockPools() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"chain":        "ethereum",
			"address":      "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8",
			"token0":       "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
			"token1":       "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2",
			"fee":          float64(3000),
			"tick":         float64(200000),
			"price0In1":    float64(2000.5),
			"price1In0":    0.000499875,
			"liquidity":    "1000000000000000000",
			"sqrtPriceX96": "123456789012345678901234567890",
			"blockNumber":  float64(18000000),
		},
	}
}

// ---- Tests ----

func TestHealthEndpoint(t *testing.T) {
	mock := &mockQuoteProvider{}
	ts := newTestServer(mock)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]interface{}
	readJSON(t, resp, &body)
	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
}

func TestListPools(t *testing.T) {
	mock := &mockQuoteProvider{pools: mockPools()}
	ts := newTestServer(mock)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/ethereum/pools")
	if err != nil {
		t.Fatalf("GET /api/v1/pools: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string][]poolInfo
	readJSON(t, resp, &body)
	if len(body["pools"]) != 1 {
		t.Errorf("expected 1 pool, got %d", len(body["pools"]))
	}
	p := body["pools"][0]
	if p.Address != "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8" {
		t.Errorf("pool address mismatch: %s", p.Address)
	}
	if p.Fee != 3000 {
		t.Errorf("fee = %d, want 3000", p.Fee)
	}
}

func TestGetPoolFound(t *testing.T) {
	mock := &mockQuoteProvider{pools: mockPools()}
	ts := newTestServer(mock)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/ethereum/pools/0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	if err != nil {
		t.Fatalf("GET pool: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var info poolInfo
	readJSON(t, resp, &info)
	if info.Address != "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8" {
		t.Errorf("address mismatch")
	}
}

func TestGetPoolNotFound(t *testing.T) {
	mock := &mockQuoteProvider{pools: mockPools()}
	ts := newTestServer(mock)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/ethereum/pools/0x0000000000000000000000000000000000000001")
	if err != nil {
		t.Fatalf("GET pool: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetPriceFound(t *testing.T) {
	addr := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	mock := &mockQuoteProvider{
		prices: map[common.Address][3]interface{}{
			addr: {2000.5, 0.000499875, int32(200000)},
		},
	}
	ts := newTestServer(mock)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/ethereum/pools/0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8/price")
	if err != nil {
		t.Fatalf("GET price: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var pr priceResponse
	readJSON(t, resp, &pr)
	if pr.Price0In1 != 2000.5 {
		t.Errorf("Price0In1 = %f, want 2000.5", pr.Price0In1)
	}
	if pr.Tick != 200000 {
		t.Errorf("Tick = %d, want 200000", pr.Tick)
	}
}

func TestGetPriceNotFound(t *testing.T) {
	mock := &mockQuoteProvider{}
	ts := newTestServer(mock)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/ethereum/pools/0x0000000000000000000000000000000000000001/price")
	if err != nil {
		t.Fatalf("GET price: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestQuoteExactInput(t *testing.T) {
	mock := &mockQuoteProvider{
		pools:       mockPools(),
		quoteResult: big.NewInt(500000000000000000),
	}
	ts := newTestServer(mock)
	defer ts.Close()

	body := `{"amountIn":"1000000","tokenIn":"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"}`
	resp, err := http.Post(
		ts.URL+"/api/v1/ethereum/pools/0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8/quote",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST quote: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var qr quoteResponse
	readJSON(t, resp, &qr)
	if qr.AmountIn != "1000000" {
		t.Errorf("AmountIn mismatch")
	}
	if qr.AmountOut != "500000000000000000" {
		t.Errorf("AmountOut = %s", qr.AmountOut)
	}
	if qr.TokenOut != "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2" {
		t.Errorf("TokenOut mismatch: %s", qr.TokenOut)
	}
}

func TestQuoteExactInputPoolNotFound(t *testing.T) {
	mock := &mockQuoteProvider{pools: mockPools()}
	ts := newTestServer(mock)
	defer ts.Close()

	body := `{"amountIn":"1000000","tokenIn":"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"}`
	resp, err := http.Post(
		ts.URL+"/api/v1/ethereum/pools/0x0000000000000000000000000000000000000001/quote",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST quote: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestQuoteExactInputBadRequest(t *testing.T) {
	mock := &mockQuoteProvider{pools: mockPools()}
	ts := newTestServer(mock)
	defer ts.Close()

	body := `not json`
	resp, err := http.Post(
		ts.URL+"/api/v1/ethereum/pools/0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8/quote",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST quote: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestQuoteExactInputInvalidAmount(t *testing.T) {
	mock := &mockQuoteProvider{pools: mockPools()}
	ts := newTestServer(mock)
	defer ts.Close()

	body := `{"amountIn":"not_a_number","tokenIn":"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"}`
	resp, err := http.Post(
		ts.URL+"/api/v1/ethereum/pools/0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8/quote",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST quote: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCrossQuote(t *testing.T) {
	tk0 := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	tk1 := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	poolAddr := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")

	mock := &mockQuoteProvider{
		crossResult: &quote.Result{
			Hops: []quote.Hop{
				{Pool: poolAddr, TokenIn: tk0, TokenOut: tk1},
			},
			AmountIn:  big.NewInt(1000000),
			AmountOut: big.NewInt(500000000000000000),
			TokenIn:   tk0,
			TokenOut:  tk1,
		},
	}
	ts := newTestServer(mock)
	defer ts.Close()

	body := `{"amountIn":"1000000","tokenIn":"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48","tokenOut":"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"}`
	resp, err := http.Post(
		ts.URL+"/api/v1/ethereum/quote",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST cross quote: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var cqr crossQuoteResponse
	readJSON(t, resp, &cqr)
	if cqr.AmountOut != "500000000000000000" {
		t.Errorf("AmountOut = %s", cqr.AmountOut)
	}
	if cqr.Hops != 1 {
		t.Errorf("Hops = %d, want 1", cqr.Hops)
	}
	if len(cqr.Path) != 1 {
		t.Errorf("Path len = %d, want 1", len(cqr.Path))
	}
}

func TestCrossQuoteNotFound(t *testing.T) {
	mock := &mockQuoteProvider{
		crossErr: fmt.Errorf("no path found"),
	}
	ts := newTestServer(mock)
	defer ts.Close()

	body := `{"amountIn":"1000000","tokenIn":"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48","tokenOut":"0x0000000000000000000000000000000000000001"}`
	resp, err := http.Post(
		ts.URL+"/api/v1/ethereum/quote",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST cross quote: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestCrossQuoteBadRequest(t *testing.T) {
	mock := &mockQuoteProvider{}
	ts := newTestServer(mock)
	defer ts.Close()

	body := `not json`
	resp, err := http.Post(
		ts.URL+"/api/v1/ethereum/quote",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST cross quote: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCrossQuoteInvalidAmount(t *testing.T) {
	mock := &mockQuoteProvider{}
	ts := newTestServer(mock)
	defer ts.Close()

	body := `{"amountIn":"not_a_number","tokenIn":"0xAA","tokenOut":"0xBB"}`
	resp, err := http.Post(
		ts.URL+"/api/v1/ethereum/quote",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST cross quote: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestTriangleOpportunities(t *testing.T) {
	base := common.HexToAddress("0x1000000000000000000000000000000000000001")
	tkB := common.HexToAddress("0x2000000000000000000000000000000000000002")
	tkC := common.HexToAddress("0x3000000000000000000000000000000000000003")
	poolAB := common.HexToAddress("0xa000000000000000000000000000000000000001")
	poolBC := common.HexToAddress("0xb000000000000000000000000000000000000002")
	poolCA := common.HexToAddress("0xc000000000000000000000000000000000000003")
	mock := &mockQuoteProvider{
		triangles: []arbitrage.TriangleOpportunity{{
			Path: arbitrage.TrianglePath{
				BaseToken: base,
				Hops: [3]arbitrage.TriangleHop{
					{Pool: poolAB, TokenIn: base, TokenOut: tkB},
					{Pool: poolBC, TokenIn: tkB, TokenOut: tkC},
					{Pool: poolCA, TokenIn: tkC, TokenOut: base},
				},
			},
			AmountIn:    big.NewInt(1000),
			AmountOut:   big.NewInt(1100),
			Profit:      big.NewInt(100),
			ProfitBps:   1000,
			BlockNumber: 123,
			DetectedAt:  time.Unix(10, 0).UTC(),
		}},
	}
	ts := newTestServer(mock)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/ethereum/arbitrage/triangles")
	if err != nil {
		t.Fatalf("GET triangles: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string][]triangleOpportunityResponse
	readJSON(t, resp, &body)
	if len(body["opportunities"]) != 1 {
		t.Fatalf("opportunities=%d want 1", len(body["opportunities"]))
	}
	got := body["opportunities"][0]
	if got.Profit != "100" || got.ProfitBps != 1000 || len(got.Path) != 3 {
		t.Fatalf("unexpected opportunity: %+v", got)
	}
}

func TestQuoteExactInputMissingFields(t *testing.T) {
	mock := &mockQuoteProvider{pools: mockPools()}
	ts := newTestServer(mock)
	defer ts.Close()

	body := `{"amountIn":""}`
	resp, err := http.Post(
		ts.URL+"/api/v1/ethereum/pools/0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8/quote",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST quote: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestQuoteError(t *testing.T) {
	mock := &mockQuoteProvider{
		pools:    mockPools(),
		quoteErr: fmt.Errorf("RPC error"),
	}
	ts := newTestServer(mock)
	defer ts.Close()

	body := `{"amountIn":"1000000","tokenIn":"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"}`
	resp, err := http.Post(
		ts.URL+"/api/v1/ethereum/pools/0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8/quote",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST quote: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 500 {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestQuoteReverseToken(t *testing.T) {
	mock := &mockQuoteProvider{
		pools:       mockPools(),
		quoteResult: big.NewInt(2000000),
	}
	ts := newTestServer(mock)
	defer ts.Close()

	// tokenIn = WETH (token1), so tokenOut = USDC (token0)
	body := `{"amountIn":"500000000000000000","tokenIn":"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"}`
	resp, err := http.Post(
		ts.URL+"/api/v1/ethereum/pools/0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8/quote",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST quote: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var qr quoteResponse
	readJSON(t, resp, &qr)
	if qr.TokenOut != "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48" {
		t.Errorf("TokenOut = %s, want USDC", qr.TokenOut)
	}
}

func TestNewServerDefault(t *testing.T) {
	mock := &mockQuoteProvider{}
	srv := NewServer(":0", mock, nil, logx.Nop(), 0, "")
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	// Should have a default *http.Server
	if srv.srv == nil {
		t.Error("srv should not be nil")
	}
	if _, ok := srv.srv.(*http.Server); !ok {
		t.Errorf("srv should be *http.Server, got %T", srv.srv)
	}
}

type mockHTTPServer struct {
	listenErr error
	closed    bool
}

func (m *mockHTTPServer) ListenAndServe() error { return m.listenErr }
func (m *mockHTTPServer) Close() error          { m.closed = true; return nil }

func TestNewServerCustomSrv(t *testing.T) {
	mock := &mockQuoteProvider{}
	custom := &mockHTTPServer{}
	srv := NewServer(":0", mock, custom, logx.Nop(), 0, "")
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	if srv.srv != custom {
		t.Error("srv should be the custom implementation")
	}
}

func TestShutdown(t *testing.T) {
	mock := &mockQuoteProvider{}
	srv := NewServer(":0", mock, nil, logx.Nop(), 0, "")
	if err := srv.Shutdown(); err != nil {
		t.Errorf("Shutdown error: %v", err)
	}
}

func TestInt64ValAllTypes(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want int64
	}{
		{"int", int(42), 42},
		{"int32", int32(100), 100},
		{"int64", int64(200), 200},
		{"uint64", uint64(300), 300},
		{"uint32", uint32(400), 400},
		{"float64", float64(500), 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := map[string]interface{}{"v": tt.val}
			got := int64Val(m, "v")
			if got != tt.want {
				t.Errorf("int64Val = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestUint32ValAllTypes(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want uint32
	}{
		{"int", int(42), 42},
		{"int32", int32(100), 100},
		{"int64", int64(200), 200},
		{"uint32", uint32(300), 300},
		{"float64", float64(400), 400},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := map[string]interface{}{"v": tt.val}
			got := uint32Val(m, "v")
			if got != tt.want {
				t.Errorf("uint32Val = %d, want %d", got, tt.want)
			}
		})
	}
}
