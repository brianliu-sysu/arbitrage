// Package subscriber 提供对 Uniswap V3 池子事件的实时订阅，内置 WebSocket 断线重连。
package subscriber

import (
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
	rpcMaxRetries           = 3
	rpcBaseBackoff          = 100 * time.Millisecond
	rpcMaxBackoff           = 1 * time.Second
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
	logger    logx.Logger
	wsURL     string // WebSocket 端点（已脱敏，用于日志；实际连接用 wsDial）
	rpcURL    string // HTTP RPC 端点（已脱敏，用于日志；实际连接用 rpcDial）
	wsDial    string // 真实 WebSocket URL（含 API key）
	rpcDial   string // 真实 HTTP RPC URL（含 API key）
	poolAddr  common.Address
	handler   EventHandler

	rpcClient     *ethclient.Client // 持久 HTTP 客户端（复用连接池）
	rpcClientMu   sync.Mutex
	rpcClientInit bool

	connectedOnce bool
	seenTxHashes  map[common.Hash]struct{} // WS 事件去重

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewSubscriber 创建事件订阅器。
//
// wsURL / rpcURL 中的 API key 会在存储时自动脱敏（日志中只显示 ***）。
// 真实 URL 保存在 wsDial / rpcDial，仅在拨号时使用。
func NewSubscriber(wsURL, rpcURL string, poolAddr common.Address, handler EventHandler, logger logx.Logger) (*Subscriber, error) {
	ctx, cancel := context.WithCancel(context.Background())
	return &Subscriber{
		seenTxHashes: make(map[common.Hash]struct{}),
		logger:   logger,
		wsURL:    maskAPIKey(wsURL),
		rpcURL:   maskAPIKey(rpcURL),
		wsDial:   wsURL,
		rpcDial:  rpcURL,
		poolAddr: poolAddr,
		handler:  handler,
		ctx:      ctx,
		cancel:   cancel,
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

		s.logger.Info("connected", "pool", s.poolAddr.Hex())
		backoff = initialReconnectBackoff

		if s.connectedOnce {
			metrics.WSReconnectsTotal.WithLabelValues(s.poolAddr.Hex()).Inc()
		}
		s.connectedOnce = true

		s.handler.OnReconnected()

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
	if isConnectionError(err) { s.resetRPCClient() }
	if err != nil {
		return nil, maskError(fmt.Errorf("call slot0: %w", err))
	}
	slot0Unpacked, _ := parsed.Unpack("slot0", slot0Result)
	sqrtPriceX96 := slot0Unpacked[0].(*big.Int)
	tick := int32(slot0Unpacked[1].(*big.Int).Int64())

	liqData, _ := parsed.Pack("liquidity")
	liqResult, err := client.CallContract(s.ctx, ethereum.CallMsg{To: &s.poolAddr, Data: liqData}, nil)
	if isConnectionError(err) { s.resetRPCClient() }
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

func (s *Subscriber) SimulateSwap(amountIn *big.Int, zeroForOne bool, sqrtPriceLimitX96 *big.Int) (*big.Int, error) {
	_, span := tracing.Tracer().Start(s.ctx, "subscriber.simulate_swap",
		trace.WithAttributes(
			attribute.String("pool", s.poolAddr.Hex()),
			attribute.String("amount_in", amountIn.String()),
			attribute.Bool("zero_for_one", zeroForOne),
		))
	defer span.End()

	client, done, err := s.rpcDialWithRetry()
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	defer done()

	const swapABI = `[{"inputs":[{"internalType":"address","name":"recipient","type":"address"},{"internalType":"bool","name":"zeroForOne","type":"bool"},{"internalType":"int256","name":"amountSpecified","type":"int256"},{"internalType":"uint160","name":"sqrtPriceLimitX96","type":"uint160"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"swap","outputs":[{"internalType":"int256","name":"amount0","type":"int256"},{"internalType":"int256","name":"amount1","type":"int256"}],"stateMutability":"nonpayable","type":"function"}]`

	parsed, _ := abi.JSON(strings.NewReader(swapABI))

	if sqrtPriceLimitX96 == nil {
		if zeroForOne {
			sqrtPriceLimitX96 = pool.MinSqrtRatio
		} else {
			sqrtPriceLimitX96 = pool.MaxSqrtRatio
		}
	}

	recipient := common.HexToAddress("0x0000000000000000000000000000000000000001")
	data, _ := parsed.Pack("swap", recipient, zeroForOne, amountIn, sqrtPriceLimitX96, []byte{})
	result, err := client.CallContract(s.ctx, ethereum.CallMsg{To: &s.poolAddr, Data: data}, nil)
	if err != nil {
		if isConnectionError(err) {
			s.resetRPCClient()
		}
		return nil, maskError(fmt.Errorf("call swap: %w", err))
	}
	unpacked, _ := parsed.Unpack("swap", result)
	amount0 := unpacked[0].(*big.Int)
	amount1 := unpacked[1].(*big.Int)

	if zeroForOne {
		return new(big.Int).Abs(amount1), nil
	}
	return new(big.Int).Abs(amount0), nil
}

// PoolMetadata 池子的静态元数据。
type PoolMetadata struct {
	Token0 common.Address
	Token1 common.Address
	Fee    uint32
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
	// P3-15: 按 txHash 去重（防止重复事件推送）
	if vLog.TxHash != (common.Hash{}) {
		if _, seen := s.seenTxHashes[vLog.TxHash]; seen {
			return
		}
		s.seenTxHashes[vLog.TxHash] = struct{}{}
		// 限制 map 大小，超过 10000 条时清空
		if len(s.seenTxHashes) > 10000 {
			s.seenTxHashes = make(map[common.Hash]struct{})
		}
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
