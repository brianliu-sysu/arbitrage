package service

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/brianliu-sysu/arbitrage/internal/store"
	"github.com/brianliu-sysu/arbitrage/internal/blockchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type testAppliedLogCache struct {
	seen map[string]struct{}
}

type recordingStore struct {
	mu          sync.Mutex
	savedBlocks []uint64
}

func (s *recordingStore) Save(_ context.Context, snap *store.PoolSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.savedBlocks = append(s.savedBlocks, snap.BlockNumber)
	return nil
}

func (s *recordingStore) SaveHistory(_ context.Context, _ *store.PoolSnapshot) error { return nil }
func (s *recordingStore) Load(_ context.Context, _, _ string) (*store.PoolSnapshot, error) {
	return nil, nil
}
func (s *recordingStore) LoadAll(_ context.Context, _ string) (map[string]*store.PoolSnapshot, error) {
	return nil, nil
}
func (s *recordingStore) LoadTokenMetadata(_ context.Context, _, _ string) (*store.TokenMetadata, error) {
	return nil, nil
}
func (s *recordingStore) SaveTokenMetadata(_ context.Context, _ *store.TokenMetadata) error {
	return nil
}
func (s *recordingStore) Close() {}

func (s *recordingStore) blocks() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]uint64, len(s.savedBlocks))
	copy(cp, s.savedBlocks)
	return cp
}

func newTestAppliedLogCache() *testAppliedLogCache {
	return &testAppliedLogCache{seen: make(map[string]struct{})}
}

func (c *testAppliedLogCache) MarkAppliedIfNew(_ context.Context, chainName, poolAddress string, blockNumber uint64, txHash string, logIndex uint) (bool, error) {
	key := fmt.Sprintf("%s:%s:%d:%s:%d", chainName, poolAddress, blockNumber, txHash, logIndex)
	if _, ok := c.seen[key]; ok {
		return false, nil
	}
	c.seen[key] = struct{}{}
	return true, nil
}

func (c *testAppliedLogCache) Close() error { return nil }

func TestDrainAndReplayDedupAppliedLogs(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)

	svc := &PoolQuoteService{
		pool:               ps,
		logger:             logx.Nop(),
		chainName:          "ethereum",
		logCache:           newTestAppliedLogCache(),
		snapshotStartBlock: 100,
	}
	svc.bufferingMode.Store(true)

	tx := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	mint := &pool.MintEvent{
		Owner:     common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Amount:    big.NewInt(100000),
		TickLower: -10,
		TickUpper: 10,
		Raw: types.Log{
			BlockNumber: 101,
			TxHash:      tx,
			Index:       1,
		},
	}

	// 同一条日志重复进入回放时，第二次应被去重过滤。
	svc.eventBuffer = []bufferedEvent{
		{mint: mint},
		{mint: mint},
	}
	svc.drainAndReplay()

	if svc.bufferingMode.Load() {
		t.Fatal("buffering mode should be disabled after draining")
	}

	_, _, liq, _ := ps.GetRawState()
	want := new(big.Int).Add(testLiq, big.NewInt(100000))
	if liq.Cmp(want) != 0 {
		t.Fatalf("liquidity after dedup replay = %s, want %s", liq, want)
	}
}

func TestDrainAndReplaySkipsEventsBelowSnapshot(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{
		pool:               ps,
		logger:             logx.Nop(),
		snapshotStartBlock: 100,
	}

	svc.bufferingMode.Store(true)
	// Event at block 50 (below snapshotStartBlock 100) — should be skipped
	swap := &pool.SwapEvent{
		SqrtPriceX96: testSQ96,
		Tick:         99,
		Liquidity:    testLiq,
		Raw:          types.Log{BlockNumber: 50},
	}
	svc.eventBuffer = []bufferedEvent{{swap: swap}}

	svc.drainAndReplay()

	if svc.bufferingMode.Load() {
		t.Fatal("buffering mode should be disabled after draining")
	}
	_, _, tick := svc.GetPrice()
	if tick != 0 {
		t.Fatalf("tick = %d, want 0 (event below snapshot should be skipped)", tick)
	}
}

func TestBufferedEventBlockNumber(t *testing.T) {
	swapEv := bufferedEvent{swap: &pool.SwapEvent{Raw: types.Log{BlockNumber: 150}}}
	if n := swapEv.blockNumber(); n != 150 {
		t.Errorf("swap blockNumber = %d, want 150", n)
	}

	mintEv := bufferedEvent{mint: &pool.MintEvent{Raw: types.Log{BlockNumber: 151}}}
	if n := mintEv.blockNumber(); n != 151 {
		t.Errorf("mint blockNumber = %d, want 151", n)
	}

	burnEv := bufferedEvent{burn: &pool.BurnEvent{Raw: types.Log{BlockNumber: 152}}}
	if n := burnEv.blockNumber(); n != 152 {
		t.Errorf("burn blockNumber = %d, want 152", n)
	}

	emptyEv := bufferedEvent{}
	if n := emptyEv.blockNumber(); n != 0 {
		t.Errorf("empty blockNumber = %d, want 0", n)
	}
}

func TestApplyBufferedEventMint(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	ev := bufferedEvent{mint: &pool.MintEvent{
		TickLower: -50,
		TickUpper: 50,
		Amount:    big.NewInt(100000),
	}}
	svc.applyBufferedEvent(ev)

	if ps.GetTickCount() != 2 {
		t.Errorf("GetTickCount after buffered mint = %d, want 2", ps.GetTickCount())
	}
	_, _, liq, _ := ps.GetRawState()
	want := new(big.Int).Add(testLiq, big.NewInt(100000))
	if liq.Cmp(want) != 0 {
		t.Errorf("liquidity after buffered mint = %s, want %s", liq, want)
	}
}

func TestApplyBufferedEventBurn(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	ps.UpdateTickFromMint(-50, 50, big.NewInt(100000))
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	ev := bufferedEvent{burn: &pool.BurnEvent{
		TickLower: -50,
		TickUpper: 50,
		Amount:    big.NewInt(100000),
	}}
	svc.applyBufferedEvent(ev)

	// After burning the full position, ticks should be removed
	_, _, liq, _ := ps.GetRawState()
	if liq.Cmp(testLiq) != 0 {
		t.Errorf("liquidity after buffered burn = %s, want %s", liq, testLiq)
	}
}

func TestOnMintBufferingMode(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	svc.bufferingMode.Store(true)

	event := &pool.MintEvent{
		Owner:     common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Amount:    big.NewInt(1000000),
		TickLower: -100,
		TickUpper: 100,
		Raw:       types.Log{BlockNumber: 123},
	}
	svc.OnMint(event)

	// Should be buffered, not applied
	if ps.GetTickCount() != 0 {
		t.Errorf("GetTickCount = %d, want 0 (event should be buffered, not applied)", ps.GetTickCount())
	}
	if svc.bufferLen() != 1 {
		t.Errorf("bufferLen = %d, want 1", svc.bufferLen())
	}
}

func TestOnBurnBufferingMode(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	ps.UpdateTickFromMint(-100, 100, big.NewInt(500000))
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	svc.bufferingMode.Store(true)

	event := &pool.BurnEvent{
		Owner:     common.HexToAddress("0x2222222222222222222222222222222222222222"),
		Amount:    big.NewInt(500000),
		TickLower: -100,
		TickUpper: 100,
		Raw:       types.Log{BlockNumber: 124},
	}
	svc.OnBurn(event)

	// Should be buffered, tick count should remain
	if ps.GetTickCount() != 2 {
		t.Errorf("GetTickCount = %d, want 2 (event should be buffered, not applied)", ps.GetTickCount())
	}
	if svc.bufferLen() != 1 {
		t.Errorf("bufferLen = %d, want 1", svc.bufferLen())
	}
}

func TestTryBufferEventMintAndBurn(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	// Mint event - not buffered when mode off
	mintEv := bufferedEvent{mint: &pool.MintEvent{Raw: types.Log{BlockNumber: 10}}}
	if svc.tryBufferEvent(mintEv) {
		t.Fatal("should not buffer mint when mode is off")
	}

	// Burn event - not buffered when mode off
	burnEv := bufferedEvent{burn: &pool.BurnEvent{Raw: types.Log{BlockNumber: 10}}}
	if svc.tryBufferEvent(burnEv) {
		t.Fatal("should not buffer burn when mode is off")
	}

	svc.bufferingMode.Store(true)

	if !svc.tryBufferEvent(mintEv) {
		t.Fatal("should buffer mint when mode is on")
	}
	if svc.bufferLen() != 1 {
		t.Fatalf("bufferLen = %d, want 1", svc.bufferLen())
	}

	if !svc.tryBufferEvent(burnEv) {
		t.Fatal("should buffer burn when mode is on")
	}
	if svc.bufferLen() != 2 {
		t.Fatalf("bufferLen = %d, want 2", svc.bufferLen())
	}
}

func TestDoLightSyncNilSubscriber(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	err := svc.DoLightSync()
	if err == nil {
		t.Error("expected error with nil subscriber")
	}
}

func TestRebuildTickMapFromChainNilSubscriber(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	err := svc.RebuildTickMapFromChain()
	if err == nil {
		t.Error("expected error with nil subscriber")
	}
}

func TestTickToWord(t *testing.T) {
	wordRange := int32(256) * int32(60) // tickSpacing=60 → wordRange=15360

	tests := []struct {
		tick     int32
		expected int16
	}{
		{0, 0},
		{15360, 1},
		{30720, 2},
		{-1, -1},
		{-15360, -1},
		{-15361, -2},
	}
	for _, tt := range tests {
		got := tickToWord(tt.tick, wordRange)
		if got != tt.expected {
			t.Errorf("tickToWord(%d, %d) = %d, want %d", tt.tick, wordRange, got, tt.expected)
		}
	}
}

func TestTickFromWordBit(t *testing.T) {
	wordRange := int32(256) * int32(60) // tickSpacing=60 → wordRange=15360

	tests := []struct {
		wordPos  int16
		bit      int
		expected int32
	}{
		{0, 0, 0},
		{0, 1, 60},
		{0, 255, 15300},
		{1, 0, 15360},
		{-1, 0, -15360},
		{-1, 255, -60},
	}
	for _, tt := range tests {
		got := tickFromWordBit(tt.wordPos, tt.bit, wordRange, int32(60))
		if got != tt.expected {
			t.Errorf("tickFromWordBit(%d, %d, %d, 60) = %d, want %d",
				tt.wordPos, tt.bit, wordRange, got, tt.expected)
		}
	}
}

func TestFetchTicksConcurrentlyEmpty(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	result := svc.fetchTicksConcurrently(nil)
	if result != nil {
		t.Error("expected nil for nil input")
	}
	result = svc.fetchTicksConcurrently([]int32{})
	if result != nil {
		t.Error("expected nil for empty slice")
	}
}

func TestApplyTickData(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	data := map[int32]*blockchain.TickData{
		10:  {Initialized: true, LiquidityNet: big.NewInt(500)},
		-10: {Initialized: true, LiquidityNet: big.NewInt(-500)},
		20:  {Initialized: false, LiquidityNet: big.NewInt(100)}, // not initialized → skipped
		30:  {Initialized: true, LiquidityNet: big.NewInt(0)},    // zero → skipped
	}
	count := svc.applyTickData(data)
	if count != 2 {
		t.Errorf("applyTickData count = %d, want 2", count)
	}
	if ps.GetTickCount() != 2 {
		t.Errorf("GetTickCount = %d, want 2", ps.GetTickCount())
	}
	if l := ps.GetTickLiquidity(10); l.Cmp(big.NewInt(500)) != 0 {
		t.Errorf("tick 10 = %s, want 500", l)
	}
}

func TestSaveSnapshotNilStore(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop(), store: nil}
	// Should not panic
	svc.saveSnapshot()
}

func TestLoadFromStoreNilStore(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop(), store: nil}
	blockNum, err := svc.LoadFromStore("ethereum")
	if err != nil {
		t.Fatalf("LoadFromStore with nil store should not error: %v", err)
	}
	if blockNum != 0 {
		t.Errorf("blockNum = %d, want 0", blockNum)
	}
}

func TestStartNilSubscriber(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	err := svc.Start(0)
	if err == nil {
		t.Error("expected error with nil subscriber")
	}
}

func TestRunHealthCheckNilSubscriber(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	// Should not panic with nil subscriber
	svc.runHealthCheck()
}

func TestOnSwapBufferingMode(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	svc.bufferingMode.Store(true)

	event := &pool.SwapEvent{
		SqrtPriceX96: testSQ96,
		Tick:         42,
		Liquidity:    testLiq,
		Raw:          types.Log{BlockNumber: 15000000},
	}

	svc.OnSwap(event)

	// Event should be buffered, not applied
	_, _, tick := svc.GetPrice()
	if tick != 0 {
		t.Errorf("tick = %d, want 0 (event should be buffered)", tick)
	}
	if svc.bufferLen() != 1 {
		t.Errorf("bufferLen = %d, want 1", svc.bufferLen())
	}
}

func TestGetPoolInfoFull(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	two96 := new(big.Int).Lsh(big.NewInt(1), 96)
	ps.UpdateFromSwap(two96, 0, testLiq, 100)
	ps.Token0Symbol = "USDC"
	ps.Token1Symbol = "WETH"

	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	info := svc.GetPoolInfo()

	if info["address"] != addrPool1.Hex() {
		t.Errorf("address mismatch")
	}
	if info["token0"] != tkUSDC.Hex() {
		t.Errorf("token0 mismatch")
	}
	if info["token1"] != tkWETH.Hex() {
		t.Errorf("token1 mismatch")
	}
	if info["token0Symbol"] != "USDC" {
		t.Errorf("token0Symbol mismatch")
	}
	if info["token1Symbol"] != "WETH" {
		t.Errorf("token1Symbol mismatch")
	}
	if info["fee"] != uint32(3000) {
		t.Errorf("fee mismatch")
	}
	if info["tick"] != int32(0) {
		t.Errorf("tick mismatch")
	}
}

func TestCollectTicksFromWordsCached(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	// Create bitmaps with entries for word positions 0 and 1
	wordRange := int32(256) * int32(60) // tickSpacing=60
	tickSpacing := int32(60)

	// wordPos=0, bit=1 → tick=60; bit=5 → tick=300
	bitmap0 := big.NewInt(0)
	bitmap0.SetBit(bitmap0, 1, 1) // tick 60
	bitmap0.SetBit(bitmap0, 5, 1) // tick 300

	// wordPos=1, bit=0 → tick=15360
	bitmap1 := big.NewInt(0)
	bitmap1.SetBit(bitmap1, 0, 1) // tick 15360

	bitmaps := map[int16]*big.Int{
		0: bitmap0,
		1: bitmap1,
	}

	words := []int16{0, 1}
	ticks := svc.collectTicksFromWordsCached(words, wordRange, tickSpacing, bitmaps)

	expectedTicks := map[int32]bool{60: true, 300: true, 15360: true}
	found := make(map[int32]bool)
	for _, tick := range ticks {
		found[tick] = true
	}
	for expected := range expectedTicks {
		if !found[expected] {
			t.Errorf("missing tick %d", expected)
		}
	}
	if len(ticks) != 3 {
		t.Errorf("got %d ticks, want 3: %v", len(ticks), ticks)
	}
}

func TestCollectTicksFromWordsCachedEmptyWords(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	ticks := svc.collectTicksFromWordsCached(nil, 15360, 60, nil)
	if len(ticks) != 0 {
		t.Errorf("expected empty ticks, got %d", len(ticks))
	}
}

func TestCollectTicksFromWordsCachedAllZeroBitmap(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	bitmap := big.NewInt(0) // all zeros, no active ticks
	bitmaps := map[int16]*big.Int{0: bitmap}
	wordRange := int32(256) * int32(60)

	ticks := svc.collectTicksFromWordsCached([]int16{0}, wordRange, 60, bitmaps)
	if len(ticks) != 0 {
		t.Errorf("expected empty ticks from zero bitmap, got %d", len(ticks))
	}
}

func TestCollectTicksFromWordsCachedTicksOutOfRange(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	// Word position that would produce ticks outside the valid range
	// tickFromWordBit(max word, 255, ...) > TickMax for some configurations
	wordRange := int32(256) * int32(60)

	// wordPos=500 → tick=500*15360=7680000 which is > TickMax (887272)
	// all bits in this word are out of range, so should be filtered
	bitmap := big.NewInt(0)
	bitmap.SetBit(bitmap, 100, 1) // tick = 500*15360 + 100*60 = 7680000+6000 > 887272
	bitmaps := map[int16]*big.Int{500: bitmap}

	ticks := svc.collectTicksFromWordsCached([]int16{500}, wordRange, 60, bitmaps)
	if len(ticks) != 0 {
		t.Errorf("expected empty ticks (all out of range), got %d", len(ticks))
	}
}

func TestRunBufferedSync(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	svc := &PoolQuoteService{
		pool:               ps,
		logger:             logx.Nop(),
		snapshotStartBlock: 100,
	}

	// syncFn that succeeds
	err := svc.runBufferedSync(func() error { return nil }, 0)
	if err != nil {
		t.Errorf("runBufferedSync should succeed: %v", err)
	}
	if svc.bufferingMode.Load() {
		t.Error("buffering mode should be disabled after sync")
	}
}

func TestRunBufferedSyncWithError(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	svc := &PoolQuoteService{
		pool:               ps,
		logger:             logx.Nop(),
		snapshotStartBlock: 100,
	}

	// syncFn that fails — should still disable buffering and drain
	svc.bufferingMode.Store(true)
	err := svc.runBufferedSync(func() error { return fmt.Errorf("sync failed") }, 0)
	if err == nil {
		t.Error("expected error from sync function")
	}
	if svc.bufferingMode.Load() {
		t.Error("buffering mode should be disabled even on error")
	}
}

func TestRunBufferedFullSync(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	// runBufferedFullSync calls DoFullSync which needs subscriber
	// With nil subscriber it will fail, but should still disable buffering
	svc.bufferingMode.Store(true)
	err := svc.runBufferedFullSync(0, 0)
	if err == nil {
		t.Error("expected error with nil subscriber")
	}
	if svc.bufferingMode.Load() {
		t.Error("buffering mode should be disabled")
	}
}

func TestRunBufferedLightSync(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	// runBufferedLightSync calls DoLightSync which needs subscriber
	svc.bufferingMode.Store(true)
	err := svc.runBufferedLightSync(0)
	if err == nil {
		t.Error("expected error with nil subscriber")
	}
	if svc.bufferingMode.Load() {
		t.Error("buffering mode should be disabled")
	}
}

func TestDoFullSyncUninitializedPool(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	// Don't call UpdateFromSwap — pool has memBlock=0
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	err := svc.DoFullSync(0)
	if err == nil {
		t.Error("expected error with nil subscriber")
	}
}

func TestRebuildTickMapFromChainEmptyTicks(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	err := svc.RebuildTickMapFromChain()
	if err == nil {
		t.Error("expected error with nil subscriber for uninitialized pool")
	}
}

func TestOnReconnectedNilSubscriber(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	// OnReconnected checks subscriber == nil
	svc.OnReconnected()
	// Should not panic
}

func TestSaveSnapshotWithTicks(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	ps.UpdateTickFromMint(-10, 10, big.NewInt(500))
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop(), store: nil}
	// Should not panic with nil store and ticks present
	svc.saveSnapshot()
}

func TestMaybeFlushSnapshotBeforeEventFlushesOncePerBlock(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	st := &recordingStore{}
	svc := &PoolQuoteService{
		pool:      ps,
		logger:    logx.Nop(),
		store:     st,
		bgCtx:     context.Background(),
		chainName: "ethereum",
	}

	// block 100: 聚合多个事件，不立即更新内存。
	svc.stageLiveEvent(bufferedEvent{
		swap: &pool.SwapEvent{
			SqrtPriceX96: testSQ96,
			Tick:         11,
			Liquidity:    testLiq,
			Raw:          types.Log{BlockNumber: 100, Index: 1},
		},
	})
	svc.stageLiveEvent(bufferedEvent{
		mint: &pool.MintEvent{
			Amount:    big.NewInt(1000),
			TickLower: -10,
			TickUpper: 10,
			Raw:       types.Log{BlockNumber: 100, Index: 2},
		},
	})
	// block 101 到来，触发 block 100 的批量提交。
	svc.stageLiveEvent(bufferedEvent{
		swap: &pool.SwapEvent{
			SqrtPriceX96: testSQ96,
			Tick:         12,
			Liquidity:    testLiq,
			Raw:          types.Log{BlockNumber: 101, Index: 1},
		},
	})
	svc.bgWG.Wait()

	blocks := st.blocks()
	if len(blocks) != 1 {
		t.Fatalf("saved blocks = %v, want one snapshot", blocks)
	}
	if blocks[0] != 100 {
		t.Fatalf("saved block = %d, want 100", blocks[0])
	}
	_, _, tick := svc.GetPrice()
	if tick != 11 {
		t.Fatalf("tick = %d, want 11 (block 100 applied once)", tick)
	}
}

func TestMaybeFlushSnapshotByTimeFlushesWhenRPCBlockAdvances(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 200)
	st := &recordingStore{}
	svc := &PoolQuoteService{
		pool:                     ps,
		logger:                   logx.Nop(),
		store:                    st,
		bgCtx:                    context.Background(),
		chainName:                "ethereum",
		snapshotMaxWriteInterval: 20 * time.Millisecond,
		fetchCurrentBlockNumber: func() (uint64, error) {
			return 201, nil
		},
	}

	svc.stageLiveEvent(bufferedEvent{
		swap: &pool.SwapEvent{
			SqrtPriceX96: testSQ96,
			Tick:         21,
			Liquidity:    testLiq,
			Raw:          types.Log{BlockNumber: 200, Index: 1},
		},
	})
	svc.maybeFlushSnapshotByTime() // rpc block advanced => flush
	svc.bgWG.Wait()

	blocks := st.blocks()
	if len(blocks) != 1 {
		t.Fatalf("saved blocks = %v, want one snapshot", blocks)
	}
	if blocks[0] != 200 {
		t.Fatalf("saved block = %d, want 200", blocks[0])
	}
}

func TestMaybeFlushSnapshotByTimeWithoutNewEvent(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 300)
	st := &recordingStore{}
	svc := &PoolQuoteService{
		pool:                     ps,
		logger:                   logx.Nop(),
		store:                    st,
		bgCtx:                    context.Background(),
		chainName:                "ethereum",
		snapshotMaxWriteInterval: 20 * time.Millisecond,
		fetchCurrentBlockNumber: func() (uint64, error) {
			return 300, nil
		},
	}

	// 仅收到同一 block 的事件后，不再有新事件。
	svc.stageLiveEvent(bufferedEvent{
		swap: &pool.SwapEvent{
			SqrtPriceX96: testSQ96,
			Tick:         31,
			Liquidity:    testLiq,
			Raw:          types.Log{BlockNumber: 300, Index: 1},
		},
	})
	// 模拟后台 ticker 的一次检查：rpc block 仍等于 pending block，不应写入。
	svc.maybeFlushSnapshotByTime()
	svc.bgWG.Wait()

	blocks := st.blocks()
	if len(blocks) != 0 {
		t.Fatalf("saved blocks = %v, want none while rpc block has not advanced", blocks)
	}
}

func TestSkipFlushWhileFullSyncInProgress(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 400)
	st := &recordingStore{}
	svc := &PoolQuoteService{
		pool:                     ps,
		logger:                   logx.Nop(),
		store:                    st,
		bgCtx:                    context.Background(),
		chainName:                "ethereum",
		snapshotMaxWriteInterval: 20 * time.Millisecond,
	}

	svc.stageLiveEvent(bufferedEvent{
		swap: &pool.SwapEvent{
			SqrtPriceX96: testSQ96,
			Tick:         41,
			Liquidity:    testLiq,
			Raw:          types.Log{BlockNumber: 400, Index: 1},
		},
	})

	svc.fullSyncInProgress.Store(true)
	time.Sleep(30 * time.Millisecond)
	svc.maybeFlushSnapshotByTime()
	svc.flushPendingSnapshotIfDue(401, false)
	svc.fullSyncInProgress.Store(false)
	svc.bgWG.Wait()

	if blocks := st.blocks(); len(blocks) != 0 {
		t.Fatalf("saved blocks during full sync = %v, want none", blocks)
	}
}
