package blockchain

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestSubmitFlashbotsBundlesSimulatesAndTargetsConsecutiveBlocks(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	var blocks []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("X-Flashbots-Signature") == "" {
			t.Error("missing flashbots signature header")
		}
		var rpcReq struct {
			Method string `json:"method"`
			Params []struct {
				BlockNumber string `json:"blockNumber"`
			} `json:"params"`
		}
		if err := json.NewDecoder(req.Body).Decode(&rpcReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		methods = append(methods, rpcReq.Method)
		blocks = append(blocks, rpcReq.Params[0].BlockNumber)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"bundleHash":"0x1"}}`))
	}))
	defer server.Close()

	authKey, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	tx := types.NewTransaction(1, common.HexToAddress("0x1"), big.NewInt(0), 21_000, big.NewInt(1), nil)
	if err := submitFlashbotsBundles(context.Background(), server.URL, authKey, tx, 100, 3); err != nil {
		t.Fatalf("submit bundles: %v", err)
	}

	wantMethods := []string{"eth_callBundle", "eth_sendBundle", "eth_sendBundle", "eth_sendBundle"}
	wantBlocks := []string{"0x64", "0x64", "0x65", "0x66"}
	if len(methods) != len(wantMethods) {
		t.Fatalf("expected %d calls, got %d", len(wantMethods), len(methods))
	}
	for index := range wantMethods {
		if methods[index] != wantMethods[index] || blocks[index] != wantBlocks[index] {
			t.Fatalf("call %d: got %s %s, want %s %s", index, methods[index], blocks[index], wantMethods[index], wantBlocks[index])
		}
	}
}
