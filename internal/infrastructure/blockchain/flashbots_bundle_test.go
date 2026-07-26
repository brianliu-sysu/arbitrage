package blockchain

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestSubmitFlashbotsBundleTargetsOneBlockWithoutSimulation(t *testing.T) {
	var method string
	var block string
	var builders []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("X-Flashbots-Signature") == "" {
			t.Error("missing flashbots signature header")
		}
		var rpcReq struct {
			Method string `json:"method"`
			Params []struct {
				BlockNumber string   `json:"blockNumber"`
				Builders    []string `json:"builders"`
			} `json:"params"`
		}
		if err := json.NewDecoder(req.Body).Decode(&rpcReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		method = rpcReq.Method
		block = rpcReq.Params[0].BlockNumber
		builders = rpcReq.Params[0].Builders
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"bundleHash":"0x1"}}`))
	}))
	defer server.Close()

	authKey, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	tx := types.NewTransaction(1, common.HexToAddress("0x1"), big.NewInt(0), 21_000, big.NewInt(1), nil)
	wantBuilders := []string{"flashbots", "builder0x69"}
	if err := submitFlashbotsBundle(context.Background(), server.URL, authKey, tx, 100, wantBuilders); err != nil {
		t.Fatalf("submit bundle: %v", err)
	}

	if method != "eth_sendBundle" {
		t.Fatalf("expected eth_sendBundle, got %s", method)
	}
	if block != "0x64" {
		t.Fatalf("expected target block 0x64, got %s", block)
	}
	if !reflect.DeepEqual(builders, wantBuilders) {
		t.Fatalf("expected builders %v, got %v", wantBuilders, builders)
	}
}
