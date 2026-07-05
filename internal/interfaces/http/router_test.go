package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	httpapi "github.com/brianliu-sysu/uniswapv3/internal/interfaces/http"
)

func TestRouterHealthEndpoints(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Handlers{
		Health: httpapi.NewHealthHandler(),
	})

	for _, path := range []string{"/health", "/api/v1/health"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", path, rec.Code)
		}

		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s: decode response: %v", path, err)
		}
		if resp["status"] != "ok" {
			t.Fatalf("%s: expected status ok, got %#v", path, resp)
		}
	}
}

func TestRouterQuoteCORSPreflight(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Handlers{
		QuoteV3: httpapi.NewQuoteV3Handler(nil),
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/univ3/quote", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("expected CORS allow-origin header")
	}
}

func TestRouterProtocolQuotePaths(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Handlers{
		QuoteCombined: httpapi.NewQuoteCombinedHandler(nil),
		QuoteV3:       httpapi.NewQuoteV3Handler(nil),
		QuoteV4:       httpapi.NewQuoteV4Handler(nil),
	})

	body := bytes.NewReader([]byte(`{"tokenIn":"0x0000000000000000000000000000000000000002","tokenOut":"0x0000000000000000000000000000000000000003","amountIn":"1"}`))
	for _, path := range []string{"/api/v1/quote", "/api/v1/univ3/quote", "/api/v1/univ4/quote"} {
		req := httptest.NewRequest(http.MethodPost, path, body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code == http.StatusNotFound {
			t.Fatalf("%s: route not registered", path)
		}
	}
}

func TestRouterCombinedQuoteCORSPreflight(t *testing.T) {
	router := httpapi.NewRouter(httpapi.Handlers{
		QuoteCombined: httpapi.NewQuoteCombinedHandler(nil),
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/quote", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}
