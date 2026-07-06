package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	clv3sync "github.com/brianliu-sysu/uniswapv3/internal/application/sync/clv3"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// PoolReader loads on-chain Uniswap V3 pool state for bootstrap.
type PoolReader struct {
	client    *EthClient
	multicall *Multicall
	poolABI   abi.ABI
}

func NewPoolReader(client *EthClient, multicall *Multicall) (*PoolReader, error) {
	return newPoolReader(client, multicall, poolABIJSON)
}

// NewPancakePoolReader loads PancakeSwap V3 pool state using the Pancake slot0 ABI.
func NewPancakePoolReader(client *EthClient, multicall *Multicall) (*PoolReader, error) {
	return newPoolReader(client, multicall, pancakePoolABIJSON)
}

func newPoolReader(client *EthClient, multicall *Multicall, abiJSON string) (*PoolReader, error) {
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, fmt.Errorf("parse pool abi: %w", err)
	}
	return &PoolReader{
		client:    client,
		multicall: multicall,
		poolABI:   parsed,
	}, nil
}

func (r *PoolReader) ReadBootstrapData(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*clv3sync.BootstrapData, error) {
	baseResults, err := r.readBaseState(ctx, poolAddress, blockNumber)
	if err != nil {
		return nil, err
	}

	ticks, bitmap, err := r.readTickState(ctx, poolAddress, blockNumber, baseResults.tickSpacing)
	if err != nil {
		return nil, err
	}

	return &clv3sync.BootstrapData{
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
	methods := []string{"token0", "token1", "fee", "tickSpacing", "slot0", "liquidity"}
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
	fee, err := unpackUint32(r.poolABI, "fee", results[2])
	if err != nil {
		return nil, fmt.Errorf("fee: %w", err)
	}
	tickSpacing, err := unpackInt24(r.poolABI, "tickSpacing", results[3])
	if err != nil {
		return nil, fmt.Errorf("tickSpacing: %w", err)
	}
	slot0, err := unpackSlot0(r.poolABI, results[4])
	if err != nil {
		return nil, fmt.Errorf("slot0: %w", err)
	}
	liquidity, err := unpackBigInt(r.poolABI, "liquidity", results[5])
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
	if blockNumber == 0 {
		header, err := r.client.GetLatestBlockHeader(ctx)
		if err != nil {
			return nil, err
		}
		blockNumber = header.Number
	}
	state, err := r.readBaseState(ctx, poolAddress, blockNumber)
	if err != nil {
		return nil, err
	}
	return &BasePoolState{
		SqrtPriceX96: new(big.Int).Set(state.sqrtPriceX96),
		Tick:         state.tick,
		Liquidity:    new(big.Int).Set(state.liquidity),
	}, nil
}

func (r *PoolReader) callPoolMethods(
	ctx context.Context,
	poolAddress common.Address,
	blockNumber uint64,
	methods []string,
) ([]MulticallResult, error) {
	results, err := r.multicallPoolMethods(ctx, poolAddress, blockNumber, methods)
	if err != nil {
		return r.directPoolMethods(ctx, poolAddress, blockNumber, methods)
	}
	if needsDirectPoolFallback(results) {
		return r.directPoolMethods(ctx, poolAddress, blockNumber, methods)
	}
	return results, nil
}

func (r *PoolReader) multicallPoolMethods(
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

	results, err := r.multicall.Aggregate3(ctx, calls, blockNumber)
	if err != nil {
		return nil, err
	}
	if len(results) != len(calls) {
		return nil, fmt.Errorf("expected %d pool call results, got %d", len(calls), len(results))
	}
	return results, nil
}

func (r *PoolReader) directPoolMethods(
	ctx context.Context,
	poolAddress common.Address,
	blockNumber uint64,
	methods []string,
) ([]MulticallResult, error) {
	results := make([]MulticallResult, len(methods))
	for i, method := range methods {
		data, err := r.poolABI.Pack(method)
		if err != nil {
			return nil, fmt.Errorf("pack %s: %w", method, err)
		}
		output, err := r.client.CallContract(ctx, poolAddress, data, blockNumber)
		if err != nil {
			return nil, fmt.Errorf("call %s: %w", method, err)
		}
		results[i] = MulticallResult{
			Success:    len(output) > 0,
			ReturnData: output,
		}
	}
	return results, nil
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
}

func unpackSlot0(poolABI abi.ABI, result MulticallResult) (slot0Values, error) {
	values, err := unpackCallResult(poolABI, "slot0", result)
	if err != nil {
		return slot0Values{}, err
	}
	if len(values) < 2 {
		return slot0Values{}, fmt.Errorf("slot0 returned %d values", len(values))
	}
	tick, err := abiInt24ToInt32(values[1])
	if err != nil {
		return slot0Values{}, err
	}
	return slot0Values{
		sqrtPriceX96: values[0].(*big.Int),
		tick:         tick,
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
	if result.Success && len(result.ReturnData) > 0 {
		return result.ReturnData, nil
	}

	data, err := r.poolABI.Pack("ticks", int32ToABIInt24(tick))
	if err != nil {
		return nil, fmt.Errorf("pack ticks: %w", err)
	}
	output, err := r.client.CallContract(ctx, poolAddress, data, blockNumber)
	if err != nil {
		return nil, err
	}
	if len(output) == 0 {
		return nil, fmt.Errorf("returned empty data")
	}
	return output, nil
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
		data, err := r.poolABI.Pack("tickBitmap", wordPos)
		if err != nil {
			return nil, fmt.Errorf("pack tickBitmap word %d: %w", wordPos, err)
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
		values, err := r.poolABI.Unpack("tickBitmap", returnData)
		if err != nil {
			return nil, fmt.Errorf("unpack tickBitmap word %d: %w", wordPos, err)
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
	if result.Success && len(result.ReturnData) > 0 {
		return result.ReturnData, nil
	}

	data, err := r.poolABI.Pack("tickBitmap", wordPos)
	if err != nil {
		return nil, fmt.Errorf("pack tickBitmap: %w", err)
	}
	output, err := r.client.CallContract(ctx, poolAddress, data, blockNumber)
	if err != nil {
		return nil, err
	}
	if len(output) == 0 {
		return nil, fmt.Errorf("returned empty data")
	}
	return output, nil
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
