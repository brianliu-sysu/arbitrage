// Package subscriber 提供对 Uniswap V3 池子事件的实时订阅，内置 WebSocket 断线重连。
package subscriber

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/metrics"
	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/brianliu-sysu/arbitrage/internal/tracing"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// tickBitmap(int16) 函数选择器
var tickBitmapSelector = crypto.Keccak256([]byte("tickBitmap(int16)"))[:4]

// mustNewABIType panics on error (type strings are compile-time constants).
func mustNewABIType(kind string) abi.Type {
	t, err := abi.NewType(kind, "", nil)
	if err != nil {
		panic(fmt.Sprintf("abi.NewType(%q): %v", kind, err))
	}
	return t
}

// maskAPIKey 将 URL 中的 API key 替换为 ***，防止敏感信息泄露到日志中。
func maskAPIKey(rawURL string) string {
	lastSlash := strings.LastIndex(rawURL, "/")
	if lastSlash < 0 || lastSlash >= len(rawURL)-1 {
		return rawURL
	}
	prefix := rawURL[:lastSlash+1]
	keyPart := rawURL[lastSlash+1:]
	if idx := strings.Index(keyPart, "?"); idx >= 0 {
		return prefix + "***" + keyPart[idx:]
	}
	if len(keyPart) < 16 {
		return rawURL
	}
	return prefix + "***"
}

// dialAndMaskError 拨号并在失败时将错误消息中的 API key 脱敏。
func dialAndMaskError(rawURL string) (*ethclient.Client, error) {
	client, err := ethclient.Dial(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%s", maskAPIKey(err.Error()))
	}
	return client, nil
}

// getRPCClient 懒加载持久 HTTP RPC 客户端（复用连接池）。
func (s *Subscriber) getRPCClient() *ethclient.Client {
	s.rpcClientMu.Lock()
	defer s.rpcClientMu.Unlock()
	if !s.rpcClientInit {
		client, err := dialAndMaskError(s.rpcDial)
		if err != nil {
			s.logger.Error("failed to create persistent RPC client", "error", err)
			s.rpcClientInit = true // don't retry forever
			return nil
		}
		s.rpcClient = client
		s.rpcClientInit = true
	}
	return s.rpcClient
}

// rpcDialWithRetry 获取 HTTP RPC 客户端。
// 第一次调用时创建持久连接（复用连接池），后续调用返回同一个 client。
// 如果持久 client 未创建成功，回退到每次 Dial 短连接。
func (s *Subscriber) rpcDialWithRetry() (*ethclient.Client, func(), error) {
	client := s.getRPCClient()
	if client != nil {
		return client, func() {}, nil
	}
	// 降级：创建临时短连接
	c, err := dialAndMaskError(s.rpcDial)
	if err != nil {
		return nil, nil, err
	}
	return c, func() { c.Close() }, nil
}

// resetRPCClient 关闭并清除持久 RPC 客户端，下次调用会重新创建连接。
// 当 CallContract 返回 "connection reset" / "broken pipe" / "EOF" 等连接级错误时调用。
func (s *Subscriber) resetRPCClient() {
	s.rpcClientMu.Lock()
	defer s.rpcClientMu.Unlock()
	if s.rpcClient != nil {
		s.rpcClient.Close()
		s.rpcClient = nil
	}
	s.rpcClientInit = false // 允许重新初始化
}

// isRetryableHTTPError 判断是否是应该重试的 HTTP 错误 (429/5xx)。
func isRetryableHTTPError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "500") ||
		strings.Contains(msg, "502") ||
		strings.Contains(msg, "503") ||
		strings.Contains(msg, "504")
}

// isConnectionError 判断是否是连接级错误（应触发重连而非重试）。
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// HTTP 传输层错误
	for _, keyword := range []string{
		"connection reset",
		"broken pipe",
		"unexpected EOF",
		"EOF",
		"no such host",
		"connection refused",
		"i/o timeout",
		"TLS handshake",
	} {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	return false
}

// maskError 脱敏错误消息中的 API key。
func maskError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", maskAPIKey(err.Error()))
}

const (
	initialReconnectBackoff = 1 * time.Second
	maxReconnectBackoff     = 60 * time.Second
)

// EventHandler 定义事件处理的回调接口。
type EventHandler interface {
	OnSwap(event *pool.SwapEvent)
	OnMint(event *pool.MintEvent)
	OnBurn(event *pool.BurnEvent)
	OnError(err error)
	OnReconnected()
}

// Subscriber 负责订阅 Uniswap V3 Pool 合约的事件日志，内置 WebSocket 断线重连。
type Subscriber struct {
	logger   logx.Logger
	wsURL    string // WebSocket 端点（已脱敏，用于日志；实际连接用 wsDial）
	rpcURL   string // HTTP RPC 端点（已脱敏，用于日志；实际连接用 rpcDial）
	wsDial   string // 真实 WebSocket URL（含 API key）
	rpcDial  string // 真实 HTTP RPC URL（含 API key）
	poolAddr common.Address
	handler  EventHandler

	rpcClient     *ethclient.Client // 持久 HTTP 客户端（复用连接池）
	rpcClientMu   sync.Mutex
	rpcClientInit bool

	connectedOnce bool
	seenLogKeys   map[logDedupKey]struct{} // WS 事件去重

	multicallAddr common.Address // Multicall3 合约地址
	quoterAddr    common.Address // Uniswap V3 Quoter 合约地址

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// logDedupKey 用于标识一条唯一日志。
// 同一交易可能包含多条日志，必须包含 logIndex 才不会误去重。
type logDedupKey struct {
	BlockHash common.Hash
	TxHash    common.Hash
	LogIndex  uint
}

// NewSubscriber 创建事件订阅器。
//
// wsURL / rpcURL 中的 API key 会在存储时自动脱敏（日志中只显示 ***）。
// 真实 URL 保存在 wsDial / rpcDial，仅在拨号时使用。
// multicallAddr 为 Multicall3 合约地址，zero address 表示不使用批量查询。
func NewSubscriber(wsURL, rpcURL string, poolAddr common.Address, handler EventHandler, multicallAddr common.Address, quoterAddr common.Address, logger logx.Logger) (*Subscriber, error) {
	ctx, cancel := context.WithCancel(context.Background())
	return &Subscriber{
		seenLogKeys:   make(map[logDedupKey]struct{}),
		logger:        logger,
		wsURL:         maskAPIKey(wsURL),
		rpcURL:        maskAPIKey(rpcURL),
		wsDial:        wsURL,
		rpcDial:       rpcURL,
		poolAddr:      poolAddr,
		handler:       handler,
		multicallAddr: multicallAddr,
		quoterAddr:    quoterAddr,
		ctx:           ctx,
		cancel:        cancel,
	}, nil
}

// Start 启动实时事件订阅（带自动重连）。
func (s *Subscriber) Start() error {
	query := ethereum.FilterQuery{
		Addresses: []common.Address{s.poolAddr},
		Topics: [][]common.Hash{{
			pool.SwapEventSignature,
			pool.MintEventSignature,
			pool.BurnEventSignature,
		}},
	}
	s.wg.Add(1)
	go s.runReconnectLoop(query)
	return nil
}

func (s *Subscriber) runReconnectLoop(query ethereum.FilterQuery) {
	defer s.wg.Done()
	backoff := initialReconnectBackoff

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		client, err := dialAndMaskError(s.wsDial)
		if err != nil {
			s.logger.Warn("dial failed, retrying", "pool", s.poolAddr.Hex(), "error", err, "backoff", backoff)
			backoff = s.sleepOrCancel(backoff)
			continue
		}

		logsCh := make(chan types.Log, 100)
		sub, err := client.SubscribeFilterLogs(s.ctx, query, logsCh)
		if err != nil {
			s.logger.Warn("subscribe failed, retrying", "pool", s.poolAddr.Hex(), "error", err, "backoff", backoff)
			client.Close()
			backoff = s.sleepOrCancel(backoff)
			continue
		}

		backoff = initialReconnectBackoff

		shouldNotifyReconnect := s.markConnected()

		// 首次连接仅表示 cold start，不应触发“重连恢复”逻辑。
		if shouldNotifyReconnect && s.handler != nil {
			s.handler.OnReconnected()
		}

		ctx, span := tracing.Tracer().Start(s.ctx, "subscriber.event_loop",
			trace.WithAttributes(attribute.String("pool", s.poolAddr.Hex())))
		s.runEventLoop(ctx, client, sub, logsCh)
		span.End()

		s.logger.Warn("disconnected, reconnecting", "pool", s.poolAddr.Hex(), "backoff", backoff)
		backoff = s.sleepOrCancel(backoff)
	}
}

func (s *Subscriber) runEventLoop(_ context.Context, client *ethclient.Client, sub ethereum.Subscription, logsCh chan types.Log) {
	defer client.Close()
	for {
		select {
		case err := <-sub.Err():
			s.logger.Warn("subscription error", "pool", s.poolAddr.Hex(), "error", err)
			sub.Unsubscribe()
			return
		case vLog := <-logsCh:
			s.processLog(vLog)
		case <-s.ctx.Done():
			sub.Unsubscribe()
			return
		}
	}
}

func (s *Subscriber) sleepOrCancel(backoff time.Duration) time.Duration {
	select {
	case <-time.After(backoff):
	case <-s.ctx.Done():
		return -1
	}
	next := backoff * 2
	if next > maxReconnectBackoff {
		next = maxReconnectBackoff
	}
	return next
}

func (s *Subscriber) Stop() {
	s.cancel()
	s.wg.Wait()
}

func (s *Subscriber) SyncHistorical(fromBlock, toBlock *big.Int) error {
	client, done, err := s.rpcDialWithRetry()
	if err != nil {
		return fmt.Errorf("dial for historical sync: %w", err)
	}
	defer done()

	logs, err := client.FilterLogs(s.ctx, ethereum.FilterQuery{
		Addresses: []common.Address{s.poolAddr},
		Topics:    [][]common.Hash{{pool.SwapEventSignature}},
		FromBlock: fromBlock,
		ToBlock:   toBlock,
	})
	if err != nil {
		return fmt.Errorf("filter historical logs: %w", err)
	}

	if len(logs) > 0 {
		lastLog := logs[len(logs)-1]
		event, err := pool.ParseSwapEvent(lastLog)
		if err != nil {
			return fmt.Errorf("parse last swap event: %w", err)
		}
		s.handler.OnSwap(event)
		s.logger.Info("initialized from historical swap", "block", lastLog.BlockNumber)
	} else {
		s.logger.Info("no historical swaps found, waiting for real-time events")
	}
	return nil
}

func (s *Subscriber) SyncHistoricalAll(fromBlock, toBlock *big.Int) error {
	client, done, err := s.rpcDialWithRetry()
	if err != nil {
		return fmt.Errorf("dial for historical sync: %w", err)
	}
	defer done()

	logs, err := client.FilterLogs(s.ctx, ethereum.FilterQuery{
		Addresses: []common.Address{s.poolAddr},
		Topics:    [][]common.Hash{{pool.SwapEventSignature, pool.MintEventSignature, pool.BurnEventSignature}},
		FromBlock: fromBlock,
		ToBlock:   toBlock,
	})
	if err != nil {
		return fmt.Errorf("filter historical logs: %w", err)
	}

	if len(logs) == 0 {
		s.logger.Info("no historical events found, waiting for real-time events")
		return nil
	}

	var lastSwapLog *types.Log
	mintCount, burnCount, swapCount := 0, 0, 0

	for i := range logs {
		switch logs[i].Topics[0] {
		case pool.SwapEventSignature:
			event, err := pool.ParseSwapEvent(logs[i])
			if err != nil {
				return fmt.Errorf("parse swap at index %d: %w", i, err)
			}
			s.handler.OnSwap(event)
			lastSwapLog = &logs[i]
			swapCount++

		case pool.MintEventSignature:
			event, err := pool.ParseMintEvent(logs[i])
			if err != nil {
				s.logger.Warn("skipping bad mint event", "index", i, "error", err)
				continue
			}
			s.handler.OnMint(event)
			mintCount++

		case pool.BurnEventSignature:
			event, err := pool.ParseBurnEvent(logs[i])
			if err != nil {
				s.logger.Warn("skipping bad burn event", "index", i, "error", err)
				continue
			}
			s.handler.OnBurn(event)
			burnCount++
		}
	}

	s.logger.Info("historical sync complete",
		"swaps", swapCount, "mints", mintCount, "burns", burnCount,
		"fromBlock", logs[0].BlockNumber, "toBlock", logs[len(logs)-1].BlockNumber)
	if lastSwapLog != nil {
		s.logger.Info("last historical swap", "block", lastSwapLog.BlockNumber)
	}
	return nil
}

// PoolStateRPC 通过 RPC 调用获取的池子状态快照。
type PoolStateRPC struct {
	SqrtPriceX96 *big.Int
	Tick         int32
	Liquidity    *big.Int
	BlockNumber  uint64
}

func (s *Subscriber) FetchStateViaRPC() (*PoolStateRPC, error) {
	client, done, err := s.rpcDialWithRetry()
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	defer done()

	const poolABI = `[{"inputs":[],"name":"slot0","outputs":[{"internalType":"uint160","name":"sqrtPriceX96","type":"uint160"},{"internalType":"int24","name":"tick","type":"int24"},{"internalType":"uint16","name":"observationIndex","type":"uint16"},{"internalType":"uint16","name":"observationCardinality","type":"uint16"},{"internalType":"uint16","name":"observationCardinalityNext","type":"uint16"},{"internalType":"uint8","name":"feeProtocol","type":"uint8"},{"internalType":"bool","name":"unlocked","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"liquidity","outputs":[{"internalType":"uint128","name":"","type":"uint128"}],"stateMutability":"view","type":"function"}]`

	parsed, err := abi.JSON(strings.NewReader(poolABI))
	if err != nil {
		return nil, fmt.Errorf("parse pool abi: %w", err)
	}

	slot0Data, _ := parsed.Pack("slot0")
	slot0Result, err := client.CallContract(s.ctx, ethereum.CallMsg{To: &s.poolAddr, Data: slot0Data}, nil)
	if isConnectionError(err) {
		s.resetRPCClient()
	}
	if err != nil {
		return nil, maskError(fmt.Errorf("call slot0: %w", err))
	}
	slot0Unpacked, _ := parsed.Unpack("slot0", slot0Result)
	sqrtPriceX96 := slot0Unpacked[0].(*big.Int)
	tick := int32(slot0Unpacked[1].(*big.Int).Int64())

	liqData, _ := parsed.Pack("liquidity")
	liqResult, err := client.CallContract(s.ctx, ethereum.CallMsg{To: &s.poolAddr, Data: liqData}, nil)
	if isConnectionError(err) {
		s.resetRPCClient()
	}
	if err != nil {
		return nil, maskError(fmt.Errorf("call liquidity: %w", err))
	}
	liqUnpacked, _ := parsed.Unpack("liquidity", liqResult)
	liquidity := liqUnpacked[0].(*big.Int)

	header, err := client.HeaderByNumber(s.ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("get header: %w", err)
	}

	return &PoolStateRPC{
		SqrtPriceX96: sqrtPriceX96,
		Tick:         tick,
		Liquidity:    liquidity,
		BlockNumber:  header.Number.Uint64(),
	}, nil
}

// FetchBlockNumber 返回当前链上最新区块高度。
func (s *Subscriber) FetchBlockNumber() (uint64, error) {
	client, done, err := s.rpcDialWithRetry()
	if err != nil {
		return 0, fmt.Errorf("dial: %w", err)
	}
	defer done()

	header, err := client.HeaderByNumber(s.ctx, nil)
	if err != nil {
		return 0, maskError(fmt.Errorf("get header: %w", err))
	}
	return header.Number.Uint64(), nil
}

func (s *Subscriber) SimulateSwap(token0, token1 common.Address, fee uint32, amountIn *big.Int, zeroForOne bool, sqrtPriceLimitX96 *big.Int) (*big.Int, error) {
	_, span := tracing.Tracer().Start(s.ctx, "subscriber.simulate_swap",
		trace.WithAttributes(
			attribute.String("pool", s.poolAddr.Hex()),
			attribute.String("amount_in", amountIn.String()),
			attribute.Bool("zero_for_one", zeroForOne),
		))
	defer span.End()

	if s.quoterAddr == (common.Address{}) {
		return nil, fmt.Errorf("quoter address is not configured")
	}

	client, done, err := s.rpcDialWithRetry()
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	defer done()

	if sqrtPriceLimitX96 == nil {
		// QuoterV2 uses 0 as "no price limit". The pool.swap min/max bounds
		// used for direct swap simulation can make quoteExactInputSingle revert.
		sqrtPriceLimitX96 = big.NewInt(0)
	}

	tokenIn := token0
	tokenOut := token1
	if !zeroForOne {
		tokenIn = token1
		tokenOut = token0
	}

	const quoterV2ABI = `[{"inputs":[{"components":[{"internalType":"address","name":"tokenIn","type":"address"},{"internalType":"address","name":"tokenOut","type":"address"},{"internalType":"uint256","name":"amountIn","type":"uint256"},{"internalType":"uint24","name":"fee","type":"uint24"},{"internalType":"uint160","name":"sqrtPriceLimitX96","type":"uint160"}],"internalType":"struct IQuoterV2.QuoteExactInputSingleParams","name":"params","type":"tuple"}],"name":"quoteExactInputSingle","outputs":[{"internalType":"uint256","name":"amountOut","type":"uint256"},{"internalType":"uint160","name":"sqrtPriceX96After","type":"uint160"},{"internalType":"uint32","name":"initializedTicksCrossed","type":"uint32"},{"internalType":"uint256","name":"gasEstimate","type":"uint256"}],"stateMutability":"nonpayable","type":"function"}]`
	v2Parsed, err := abi.JSON(strings.NewReader(quoterV2ABI))
	if err != nil {
		return nil, fmt.Errorf("parse quoter abi: %w", err)
	}
	type quoteExactInputSingleParams struct {
		TokenIn           common.Address `abi:"tokenIn"`
		TokenOut          common.Address `abi:"tokenOut"`
		AmountIn          *big.Int       `abi:"amountIn"`
		Fee               *big.Int       `abi:"fee"`
		SqrtPriceLimitX96 *big.Int       `abi:"sqrtPriceLimitX96"`
	}
	v2Data, err := v2Parsed.Pack("quoteExactInputSingle", quoteExactInputSingleParams{
		TokenIn:           tokenIn,
		TokenOut:          tokenOut,
		AmountIn:          amountIn,
		Fee:               new(big.Int).SetUint64(uint64(fee)),
		SqrtPriceLimitX96: sqrtPriceLimitX96,
	})
	if err != nil {
		return nil, fmt.Errorf("pack quoter quoteExactInputSingle: %w", err)
	}
	result, err := client.CallContract(s.ctx, ethereum.CallMsg{To: &s.quoterAddr, Data: v2Data}, nil)
	if err != nil {
		if isConnectionError(err) {
			s.resetRPCClient()
		}
		return nil, maskError(fmt.Errorf("call quoter quoteExactInputSingle: %w", err))
	}

	unpacked, err := v2Parsed.Unpack("quoteExactInputSingle", result)
	if err != nil || len(unpacked) == 0 {
		return nil, fmt.Errorf("unpack quoter result: %w", err)
	}
	amountOut, ok := unpacked[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected amountOut type %T", unpacked[0])
	}
	return new(big.Int).Set(amountOut), nil
}

// PoolMetadata 池子的静态元数据。
type PoolMetadata struct {
	Token0 common.Address
	Token1 common.Address
	Fee    uint32
}

// TokenMetadata 代币元信息（symbol + decimals）。
type TokenMetadata struct {
	Symbol   string
	Decimals int
}

func (s *Subscriber) FetchPoolMetadata() (*PoolMetadata, error) {
	client, done, err := s.rpcDialWithRetry()
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	defer done()

	const metaABI = `[{"inputs":[],"name":"token0","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"token1","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"fee","outputs":[{"internalType":"uint24","name":"","type":"uint24"}],"stateMutability":"view","type":"function"}]`

	parsed, _ := abi.JSON(strings.NewReader(metaABI))
	call := func(name string) ([]interface{}, error) {
		data, _ := parsed.Pack(name)
		result, err := client.CallContract(s.ctx, ethereum.CallMsg{To: &s.poolAddr, Data: data}, nil)
		if err != nil {
			return nil, fmt.Errorf("call %s: %w", name, err)
		}
		return parsed.Unpack(name, result)
	}

	t0, err := call("token0")
	if err != nil {
		return nil, err
	}
	t1, err := call("token1")
	if err != nil {
		return nil, err
	}
	fee, err := call("fee")
	if err != nil {
		return nil, err
	}

	return &PoolMetadata{
		Token0: t0[0].(common.Address),
		Token1: t1[0].(common.Address),
		Fee:    uint32(fee[0].(*big.Int).Uint64()),
	}, nil
}

// FetchTokenMetadata 通过 ERC20 标准接口获取 symbol 和 decimals。
// symbol 优先按 string 解码，若失败则回退 bytes32 兼容解码。
func (s *Subscriber) FetchTokenMetadata(tokenAddr common.Address) (*TokenMetadata, error) {
	client, done, err := s.rpcDialWithRetry()
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	defer done()

	const erc20MetaABI = `[{"inputs":[],"name":"symbol","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"decimals","outputs":[{"internalType":"uint8","name":"","type":"uint8"}],"stateMutability":"view","type":"function"}]`
	parsed, err := abi.JSON(strings.NewReader(erc20MetaABI))
	if err != nil {
		return nil, fmt.Errorf("parse erc20 metadata abi: %w", err)
	}

	symbolData, _ := parsed.Pack("symbol")
	symbolRaw, err := client.CallContract(s.ctx, ethereum.CallMsg{To: &tokenAddr, Data: symbolData}, nil)
	if err != nil {
		if isConnectionError(err) {
			s.resetRPCClient()
		}
		return nil, maskError(fmt.Errorf("call symbol for %s: %w", tokenAddr.Hex(), err))
	}

	symbol := ""
	if unpacked, err := parsed.Unpack("symbol", symbolRaw); err == nil && len(unpacked) > 0 {
		if v, ok := unpacked[0].(string); ok {
			symbol = strings.TrimSpace(v)
		}
	}
	// 兼容部分老代币将 symbol() 实现为 bytes32。
	if symbol == "" && len(symbolRaw) >= 32 {
		symbol = strings.TrimSpace(string(bytes.TrimRight(symbolRaw[:32], "\x00")))
	}

	decimalsData, _ := parsed.Pack("decimals")
	decimalsRaw, err := client.CallContract(s.ctx, ethereum.CallMsg{To: &tokenAddr, Data: decimalsData}, nil)
	if err != nil {
		if isConnectionError(err) {
			s.resetRPCClient()
		}
		return nil, maskError(fmt.Errorf("call decimals for %s: %w", tokenAddr.Hex(), err))
	}

	decimalsUnpacked, err := parsed.Unpack("decimals", decimalsRaw)
	if err != nil || len(decimalsUnpacked) == 0 {
		return nil, fmt.Errorf("unpack decimals for %s: %w", tokenAddr.Hex(), err)
	}
	decimals, ok := decimalsUnpacked[0].(uint8)
	if !ok {
		return nil, fmt.Errorf("unexpected decimals type %T for %s", decimalsUnpacked[0], tokenAddr.Hex())
	}

	return &TokenMetadata{
		Symbol:   symbol,
		Decimals: int(decimals),
	}, nil
}

// TickData 单个 tick 的链上数据。
type TickData struct {
	LiquidityGross *big.Int
	LiquidityNet   *big.Int
	Initialized    bool
}

func (s *Subscriber) FetchTickInfo(tick int32) (*TickData, error) {
	client, done, err := s.rpcDialWithRetry()
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	defer done()

	const tickABI = `[{"inputs":[{"internalType":"int24","name":"tick","type":"int24"}],"name":"ticks","outputs":[{"internalType":"uint128","name":"liquidityGross","type":"uint128"},{"internalType":"int128","name":"liquidityNet","type":"int128"},{"internalType":"uint256","name":"feeGrowthOutside0X128","type":"uint256"},{"internalType":"uint256","name":"feeGrowthOutside1X128","type":"uint256"},{"internalType":"int56","name":"tickCumulativeOutside","type":"int56"},{"internalType":"uint160","name":"secondsPerLiquidityOutsideX128","type":"uint160"},{"internalType":"uint32","name":"secondsOutside","type":"uint32"},{"internalType":"bool","name":"initialized","type":"bool"}],"stateMutability":"view","type":"function"}]`

	parsed, _ := abi.JSON(strings.NewReader(tickABI))
	data, _ := parsed.Pack("ticks", big.NewInt(int64(tick)))
	result, err := client.CallContract(s.ctx, ethereum.CallMsg{To: &s.poolAddr, Data: data}, nil)
	if err != nil {
		// 连接级错误 → 重置持久连接，下次调用自动重建
		if isConnectionError(err) {
			s.resetRPCClient()
		}
		return nil, maskError(fmt.Errorf("call ticks(%d): %w", tick, err))
	}
	unpacked, _ := parsed.Unpack("ticks", result)

	return &TickData{
		LiquidityGross: unpacked[0].(*big.Int),
		LiquidityNet:   unpacked[1].(*big.Int),
		Initialized:    unpacked[7].(bool),
	}, nil
}

func (s *Subscriber) FetchTickBitmap(wordPos int16) (*big.Int, error) {
	client, done, err := s.rpcDialWithRetry()
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	defer done()

	int16Type := mustNewABIType("int16")
	uint256Type := mustNewABIType("uint256")

	inputArgs := abi.Arguments{{Type: int16Type}}
	data, _ := inputArgs.Pack(wordPos)
	result, err := client.CallContract(s.ctx, ethereum.CallMsg{
		To:   &s.poolAddr,
		Data: append(tickBitmapSelector, data...),
	}, nil)
	if err != nil {
		if isConnectionError(err) {
			s.resetRPCClient()
		}
		return nil, maskError(fmt.Errorf("call tickBitmap(%d): %w", wordPos, err))
	}
	outputArgs := abi.Arguments{{Type: uint256Type}}
	unpacked, _ := outputArgs.Unpack(result)
	return unpacked[0].(*big.Int), nil
}

// maxMulticallBatchSize 是单次 Multicall3 aggregate3 调用的最大子调用数。
// 每次子调用约 132 字节，800 次 ≈ 105KB，远低于多数 RPC 代理的 1MB 限制。
const maxMulticallBatchSize = 800

// FetchTickBitmapBatch 通过 Multicall3 批量获取多个 word 的 tick bitmap。
// 内部自动分块，避免单次 RPC 调用的 calldata 超过代理限制（413 Payload Too Large）。
func (s *Subscriber) FetchTickBitmapBatch(wordPositions []int16) (map[int16]*big.Int, error) {
	if len(wordPositions) == 0 {
		return nil, nil
	}

	client, done, err := s.rpcDialWithRetry()
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	defer done()

	int16Type := mustNewABIType("int16")
	inputArgs := abi.Arguments{{Type: int16Type}}

	type call3 struct {
		Target       common.Address
		AllowFailure bool   `json:"allowFailure"`
		CallData     []byte `json:"callData"`
	}

	multicallABI := `[{"inputs":[{"components":[{"internalType":"address","name":"target","type":"address"},{"internalType":"bool","name":"allowFailure","type":"bool"},{"internalType":"bytes","name":"callData","type":"bytes"}],"internalType":"struct Multicall3.Call3[]","name":"calls","type":"tuple[]"}],"name":"aggregate3","outputs":[{"components":[{"internalType":"bool","name":"success","type":"bool"},{"internalType":"bytes","name":"returnData","type":"bytes"}],"internalType":"struct Multicall3.Result[]","name":"returnData","type":"tuple[]"}],"stateMutability":"payable","type":"function"}]`

	parsed, err := abi.JSON(strings.NewReader(multicallABI))
	if err != nil {
		return nil, fmt.Errorf("parse multicall abi: %w", err)
	}

	uint256Type := mustNewABIType("uint256")
	bitmapArgs := abi.Arguments{{Type: uint256Type}}

	bitmaps := make(map[int16]*big.Int, len(wordPositions))

	// 分块执行，每块最多 maxMulticallBatchSize 次调用
	for start := 0; start < len(wordPositions); start += maxMulticallBatchSize {
		end := start + maxMulticallBatchSize
		if end > len(wordPositions) {
			end = len(wordPositions)
		}
		chunk := wordPositions[start:end]

		calls := make([]call3, len(chunk))
		for i, wp := range chunk {
			data, _ := inputArgs.Pack(wp)
			calls[i] = call3{
				Target:       s.poolAddr,
				AllowFailure: true,
				CallData:     append(tickBitmapSelector, data...),
			}
		}

		data, err := parsed.Pack("aggregate3", calls)
		if err != nil {
			return nil, fmt.Errorf("pack multicall: %w", err)
		}

		result, err := client.CallContract(s.ctx, ethereum.CallMsg{
			To:   &s.multicallAddr,
			Data: data,
		}, nil)
		if err != nil {
			if isConnectionError(err) {
				s.resetRPCClient()
			}
			return nil, maskError(fmt.Errorf("multicall aggregate3: %w", err))
		}

		unpacked, err := parsed.Unpack("aggregate3", result)
		if err != nil {
			return nil, fmt.Errorf("unpack multicall results: %w", err)
		}

		results, ok := unpacked[0].([]struct {
			Success    bool   `json:"success"`
			ReturnData []byte `json:"returnData"`
		})
		if !ok {
			return nil, fmt.Errorf("unpack multicall: unexpected result type %T", unpacked[0])
		}

		for i, r := range results {
			if !r.Success || len(r.ReturnData) == 0 {
				continue
			}
			bmUnpacked, err := bitmapArgs.Unpack(r.ReturnData)
			if err != nil {
				s.logger.Warn("multicall: unpack tickBitmap result failed",
					"wordPos", chunk[i], "error", err)
				continue
			}
			bm := bmUnpacked[0].(*big.Int)
			if bm.Sign() != 0 {
				bitmaps[chunk[i]] = bm
			}
		}
	}

	return bitmaps, nil
}

// FetchTickInfoBatch 通过 Multicall3 批量获取多个 tick 的链上数据。
// 一次 RPC 调用（自动分块）即可获取所有 tick 的 liquidityGross / liquidityNet / initialized。
func (s *Subscriber) FetchTickInfoBatch(ticks []int32) (map[int32]*TickData, error) {
	if len(ticks) == 0 {
		return nil, nil
	}

	client, done, err := s.rpcDialWithRetry()
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	defer done()

	// ticks(int24) 的 ABI 编码
	const tickABI = `[{"inputs":[{"internalType":"int24","name":"tick","type":"int24"}],"name":"ticks","outputs":[{"internalType":"uint128","name":"liquidityGross","type":"uint128"},{"internalType":"int128","name":"liquidityNet","type":"int128"},{"internalType":"uint256","name":"feeGrowthOutside0X128","type":"uint256"},{"internalType":"uint256","name":"feeGrowthOutside1X128","type":"uint256"},{"internalType":"int56","name":"tickCumulativeOutside","type":"int56"},{"internalType":"uint160","name":"secondsPerLiquidityOutsideX128","type":"uint160"},{"internalType":"uint32","name":"secondsOutside","type":"uint32"},{"internalType":"bool","name":"initialized","type":"bool"}],"stateMutability":"view","type":"function"}]`

	tickParsed, err := abi.JSON(strings.NewReader(tickABI))
	if err != nil {
		return nil, fmt.Errorf("parse ticks abi: %w", err)
	}

	// Multicall3 aggregate3 ABI
	const multicallABI = `[{"inputs":[{"components":[{"internalType":"address","name":"target","type":"address"},{"internalType":"bool","name":"allowFailure","type":"bool"},{"internalType":"bytes","name":"callData","type":"bytes"}],"internalType":"struct Multicall3.Call3[]","name":"calls","type":"tuple[]"}],"name":"aggregate3","outputs":[{"components":[{"internalType":"bool","name":"success","type":"bool"},{"internalType":"bytes","name":"returnData","type":"bytes"}],"internalType":"struct Multicall3.Result[]","name":"returnData","type":"tuple[]"}],"stateMutability":"payable","type":"function"}]`

	multicallParsed, err := abi.JSON(strings.NewReader(multicallABI))
	if err != nil {
		return nil, fmt.Errorf("parse multicall abi: %w", err)
	}

	type call3 struct {
		Target       common.Address `json:"target"`
		AllowFailure bool           `json:"allowFailure"`
		CallData     []byte         `json:"callData"`
	}

	result := make(map[int32]*TickData, len(ticks))

	// 分块执行
	for start := 0; start < len(ticks); start += maxMulticallBatchSize {
		end := start + maxMulticallBatchSize
		if end > len(ticks) {
			end = len(ticks)
		}
		chunk := ticks[start:end]

		calls := make([]call3, len(chunk))
		for i, t := range chunk {
			data, _ := tickParsed.Pack("ticks", big.NewInt(int64(t)))
			calls[i] = call3{
				Target:       s.poolAddr,
				AllowFailure: true,
				CallData:     data,
			}
		}

		data, err := multicallParsed.Pack("aggregate3", calls)
		if err != nil {
			return nil, fmt.Errorf("pack multicall: %w", err)
		}

		rawResult, err := client.CallContract(s.ctx, ethereum.CallMsg{
			To:   &s.multicallAddr,
			Data: data,
		}, nil)
		if err != nil {
			if isConnectionError(err) {
				s.resetRPCClient()
			}
			return nil, maskError(fmt.Errorf("multicall aggregate3: %w", err))
		}

		unpacked, err := multicallParsed.Unpack("aggregate3", rawResult)
		if err != nil {
			return nil, fmt.Errorf("unpack multicall results: %w", err)
		}

		results, ok := unpacked[0].([]struct {
			Success    bool   `json:"success"`
			ReturnData []byte `json:"returnData"`
		})
		if !ok {
			return nil, fmt.Errorf("unpack multicall: unexpected result type %T", unpacked[0])
		}

		for i, r := range results {
			if !r.Success || len(r.ReturnData) == 0 {
				continue
			}
			tickUnpacked, err := tickParsed.Unpack("ticks", r.ReturnData)
			if err != nil {
				s.logger.Warn("multicall: unpack ticks result failed",
					"tick", chunk[i], "error", err)
				continue
			}
			result[chunk[i]] = &TickData{
				LiquidityGross: tickUnpacked[0].(*big.Int),
				LiquidityNet:   tickUnpacked[1].(*big.Int),
				Initialized:    tickUnpacked[7].(bool),
			}
		}
	}

	return result, nil
}

func (s *Subscriber) FetchTickSpacing() (int32, error) {
	client, done, err := s.rpcDialWithRetry()
	if err != nil {
		return 0, fmt.Errorf("dial: %w", err)
	}
	defer done()

	const tsABI = `[{"inputs":[],"name":"tickSpacing","outputs":[{"internalType":"int24","name":"","type":"int24"}],"stateMutability":"view","type":"function"}]`
	parsed, _ := abi.JSON(strings.NewReader(tsABI))
	data, _ := parsed.Pack("tickSpacing")
	result, err := client.CallContract(s.ctx, ethereum.CallMsg{To: &s.poolAddr, Data: data}, nil)
	if err != nil {
		if isConnectionError(err) {
			s.resetRPCClient()
		}
		return 0, maskError(fmt.Errorf("call tickSpacing: %w", err))
	}
	unpacked, _ := parsed.Unpack("tickSpacing", result)
	return int32(unpacked[0].(*big.Int).Int64()), nil
}

func (s *Subscriber) processLog(vLog types.Log) {
	if len(vLog.Topics) == 0 {
		return
	}

	if s.isDuplicateLog(vLog) {
		return
	}
	poolAddr := s.poolAddr.Hex()
	switch vLog.Topics[0] {
	case pool.SwapEventSignature:
		event, err := pool.ParseSwapEvent(vLog)
		if err != nil {
			s.handler.OnError(fmt.Errorf("parse swap: %w", err))
			return
		}
		metrics.EventsTotal.WithLabelValues(poolAddr, "swap").Inc()
		s.handler.OnSwap(event)
	case pool.MintEventSignature:
		event, err := pool.ParseMintEvent(vLog)
		if err != nil {
			s.handler.OnError(fmt.Errorf("parse mint: %w", err))
			return
		}
		metrics.EventsTotal.WithLabelValues(poolAddr, "mint").Inc()
		s.handler.OnMint(event)
	case pool.BurnEventSignature:
		event, err := pool.ParseBurnEvent(vLog)
		if err != nil {
			s.handler.OnError(fmt.Errorf("parse burn: %w", err))
			return
		}
		metrics.EventsTotal.WithLabelValues(poolAddr, "burn").Inc()
		s.handler.OnBurn(event)
	}
}

// markConnected 标记一次成功连接。
// 返回 true 表示这是“重连成功”（而不是首次连接）。
func (s *Subscriber) markConnected() bool {
	reconnected := s.connectedOnce
	if reconnected {
		metrics.WSReconnectsTotal.WithLabelValues(s.poolAddr.Hex()).Inc()
	}
	s.connectedOnce = true
	return reconnected
}

// isDuplicateLog 判断日志是否重复（按 blockHash + txHash + logIndex 去重）。
func (s *Subscriber) isDuplicateLog(vLog types.Log) bool {
	// 对于没有链上定位信息的日志，无法安全去重，直接放行。
	if vLog.TxHash == (common.Hash{}) && vLog.BlockHash == (common.Hash{}) {
		return false
	}

	key := logDedupKey{
		BlockHash: vLog.BlockHash,
		TxHash:    vLog.TxHash,
		LogIndex:  vLog.Index,
	}
	if _, seen := s.seenLogKeys[key]; seen {
		return true
	}
	s.seenLogKeys[key] = struct{}{}

	// 限制 map 大小，超过 10000 条时清空。
	if len(s.seenLogKeys) > 10000 {
		s.seenLogKeys = make(map[logDedupKey]struct{})
	}
	return false
}

// ---- Uniswap V3 Factory ----

// FetchPoolFromFactory 通过 Uniswap V3 Factory 查询给定代币对+手续费的池子地址。
// factoryAddr 来自配置（chain.factory_address），支持多链。
func (s *Subscriber) FetchPoolFromFactory(factoryAddr common.Address, tokenA, tokenB common.Address, fee *big.Int) (common.Address, error) {
	client, done, err := s.rpcDialWithRetry()
	if err != nil {
		return common.Address{}, fmt.Errorf("dial: %w", err)
	}
	defer done()

	const factoryABI = `[{"inputs":[{"internalType":"address","name":"tokenA","type":"address"},{"internalType":"address","name":"tokenB","type":"address"},{"internalType":"uint24","name":"fee","type":"uint24"}],"name":"getPool","outputs":[{"internalType":"address","name":"pool","type":"address"}],"stateMutability":"view","type":"function"}]`

	parsed, err := abi.JSON(strings.NewReader(factoryABI))
	if err != nil {
		return common.Address{}, fmt.Errorf("parse factory abi: %w", err)
	}

	data, err := parsed.Pack("getPool", tokenA, tokenB, fee)
	if err != nil {
		return common.Address{}, fmt.Errorf("pack getPool: %w", err)
	}

	result, err := client.CallContract(s.ctx, ethereum.CallMsg{To: &factoryAddr, Data: data}, nil)
	if err != nil {
		return common.Address{}, maskError(fmt.Errorf("call getPool: %w", err))
	}

	unpacked, err := parsed.Unpack("getPool", result)
	if err != nil {
		return common.Address{}, fmt.Errorf("unpack getPool: %w", err)
	}

	return unpacked[0].(common.Address), nil
}
