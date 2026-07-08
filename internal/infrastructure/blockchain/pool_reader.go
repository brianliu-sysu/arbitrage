package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// PoolReader loads on-chain Uniswap V3 pool state for bootstrap.
type PoolReader struct {
	client           *EthClient
	multicall        *Multicall
	poolABI          abi.ABI
	stateMethod      string
	tickBitmapMethod string
	feeFromState     bool
}

func NewPoolReader(client *EthClient, multicall *Multicall) (*PoolReader, error) {
	return newPoolReader(client, multicall, poolABIJSON)
}

// NewPancakePoolReader loads PancakeSwap V3 pool state using the Pancake slot0 ABI.
func NewPancakePoolReader(client *EthClient, multicall *Multicall) (*PoolReader, error) {
	return newPoolReader(client, multicall, pancakePoolABIJSON)
}

// NewQuickSwapPoolReader loads QuickSwap V3 pool state using the Algebra pool ABI.
func NewQuickSwapPoolReader(client *EthClient, multicall *Multicall) (*PoolReader, error) {
	return newPoolReaderWithOptions(client, multicall, algebraPoolABIJSON, poolReaderOptions{
		stateMethod:      "globalState",
		tickBitmapMethod: "tickTable",
		feeFromState:     true,
	})
}

type poolReaderOptions struct {
	stateMethod      string
	tickBitmapMethod string
	feeFromState     bool
}

func newPoolReader(client *EthClient, multicall *Multicall, abiJSON string) (*PoolReader, error) {
	return newPoolReaderWithOptions(client, multicall, abiJSON, poolReaderOptions{})
}

func newPoolReaderWithOptions(client *EthClient, multicall *Multicall, abiJSON string, options poolReaderOptions) (*PoolReader, error) {
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, fmt.Errorf("parse pool abi: %w", err)
	}
	if options.stateMethod == "" {
		options.stateMethod = "slot0"
	}
	if options.tickBitmapMethod == "" {
		options.tickBitmapMethod = "tickBitmap"
	}
	return &PoolReader{
		client:           client,
		multicall:        multicall,
		poolABI:          parsed,
		stateMethod:      options.stateMethod,
		tickBitmapMethod: options.tickBitmapMethod,
		feeFromState:     options.feeFromState,
	}, nil
}

func (r *PoolReader) ReadBootstrapData(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*marketclv3.BootstrapData, error) {
	baseResults, err := r.readBaseState(ctx, poolAddress, blockNumber)
	if err != nil {
		return nil, err
	}

	ticks, bitmap, err := r.readTickState(ctx, poolAddress, blockNumber, baseResults.tickSpacing)
	if err != nil {
		return nil, err
	}

	return &marketclv3.BootstrapData{
		Token0:      baseResults.token0,
		Token1:      baseResults.token1,
		Fee:         baseResults.fee,
		TickSpacing: baseResults.tickSpacing,
		State: market.PoolState{
			SqrtPriceX96:         baseResults.sqrtPriceX96,
			Tick:                 baseResults.tick,
			Liquidity:            baseResults.liquidity,
			FeeGrowthGlobal0X128: big.NewInt(0),
			FeeGrowthGlobal1X128: big.NewInt(0),
		},
		Ticks:  ticks,
		Bitmap: bitmap,
	}, nil
}

type basePoolState struct {
	token0       common.Address
	token1       common.Address
	fee          uint32
	tickSpacing  int32
	sqrtPriceX96 *big.Int
	tick         int32
	liquidity    *big.Int
}

func (r *PoolReader) readBaseState(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*basePoolState, error) {
	methods := []string{"token0", "token1", "tickSpacing", r.stateMethod, "liquidity"}
	if !r.feeFromState {
		methods = []string{"token0", "token1", "fee", "tickSpacing", r.stateMethod, "liquidity"}
	}
	results, err := r.callPoolMethods(ctx, poolAddress, blockNumber, methods)
	if err != nil {
		return nil, err
	}

	token0, err := unpackAddress(r.poolABI, "token0", results[0])
	if err != nil {
		return nil, fmt.Errorf("token0: %w", err)
	}
	token1, err := unpackAddress(r.poolABI, "token1", results[1])
	if err != nil {
		return nil, fmt.Errorf("token1: %w", err)
	}
	resultOffset := 0
	var fee uint32
	if !r.feeFromState {
		fee, err = unpackUint32(r.poolABI, "fee", results[2])
		if err != nil {
			return nil, fmt.Errorf("fee: %w", err)
		}
		resultOffset = 1
	}
	tickSpacing, err := unpackInt24(r.poolABI, "tickSpacing", results[2+resultOffset])
	if err != nil {
		return nil, fmt.Errorf("tickSpacing: %w", err)
	}
	slot0, err := unpackPoolState(r.poolABI, r.stateMethod, results[3+resultOffset])
	if err != nil {
		return nil, fmt.Errorf("%s: %w", r.stateMethod, err)
	}
	if r.feeFromState {
		fee = slot0.fee
	}
	liquidity, err := unpackBigInt(r.poolABI, "liquidity", results[4+resultOffset])
	if err != nil {
		return nil, fmt.Errorf("liquidity: %w", err)
	}

	return &basePoolState{
		token0:       token0,
		token1:       token1,
		fee:          fee,
		tickSpacing:  tickSpacing,
		sqrtPriceX96: slot0.sqrtPriceX96,
		tick:         slot0.tick,
		liquidity:    liquidity,
	}, nil
}

// ReadBaseState loads slot0 and liquidity for a V3-style pool at the given block.
// blockNumber 0 uses the latest head.
func (r *PoolReader) ReadBaseState(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*BasePoolState, error) {
	states, err := r.ReadManyBaseStates(ctx, []common.Address{poolAddress}, blockNumber)
	if err != nil {
		return nil, err
	}
	state, ok := states[poolAddress]
	if !ok || state == nil {
		return nil, fmt.Errorf("read base state for pool %s", poolAddress.Hex())
	}
	return state, nil
}

// ReadManyBaseStates loads slot0 and liquidity for many V3-style pools in batched multicall requests.
// blockNumber 0 uses the latest head. Missing or failed pools are omitted from the result map.
func (r *PoolReader) ReadManyBaseStates(
	ctx context.Context,
	poolAddresses []common.Address,
	blockNumber uint64,
) (map[common.Address]*BasePoolState, error) {
	if len(poolAddresses) == 0 {
		return map[common.Address]*BasePoolState{}, nil
	}

	blockNumber, err := r.client.ResolveBlockNumber(ctx, blockNumber)
	if err != nil {
		return nil, err
	}

	slot0Data, err := r.poolABI.Pack(r.stateMethod)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", r.stateMethod, err)
	}
	liquidityData, err := r.poolABI.Pack("liquidity")
	if err != nil {
		return nil, fmt.Errorf("pack liquidity: %w", err)
	}

	requests := make([]MulticallRequest, 0, len(poolAddresses)*2)
	for _, poolAddress := range poolAddresses {
		requests = append(requests,
			MulticallRequest{Target: poolAddress, Data: slot0Data},
			MulticallRequest{Target: poolAddress, Data: liquidityData},
		)
	}

	results, err := r.multicall.Aggregate3(ctx, requests, blockNumber)
	if err != nil {
		return nil, err
	}
	if len(results) != len(requests) {
		return nil, fmt.Errorf("expected %d pool call results, got %d", len(requests), len(results))
	}

	out := make(map[common.Address]*BasePoolState, len(poolAddresses))
	for i, poolAddress := range poolAddresses {
		slot0Result := results[i*2]
		liquidityResult := results[i*2+1]
		if !slot0Result.Success || !liquidityResult.Success {
			continue
		}

		slot0, err := unpackPoolState(r.poolABI, r.stateMethod, slot0Result)
		if err != nil {
			continue
		}
		liquidity, err := unpackBigInt(r.poolABI, "liquidity", liquidityResult)
		if err != nil {
			continue
		}

		out[poolAddress] = &BasePoolState{
			SqrtPriceX96: new(big.Int).Set(slot0.sqrtPriceX96),
			Tick:         slot0.tick,
			Liquidity:    new(big.Int).Set(liquidity),
		}
	}
	return out, nil
}

func (r *PoolReader) callPoolMethods(
	ctx context.Context,
	poolAddress common.Address,
	blockNumber uint64,
	methods []string,
) ([]MulticallResult, error) {
	calls := make([]MulticallRequest, 0, len(methods))
	for _, method := range methods {
		data, err := r.poolABI.Pack(method)
		if err != nil {
			return nil, fmt.Errorf("pack %s: %w", method, err)
		}
		calls = append(calls, MulticallRequest{Target: poolAddress, Data: data})
	}
	return r.multicall.Aggregate3WithDirectFallback(ctx, calls, blockNumber)
}

func needsDirectPoolFallback(results []MulticallResult) bool {
	for _, result := range results {
		if !result.Success || len(result.ReturnData) == 0 {
			return true
		}
	}
	return false
}

type slot0Values struct {
	sqrtPriceX96 *big.Int
	tick         int32
	fee          uint32
}

func unpackSlot0(poolABI abi.ABI, result MulticallResult) (slot0Values, error) {
	return unpackPoolState(poolABI, "slot0", result)
}

func unpackPoolState(poolABI abi.ABI, method string, result MulticallResult) (slot0Values, error) {
	values, err := unpackCallResult(poolABI, method, result)
	if err != nil {
		return slot0Values{}, err
	}
	if len(values) < 2 {
		return slot0Values{}, fmt.Errorf("%s returned %d values", method, len(values))
	}
	tick, err := abiInt24ToInt32(values[1])
	if err != nil {
		return slot0Values{}, err
	}
	var fee uint32
	if method == "globalState" && len(values) >= 3 {
		fee, err = abiUintToUint32(values[2])
		if err != nil {
			return slot0Values{}, err
		}
	}
	return slot0Values{
		sqrtPriceX96: values[0].(*big.Int),
		tick:         tick,
		fee:          fee,
	}, nil
}

func (r *PoolReader) readTickState(
	ctx context.Context,
	poolAddress common.Address,
	blockNumber uint64,
	tickSpacing int32,
) (market.TickTable, market.TickBitmap, error) {
	ticks := market.NewTickTable()
	bitmap := market.NewTickBitmap()

	if tickSpacing <= 0 {
		return ticks, bitmap, fmt.Errorf("invalid tick spacing %d", tickSpacing)
	}

	compressedMin := market.MinTick / tickSpacing
	if market.MinTick%tickSpacing != 0 && market.MinTick < 0 {
		compressedMin--
	}
	compressedMax := market.MaxTick / tickSpacing

	minWord := int16(compressedMin >> 8)
	maxWord := int16(compressedMax >> 8)

	bitmapWords, err := r.readTickBitmapWords(ctx, poolAddress, blockNumber, minWord, maxWord)
	if err != nil {
		return ticks, bitmap, err
	}

	initializedTicks := make([]int32, 0)
	for wordPos, word := range bitmapWords {
		bitmapFlipFromWord(&bitmap, wordPos, word, tickSpacing, &initializedTicks)
	}

	if len(initializedTicks) == 0 {
		return ticks, bitmap, nil
	}

	requests := make([]MulticallRequest, len(initializedTicks))
	for i, tick := range initializedTicks {
		data, err := r.poolABI.Pack("ticks", int32ToABIInt24(tick))
		if err != nil {
			return ticks, bitmap, fmt.Errorf("pack ticks: %w", err)
		}
		requests[i] = MulticallRequest{Target: poolAddress, Data: data}
	}

	results, err := r.multicall.Aggregate3(ctx, requests, blockNumber)
	if err != nil {
		return ticks, bitmap, err
	}

	for i, tick := range initializedTicks {
		returnData, err := r.tickReturnData(ctx, poolAddress, tick, blockNumber, results[i])
		if err != nil {
			return ticks, bitmap, fmt.Errorf("read tick %d: %w", tick, err)
		}
		values, err := r.poolABI.Unpack("ticks", returnData)
		if err != nil {
			return ticks, bitmap, fmt.Errorf("unpack tick %d: %w", tick, err)
		}
		liquidityGross := values[0].(*big.Int)
		liquidityNet := values[1].(*big.Int)
		if liquidityGross.Sign() == 0 {
			continue
		}
		tickState := ticks.GetOrCreate(tick)
		tickState.LiquidityGross = new(big.Int).Set(liquidityGross)
		tickState.LiquidityNet = new(big.Int).Set(liquidityNet)
	}

	return ticks, bitmap, nil
}

func (r *PoolReader) tickReturnData(
	ctx context.Context,
	poolAddress common.Address,
	tick int32,
	blockNumber uint64,
	result MulticallResult,
) ([]byte, error) {
	data, err := r.poolABI.Pack("ticks", int32ToABIInt24(tick))
	if err != nil {
		return nil, fmt.Errorf("pack ticks: %w", err)
	}
	return multicallReturnDataOrDirect(ctx, r.client, poolAddress, data, blockNumber, result)
}

func (r *PoolReader) readTickBitmapWords(
	ctx context.Context,
	poolAddress common.Address,
	blockNumber uint64,
	minWord, maxWord int16,
) (map[int16]*big.Int, error) {
	words := make(map[int16]*big.Int)
	if minWord > maxWord {
		return words, nil
	}

	requests := make([]MulticallRequest, 0, int(maxWord-minWord)+1)
	wordPositions := make([]int16, 0, int(maxWord-minWord)+1)
	for wordPos := minWord; wordPos <= maxWord; wordPos++ {
		data, err := r.poolABI.Pack(r.tickBitmapMethod, wordPos)
		if err != nil {
			return nil, fmt.Errorf("pack %s word %d: %w", r.tickBitmapMethod, wordPos, err)
		}
		requests = append(requests, MulticallRequest{Target: poolAddress, Data: data})
		wordPositions = append(wordPositions, wordPos)
	}

	results, err := r.multicall.Aggregate3(ctx, requests, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("load tickBitmap words: %w", err)
	}
	if len(results) != len(requests) {
		return nil, fmt.Errorf("expected %d tickBitmap results, got %d", len(requests), len(results))
	}

	for i, result := range results {
		wordPos := wordPositions[i]
		returnData, err := r.tickBitmapReturnData(ctx, poolAddress, wordPos, blockNumber, result)
		if err != nil {
			return nil, fmt.Errorf("read tickBitmap word %d: %w", wordPos, err)
		}
		values, err := r.poolABI.Unpack(r.tickBitmapMethod, returnData)
		if err != nil {
			return nil, fmt.Errorf("unpack %s word %d: %w", r.tickBitmapMethod, wordPos, err)
		}
		word := values[0].(*big.Int)
		if word.Sign() == 0 {
			continue
		}
		words[wordPos] = word
	}
	return words, nil
}

func (r *PoolReader) tickBitmapReturnData(
	ctx context.Context,
	poolAddress common.Address,
	wordPos int16,
	blockNumber uint64,
	result MulticallResult,
) ([]byte, error) {
	data, err := r.poolABI.Pack(r.tickBitmapMethod, wordPos)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", r.tickBitmapMethod, err)
	}
	return multicallReturnDataOrDirect(ctx, r.client, poolAddress, data, blockNumber, result)
}

func bitmapFlipFromWord(bitmap *market.TickBitmap, wordPos int16, word *big.Int, tickSpacing int32, initialized *[]int32) {
	for bit := 0; bit < 256; bit++ {
		if word.Bit(bit) != 1 {
			continue
		}
		compressed := int32(int(wordPos)*256 + bit)
		tick := compressed * tickSpacing
		if tick < market.MinTick || tick > market.MaxTick {
			continue
		}
		if err := bitmap.FlipTick(tick, tickSpacing); err == nil {
			*initialized = append(*initialized, tick)
		}
	}
}

func unpackCallResult(poolABI abi.ABI, method string, result MulticallResult) ([]interface{}, error) {
	if !result.Success {
		return nil, fmt.Errorf("call failed")
	}
	if len(result.ReturnData) == 0 {
		return nil, fmt.Errorf("returned empty data (RPC may be rate-limited or block state unavailable; retry or use another endpoint)")
	}
	values, err := poolABI.Unpack(method, result.ReturnData)
	if err != nil {
		return nil, err
	}
	return values, nil
}

func unpackAddress(poolABI abi.ABI, method string, result MulticallResult) (common.Address, error) {
	values, err := unpackCallResult(poolABI, method, result)
	if err != nil {
		return common.Address{}, err
	}
	address, ok := values[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("unexpected %s type %T", method, values[0])
	}
	return address, nil
}

func unpackUint32(poolABI abi.ABI, method string, result MulticallResult) (uint32, error) {
	values, err := unpackCallResult(poolABI, method, result)
	if err != nil {
		return 0, err
	}
	switch v := values[0].(type) {
	case uint32:
		return v, nil
	case *big.Int:
		return uint32(v.Uint64()), nil
	default:
		return 0, fmt.Errorf("unexpected %s type %T", method, values[0])
	}
}

func unpackInt24(poolABI abi.ABI, method string, result MulticallResult) (int32, error) {
	values, err := unpackCallResult(poolABI, method, result)
	if err != nil {
		return 0, err
	}
	return abiInt24ToInt32(values[0])
}

func unpackBigInt(poolABI abi.ABI, method string, result MulticallResult) (*big.Int, error) {
	values, err := unpackCallResult(poolABI, method, result)
	if err != nil {
		return nil, err
	}
	value, ok := values[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected %s type %T", method, values[0])
	}
	return new(big.Int).Set(value), nil
}
