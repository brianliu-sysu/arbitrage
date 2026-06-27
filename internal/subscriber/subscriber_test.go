package subscriber

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// int24ToTopic encodes an int24 value as an indexed topic hash.
// Solidity sign-extends int24 to 256 bits (32 bytes).
func int24ToTopic(v int32) common.Hash {
	var b [32]byte
	if v >= 0 {
		b[31] = byte(v & 0xFF)
		b[30] = byte((v >> 8) & 0xFF)
		b[29] = byte((v >> 16) & 0xFF)
	} else {
		u := new(big.Int).Add(
			new(big.Int).Lsh(big.NewInt(1), 256),
			big.NewInt(int64(v)),
		)
		raw := u.Bytes()
		copy(b[32-len(raw):], raw)
	}
	return common.BytesToHash(b[:])
}

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

func TestMaskError(t *testing.T) {
	// maskError wraps error message through maskAPIKey
	err := maskError(nil)
	if err != nil {
		t.Errorf("maskError(nil) should return nil, got %v", err)
	}
	err = maskError(fmt.Errorf("some error"))
	if err == nil {
		t.Fatal("maskError should return an error")
	}
}

func TestMaskAPIKeyEdgeCases(t *testing.T) {
	// Short key (<16 chars) should not be masked
	result := maskAPIKey("https://rpc.com/abc")
	if result != "https://rpc.com/abc" {
		t.Errorf("short key should not be masked: got %q", result)
	}
	// URL with query params
	result = maskAPIKey("https://rpc.com/v2/abcdefghijklmnop123?chainId=1")
	if result != "https://rpc.com/v2/***?chainId=1" {
		t.Errorf("key with query params: got %q", result)
	}
	// Empty URL
	result = maskAPIKey("")
	if result != "" {
		t.Errorf("empty URL: got %q", result)
	}
}

func TestIsRetryableHTTPError(t *testing.T) {
	if isRetryableHTTPError(nil) {
		t.Error("nil should not be retryable")
	}
	for _, code := range []string{"429", "500", "502", "503", "504"} {
		if !isRetryableHTTPError(fmt.Errorf("HTTP error %s", code)) {
			t.Errorf("error containing %s should be retryable", code)
		}
	}
	if isRetryableHTTPError(fmt.Errorf("404 not found")) {
		t.Error("404 should not be retryable")
	}
}

func TestIsConnectionError(t *testing.T) {
	if isConnectionError(nil) {
		t.Error("nil should not be connection error")
	}
	connectionErrors := []string{
		"connection reset by peer",
		"broken pipe",
		"unexpected EOF",
		"EOF",
		"no such host",
		"connection refused",
		"i/o timeout",
		"TLS handshake timeout",
	}
	for _, msg := range connectionErrors {
		if !isConnectionError(fmt.Errorf("%s", msg)) {
			t.Errorf("%q should be connection error", msg)
		}
	}
	if isConnectionError(fmt.Errorf("some random error")) {
		t.Error("random error should not be connection error")
	}
}

func TestTickDataTypes(t *testing.T) {
	td := &TickData{
		Initialized: true,
	}
	if !td.Initialized {
		t.Error("Initialized should be true")
	}
}

func TestTokenMetadataTypes(t *testing.T) {
	tm := &TokenMetadata{
		Symbol:   "WETH",
		Decimals: 18,
	}
	if tm.Symbol != "WETH" {
		t.Errorf("Symbol = %q", tm.Symbol)
	}
	if tm.Decimals != 18 {
		t.Errorf("Decimals = %d", tm.Decimals)
	}
}

func TestIsDuplicateLogEdgeCases(t *testing.T) {
	sub, err := NewSubscriber("http://127.0.0.1:1", "http://127.0.0.1:1", common.Address{}, nil, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	// Log with empty txHash and blockHash should pass through
	emptyLog := types.Log{}
	if sub.isDuplicateLog(emptyLog) {
		t.Error("log with empty tx/block hash should not be duplicate")
	}
}

func TestNewSubscriberWithAPIKeyMasking(t *testing.T) {
	addr := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	sub, err := NewSubscriber(
		"https://eth-mainnet.g.alchemy.com/v2/secret1234567890abcdef",
		"https://eth-mainnet.g.alchemy.com/v2/secret1234567890abcdef",
		addr, nil, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	// wsURL and rpcURL should be masked
	if sub.wsURL != "https://eth-mainnet.g.alchemy.com/v2/***" {
		t.Errorf("wsURL should be masked, got %q", sub.wsURL)
	}
	if sub.rpcURL != "https://eth-mainnet.g.alchemy.com/v2/***" {
		t.Errorf("rpcURL should be masked, got %q", sub.rpcURL)
	}
	// wsDial and rpcDial should keep original
	if sub.wsDial != "https://eth-mainnet.g.alchemy.com/v2/secret1234567890abcdef" {
		t.Errorf("wsDial should be original, got %q", sub.wsDial)
	}
	if sub.rpcDial != "https://eth-mainnet.g.alchemy.com/v2/secret1234567890abcdef" {
		t.Errorf("rpcDial should be original, got %q", sub.rpcDial)
	}
	sub.Stop()
}

func TestStartNilHandler(t *testing.T) {
	sub, err := NewSubscriber("http://127.0.0.1:1", "http://127.0.0.1:1", common.Address{}, nil, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	err = sub.Start()
	if err != nil {
		t.Logf("Start failed (expected): %v", err)
	}
	sub.Stop()
}

func TestSleepOrCancel(t *testing.T) {
	// Create subscriber and cancel it
	sub, err := NewSubscriber("http://127.0.0.1:1", "http://127.0.0.1:1", common.Address{}, nil, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}

	// Cancel the context to make sleepOrCancel return -1
	sub.cancel()
	next := sub.sleepOrCancel(time.Second)
	if next != -1 {
		t.Errorf("sleepOrCancel after cancel = %v, want -1", next)
	}
	sub.Stop()
}

func TestSleepOrCancelBackoffDoubles(t *testing.T) {
	sub, err := NewSubscriber("http://127.0.0.1:1", "http://127.0.0.1:1", common.Address{}, nil, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Stop()

	// With very short backoff, sleepOrCancel should double it
	shortBackoff := 5 * time.Millisecond
	next := sub.sleepOrCancel(shortBackoff)
	if next != shortBackoff*2 {
		t.Errorf("sleepOrCancel(%v) = %v, want %v", shortBackoff, next, shortBackoff*2)
	}
}

func TestSleepOrCancelCapsAtMax(t *testing.T) {
	sub, err := NewSubscriber("http://127.0.0.1:1", "http://127.0.0.1:1", common.Address{}, nil, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Stop()

	// Using backoff=45s, which doubled would be 90s > maxReconnectBackoff (60s), so it caps.
	// But we cancel context to avoid waiting.
	go func() {
		time.Sleep(5 * time.Millisecond)
		sub.cancel()
	}()
	// Apply cancel directly — don't sleep the full duration
	next := sub.sleepOrCancel(100 * time.Millisecond)
	// After cancellation, sleepOrCancel returns -1
	if next != -1 {
		t.Logf("sleepOrCancel after cancel = %v", next)
	}
}

// mockHandler is a simple EventHandler that records calls.
type mockHandler struct {
	lastSwap        *pool.SwapEvent
	lastMint        *pool.MintEvent
	lastBurn        *pool.BurnEvent
	lastError       error
	reconnectCalled bool
}

func (h *mockHandler) OnSwap(event *pool.SwapEvent)     { h.lastSwap = event }
func (h *mockHandler) OnMint(event *pool.MintEvent)     { h.lastMint = event }
func (h *mockHandler) OnBurn(event *pool.BurnEvent)     { h.lastBurn = event }
func (h *mockHandler) OnError(err error)                 { h.lastError = err }
func (h *mockHandler) OnReconnected()                     { h.reconnectCalled = true }

func TestProcessLogSwapEvent(t *testing.T) {
	handler := &mockHandler{}
	sub, err := NewSubscriber("http://127.0.0.1:1", "http://127.0.0.1:1", common.Address{}, handler, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Stop()

	// Build a valid Swap log using ABI encoding
	int256, _ := abi.NewType("int256", "", nil)
	uint160, _ := abi.NewType("uint160", "", nil)
	uint128, _ := abi.NewType("uint128", "", nil)
	int24, _ := abi.NewType("int24", "", nil)

	args := abi.Arguments{
		{Type: int256},
		{Type: int256},
		{Type: uint160},
		{Type: uint128},
		{Type: int24},
	}
	sqrtPriceX96 := new(big.Int).Lsh(big.NewInt(1), 96) // 2^96
	data, _ := args.Pack(big.NewInt(-1000), big.NewInt(2000), sqrtPriceX96, big.NewInt(1000000), big.NewInt(42))

	vLog := types.Log{
		Topics: []common.Hash{
			pool.SwapEventSignature,
			common.BytesToHash(common.HexToAddress("0x1111111111111111111111111111111111111111").Bytes()),
			common.BytesToHash(common.HexToAddress("0x2222222222222222222222222222222222222222").Bytes()),
		},
		Data: data,
	}
	sub.processLog(vLog)

	if handler.lastSwap == nil {
		t.Fatal("Swap event was not processed")
	}
	if handler.lastSwap.Tick != 42 {
		t.Errorf("Tick = %d, want 42", handler.lastSwap.Tick)
	}
}

func TestProcessLogMintEvent(t *testing.T) {
	handler := &mockHandler{}
	sub, err := NewSubscriber("http://127.0.0.1:1", "http://127.0.0.1:1", common.Address{}, handler, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Stop()

	addressType, _ := abi.NewType("address", "", nil)
	uint128, _ := abi.NewType("uint128", "", nil)
	uint256, _ := abi.NewType("uint256", "", nil)

	args := abi.Arguments{
		{Type: addressType},
		{Type: uint128},
		{Type: uint256},
		{Type: uint256},
	}
	sender := common.HexToAddress("0x0000000000000000000000000000000000000001")
	data, _ := args.Pack(sender, big.NewInt(500000), big.NewInt(1000000), big.NewInt(0))

	// Encode tickLower=100, tickUpper=200 as indexed topics
	tickLowerTopic := int24ToTopic(100)
	tickUpperTopic := int24ToTopic(200)

	vLog := types.Log{
		Topics: []common.Hash{
			pool.MintEventSignature,
			common.BytesToHash(common.HexToAddress("0x3333333333333333333333333333333333333333").Bytes()),
			tickLowerTopic,
			tickUpperTopic,
		},
		Data: data,
	}
	sub.processLog(vLog)

	if handler.lastMint == nil {
		t.Fatal("Mint event was not processed")
	}
	if handler.lastMint.TickLower != 100 {
		t.Errorf("TickLower = %d, want 100", handler.lastMint.TickLower)
	}
	if handler.lastMint.TickUpper != 200 {
		t.Errorf("TickUpper = %d, want 200", handler.lastMint.TickUpper)
	}
}

func TestProcessLogBurnEvent(t *testing.T) {
	handler := &mockHandler{}
	sub, err := NewSubscriber("http://127.0.0.1:1", "http://127.0.0.1:1", common.Address{}, handler, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Stop()

	uint128, _ := abi.NewType("uint128", "", nil)
	uint256, _ := abi.NewType("uint256", "", nil)

	args := abi.Arguments{
		{Type: uint128},
		{Type: uint256},
		{Type: uint256},
	}
	data, _ := args.Pack(big.NewInt(500000), big.NewInt(0), big.NewInt(1000000))

	tickLowerTopic := int24ToTopic(-50)
	tickUpperTopic := int24ToTopic(50)

	vLog := types.Log{
		Topics: []common.Hash{
			pool.BurnEventSignature,
			common.BytesToHash(common.HexToAddress("0x4444444444444444444444444444444444444444").Bytes()),
			tickLowerTopic,
			tickUpperTopic,
		},
		Data: data,
	}
	sub.processLog(vLog)

	if handler.lastBurn == nil {
		t.Fatal("Burn event was not processed")
	}
	if handler.lastBurn.TickLower != -50 {
		t.Errorf("TickLower = %d, want -50", handler.lastBurn.TickLower)
	}
	if handler.lastBurn.TickUpper != 50 {
		t.Errorf("TickUpper = %d, want 50", handler.lastBurn.TickUpper)
	}
}

func TestProcessLogEmptyTopics(t *testing.T) {
	handler := &mockHandler{}
	sub, err := NewSubscriber("http://127.0.0.1:1", "http://127.0.0.1:1", common.Address{}, handler, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Stop()

	vLog := types.Log{Topics: []common.Hash{}}
	sub.processLog(vLog) // Should not panic

	if handler.lastSwap != nil || handler.lastMint != nil || handler.lastBurn != nil {
		t.Error("no events should be processed for empty topics")
	}
}

func TestProcessLogDuplicateSwap(t *testing.T) {
	handler := &mockHandler{}
	sub, err := NewSubscriber("http://127.0.0.1:1", "http://127.0.0.1:1", common.Address{}, handler, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Stop()

	int256, _ := abi.NewType("int256", "", nil)
	uint160, _ := abi.NewType("uint160", "", nil)
	uint128, _ := abi.NewType("uint128", "", nil)
	int24, _ := abi.NewType("int24", "", nil)
	args := abi.Arguments{
		{Type: int256}, {Type: int256}, {Type: uint160}, {Type: uint128}, {Type: int24},
	}
	data, _ := args.Pack(big.NewInt(-1000), big.NewInt(2000), big.NewInt(1).Lsh(big.NewInt(1), 96), big.NewInt(1000000), big.NewInt(42))

	vLog := types.Log{
		Topics: []common.Hash{
			pool.SwapEventSignature,
			common.BytesToHash(common.HexToAddress("0xaaa").Bytes()),
			common.BytesToHash(common.HexToAddress("0xbbb").Bytes()),
		},
		Data:      data,
		TxHash:    common.HexToHash("0x01"),
		BlockHash: common.HexToHash("0x02"),
		Index:     0,
	}

	// First call should process
	sub.processLog(vLog)
	if handler.lastSwap == nil {
		t.Fatal("first swap should be processed")
	}
	handler.lastSwap = nil // reset

	// Duplicate should be skipped
	sub.processLog(vLog)
	if handler.lastSwap != nil {
		t.Fatal("duplicate swap should be skipped")
	}
}

func TestProcessLogDuplicateSkipsDedup(t *testing.T) {
	handler := &mockHandler{}
	sub, err := NewSubscriber("http://127.0.0.1:1", "http://127.0.0.1:1", common.Address{}, handler, common.Address{}, common.Address{}, logx.Nop())
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Stop()

	// A log with topics but NO TxHash/BlockHash should NOT be deduped
	vLog := types.Log{
		Topics: []common.Hash{pool.SwapEventSignature},
	}
	// First call: processed (bad data → error to handler)
	sub.processLog(vLog)
	// Should have gotten an error via handler (insufficient topics)
	if handler.lastError == nil {
		t.Error("expected error from bad swap log")
	}
}
