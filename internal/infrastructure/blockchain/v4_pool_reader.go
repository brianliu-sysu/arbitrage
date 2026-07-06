package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	syncv4 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ4"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// V4PoolReader loads on-chain Uniswap V4 pool state for bootstrap.
type V4PoolReader struct {
	client    *EthClient
	multicall *Multicall
	stateView common.Address
	viewABI   abi.ABI
}

func NewV4PoolReader(client *EthClient, multicall *Multicall, stateView common.Address) (*V4PoolReader, error) {
	parsed, err := abi.JSON(strings.NewReader(stateViewABIJSON))
	if err != nil {
		return nil, fmt.Errorf("parse state view abi: %w", err)
	}
	return &V4PoolReader{
		client:    client,
		multicall: multicall,
		stateView: stateView,
		viewABI:   parsed,
	}, nil
}

func (r *V4PoolReader) ReadBootstrapData(
	ctx context.Context,
	poolID marketv4.PoolID,
	key marketv4.PoolKey,
	blockNumber uint64,
) (*syncv4.BootstrapData, error) {
	if blockNumber == 0 {
		header, err := r.client.GetLatestBlockHeader(ctx)
		if err != nil {
			return nil, err
		}
		blockNumber = header.Number
	}

	baseResults, err := r.readBaseState(ctx, poolID, blockNumber)
	if err != nil {
		return nil, err
	}

	ticks, bitmap, err := r.readTickState(ctx, poolID, blockNumber, key.TickSpacing)
	if err != nil {
		return nil, err
	}

	return &syncv4.BootstrapData{
		Key: key,
		State: market.PoolState{
			SqrtPriceX96:         baseResults.sqrtPriceX96,
			Tick:                 baseResults.tick,
			Liquidity:            baseResults.liquidity,
			FeeGrowthGlobal0X128: big.NewInt(0),
			FeeGrowthGlobal1X128: big.NewInt(0),
		},
		Ticks:       ticks,
		Bitmap:      bitmap,
		BlockNumber: blockNumber,
	}, nil
}

type v4BasePoolState struct {
	sqrtPriceX96 *big.Int
	tick         int32
	liquidity    *big.Int
}

func (r *V4PoolReader) readBaseState(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) (*v4BasePoolState, error) {
	slot0Data, err := r.viewABI.Pack("getSlot0", poolID.Hash())
	if err != nil {
		return nil, fmt.Errorf("pack getSlot0: %w", err)
	}
	liquidityData, err := r.viewABI.Pack("getLiquidity", poolID.Hash())
	if err != nil {
		return nil, fmt.Errorf("pack getLiquidity: %w", err)
	}

	results, err := r.callViewMethods(ctx, blockNumber, []MulticallRequest{
		{Target: r.stateView, Data: slot0Data},
		{Target: r.stateView, Data: liquidityData},
	})
	if err != nil {
		return nil, err
	}

	slot0Values, err := r.viewABI.Unpack("getSlot0", results[0].ReturnData)
	if err != nil {
		return nil, fmt.Errorf("unpack getSlot0: %w", err)
	}
	if len(slot0Values) < 2 {
		return nil, fmt.Errorf("getSlot0 returned %d values", len(slot0Values))
	}
	tick, err := abiInt24ToInt32(slot0Values[1])
	if err != nil {
		return nil, fmt.Errorf("getSlot0 tick: %w", err)
	}

	liquidityValues, err := r.viewABI.Unpack("getLiquidity", results[1].ReturnData)
	if err != nil {
		return nil, fmt.Errorf("unpack getLiquidity: %w", err)
	}
	liquidity, err := abiUintToBigInt(liquidityValues[0])
	if err != nil {
		return nil, fmt.Errorf("getLiquidity: %w", err)
	}

	return &v4BasePoolState{
		sqrtPriceX96: slot0Values[0].(*big.Int),
		tick:         tick,
		liquidity:    liquidity,
	}, nil
}

// ReadBaseState loads slot0 and liquidity for a V4 pool at the given block.
// blockNumber 0 uses the latest head.
func (r *V4PoolReader) ReadBaseState(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) (*BasePoolState, error) {
	if blockNumber == 0 {
		header, err := r.client.GetLatestBlockHeader(ctx)
		if err != nil {
			return nil, err
		}
		blockNumber = header.Number
	}
	state, err := r.readBaseState(ctx, poolID, blockNumber)
	if err != nil {
		return nil, err
	}
	return &BasePoolState{
		SqrtPriceX96: new(big.Int).Set(state.sqrtPriceX96),
		Tick:         state.tick,
		Liquidity:    new(big.Int).Set(state.liquidity),
	}, nil
}

// ReadPoolBaseState loads slot0 and liquidity for sync anchoring at a specific block.
func (r *V4PoolReader) ReadPoolBaseState(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) (market.PoolState, error) {
	base, err := r.ReadBaseState(ctx, poolID, blockNumber)
	if err != nil {
		return market.PoolState{}, err
	}
	return market.PoolState{
		SqrtPriceX96: new(big.Int).Set(base.SqrtPriceX96),
		Tick:         base.Tick,
		Liquidity:    new(big.Int).Set(base.Liquidity),
	}, nil
}

func (r *V4PoolReader) readTickState(
	ctx context.Context,
	poolID marketv4.PoolID,
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

	bitmapWords, err := r.readTickBitmapWords(ctx, poolID, blockNumber, minWord, maxWord)
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
		data, err := r.viewABI.Pack("getTickInfo", poolID.Hash(), int32ToABIInt24(tick))
		if err != nil {
			return ticks, bitmap, fmt.Errorf("pack getTickInfo: %w", err)
		}
		requests[i] = MulticallRequest{Target: r.stateView, Data: data}
	}

	results, err := r.callViewMethods(ctx, blockNumber, requests)
	if err != nil {
		return ticks, bitmap, err
	}

	for i, tick := range initializedTicks {
		returnData, err := r.tickInfoReturnData(ctx, tick, blockNumber, requests[i].Data, results[i])
		if err != nil {
			return ticks, bitmap, fmt.Errorf("read tick %d: %w", tick, err)
		}
		values, err := r.viewABI.Unpack("getTickInfo", returnData)
		if err != nil {
			return ticks, bitmap, fmt.Errorf("unpack tick %d: %w", tick, err)
		}
		liquidityGross, err := abiUintToBigInt(values[0])
		if err != nil {
			return ticks, bitmap, fmt.Errorf("tick %d gross: %w", tick, err)
		}
		liquidityNet, err := abiInt128ToBigInt(values[1])
		if err != nil {
			return ticks, bitmap, fmt.Errorf("tick %d net: %w", tick, err)
		}
		if liquidityGross.Sign() == 0 {
			continue
		}
		tickState := ticks.GetOrCreate(tick)
		tickState.LiquidityGross = new(big.Int).Set(liquidityGross)
		tickState.LiquidityNet = new(big.Int).Set(liquidityNet)
	}

	return ticks, bitmap, nil
}

func (r *V4PoolReader) readTickBitmapWords(
	ctx context.Context,
	poolID marketv4.PoolID,
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
		data, err := r.viewABI.Pack("getTickBitmap", poolID.Hash(), wordPos)
		if err != nil {
			return nil, fmt.Errorf("pack getTickBitmap word %d: %w", wordPos, err)
		}
		requests = append(requests, MulticallRequest{Target: r.stateView, Data: data})
		wordPositions = append(wordPositions, wordPos)
	}

	results, err := r.callViewMethods(ctx, blockNumber, requests)
	if err != nil {
		return nil, fmt.Errorf("load tickBitmap words: %w", err)
	}

	for i, result := range results {
		wordPos := wordPositions[i]
		returnData, err := r.tickBitmapReturnData(ctx, blockNumber, requests[i].Data, result)
		if err != nil {
			return nil, fmt.Errorf("read tickBitmap word %d: %w", wordPos, err)
		}
		values, err := r.viewABI.Unpack("getTickBitmap", returnData)
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

func (r *V4PoolReader) callViewMethods(
	ctx context.Context,
	blockNumber uint64,
	requests []MulticallRequest,
) ([]MulticallResult, error) {
	results, err := r.multicall.Aggregate3(ctx, requests, blockNumber)
	if err != nil {
		return r.directViewMethods(ctx, blockNumber, requests)
	}
	if needsDirectPoolFallback(results) {
		return r.directViewMethods(ctx, blockNumber, requests)
	}
	return results, nil
}

func (r *V4PoolReader) directViewMethods(
	ctx context.Context,
	blockNumber uint64,
	requests []MulticallRequest,
) ([]MulticallResult, error) {
	results := make([]MulticallResult, len(requests))
	for i, request := range requests {
		output, err := r.client.CallContract(ctx, request.Target, request.Data, blockNumber)
		if err != nil {
			return nil, fmt.Errorf("call state view: %w", err)
		}
		results[i] = MulticallResult{
			Success:    len(output) > 0,
			ReturnData: output,
		}
	}
	return results, nil
}

func (r *V4PoolReader) tickInfoReturnData(
	ctx context.Context,
	tick int32,
	blockNumber uint64,
	callData []byte,
	result MulticallResult,
) ([]byte, error) {
	if result.Success && len(result.ReturnData) > 0 {
		return result.ReturnData, nil
	}
	output, err := r.client.CallContract(ctx, r.stateView, callData, blockNumber)
	if err != nil {
		return nil, err
	}
	if len(output) == 0 {
		return nil, fmt.Errorf("tick %d returned empty data", tick)
	}
	return output, nil
}

func (r *V4PoolReader) tickBitmapReturnData(
	ctx context.Context,
	blockNumber uint64,
	callData []byte,
	result MulticallResult,
) ([]byte, error) {
	if result.Success && len(result.ReturnData) > 0 {
		return result.ReturnData, nil
	}
	output, err := r.client.CallContract(ctx, r.stateView, callData, blockNumber)
	if err != nil {
		return nil, err
	}
	if len(output) == 0 {
		return nil, fmt.Errorf("tickBitmap returned empty data")
	}
	return output, nil
}

const stateViewABIJSON = `[
  {"inputs":[{"name":"poolId","type":"bytes32"}],"name":"getSlot0","outputs":[{"type":"uint160"},{"type":"int24"},{"type":"uint24"},{"type":"uint24"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"poolId","type":"bytes32"}],"name":"getLiquidity","outputs":[{"type":"uint128"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"poolId","type":"bytes32"},{"name":"tick","type":"int16"}],"name":"getTickBitmap","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"poolId","type":"bytes32"},{"name":"tick","type":"int24"}],"name":"getTickInfo","outputs":[{"type":"uint128"},{"type":"int128"},{"type":"uint256"},{"type":"uint256"}],"stateMutability":"view","type":"function"}
]`
