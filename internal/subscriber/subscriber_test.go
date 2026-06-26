package subscriber

import (
	"testing"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestNewSubscriber(t *testing.T) {
	addr := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	sub, err := NewSubscriber("http://127.0.0.1:1", "http://127.0.0.1:1", addr, nil, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Logf("NewSubscriber failed (expected with bad URL): %v", err)
		return
	}
	if sub == nil {
		t.Fatal("NewSubscriber returned nil")
	}
	if sub.poolAddr != addr {
		t.Errorf("poolAddr mismatch")
	}
	if sub.wsURL != "http://127.0.0.1:1" {
		t.Errorf("wsURL mismatch")
	}
	if sub.rpcURL != "http://127.0.0.1:1" {
		t.Errorf("rpcURL mismatch")
	}
	if sub.handler != nil {
		t.Error("handler should be nil")
	}
	sub.Stop()
}

func TestPoolStateRPCTypes(t *testing.T) {
	rpc := &PoolStateRPC{
		Tick:         42,
		BlockNumber:  12345,
	}
	if rpc.Tick != 42 {
		t.Errorf("Tick = %d, want 42", rpc.Tick)
	}
	if rpc.BlockNumber != 12345 {
		t.Errorf("BlockNumber = %d, want 12345", rpc.BlockNumber)
	}
}

func TestPoolMetadataTypes(t *testing.T) {
	tk0 := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	tk1 := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	meta := &PoolMetadata{
		Token0: tk0,
		Token1: tk1,
		Fee:    3000,
	}
	if meta.Token0 != tk0 {
		t.Errorf("Token0 mismatch")
	}
	if meta.Token1 != tk1 {
		t.Errorf("Token1 mismatch")
	}
	if meta.Fee != 3000 {
		t.Errorf("Fee = %d, want 3000", meta.Fee)
	}
}

func TestStopIdempotent(t *testing.T) {
	sub, err := NewSubscriber("http://127.0.0.1:1", "http://127.0.0.1:1", common.Address{}, nil, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Skip("cannot create subscriber:", err)
	}
	sub.Stop()
	sub.Stop() // double stop should not panic
}

func TestMaskAPIKey(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://eth-mainnet.g.alchemy.com/v2/7NCmH4mP28eUd1BkVLMA8", "https://eth-mainnet.g.alchemy.com/v2/***"},
		{"wss://mainnet.infura.io/ws/v3/abc123def45678901234567890abcde", "wss://mainnet.infura.io/ws/v3/***"},
		{"http://localhost:8545", "http://localhost:8545"},
		{"ws://localhost:8546", "ws://localhost:8546"},
		{"https://rpc.example.com/custom", "https://rpc.example.com/custom"},
	}
	for _, tt := range tests {
		got := maskAPIKey(tt.in)
		if got != tt.want {
			t.Errorf("maskAPIKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsDuplicateLogUsesLogIndex(t *testing.T) {
	sub, err := NewSubscriber("http://127.0.0.1:1", "http://127.0.0.1:1", common.Address{}, nil, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}

	tx := common.HexToHash("0x01")
	block := common.HexToHash("0x02")
	log0 := types.Log{TxHash: tx, BlockHash: block, Index: 0}
	log1 := types.Log{TxHash: tx, BlockHash: block, Index: 1}

	if sub.isDuplicateLog(log0) {
		t.Fatal("first log should not be duplicate")
	}
	if sub.isDuplicateLog(log0) == false {
		t.Fatal("same log should be duplicate")
	}
	if sub.isDuplicateLog(log1) {
		t.Fatal("different logIndex in same tx should not be duplicate")
	}
}

func TestMarkConnectedFirstThenReconnect(t *testing.T) {
	sub, err := NewSubscriber("http://127.0.0.1:1", "http://127.0.0.1:1", common.Address{}, nil, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}

	if sub.markConnected() {
		t.Fatal("first connect should not be treated as reconnect")
	}
	if !sub.markConnected() {
		t.Fatal("second connect should be treated as reconnect")
	}
}
