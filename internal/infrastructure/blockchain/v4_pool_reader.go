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
	blockNumber, err := r.client.ResolveBlockNumber(ctx, blockNumber)
	if err != nil {
		return nil, err
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
	states, err := r.ReadManyBaseStates(ctx, []marketv4.PoolID{poolID}, blockNumber)
	if err != nil {
		return nil, err
	}
	state, ok := states[poolID]
	if !ok || state == nil {
		return nil, fmt.Errorf("read base state for pool %s", poolID.String())
	}
	return state, nil
}

// ReadManyBaseStates loads slot0 and liquidity for many V4 pools in batched multicall requests.
// blockNumber 0 uses the latest head. Missing or failed pools are omitted from the result map.
func (r *V4PoolReader) ReadManyBaseStates(
	ctx context.Context,
	poolIDs []marketv4.PoolID,
	blockNumber uint64,
) (map[marketv4.PoolID]*BasePoolState, error) {
	if len(poolIDs) == 0 {
		return map[marketv4.PoolID]*BasePoolState{}, nil
	}

	blockNumber, err := r.client.ResolveBlockNumber(ctx, blockNumber)
	if err != nil {
		return nil, err
	}

	requests := make([]MulticallRequest, 0, len(poolIDs)*2)
	for _, poolID := range poolIDs {
		slot0Data, err := r.viewABI.Pack("getSlot0", poolID.Hash())
		if err != nil {
			return nil, fmt.Errorf("pack getSlot0: %w", err)
		}
		liquidityData, err := r.viewABI.Pack("getLiquidity", poolID.Hash())
		if err != nil {
			return nil, fmt.Errorf("pack getLiquidity: %w", err)
		}
		requests = append(requests,
			MulticallRequest{Target: r.stateView, Data: slot0Data},
			MulticallRequest{Target: r.stateView, Data: liquidityData},
		)
	}

	results, err := r.multicall.Aggregate3(ctx, requests, blockNumber)
	if err != nil {
		return nil, err
	}
	if len(results) != len(requests) {
		return nil, fmt.Errorf("expected %d v4 call results, got %d", len(requests), len(results))
	}

	out := make(map[marketv4.PoolID]*BasePoolState, len(poolIDs))
	for i, poolID := range poolIDs {
		slot0Result := results[i*2]
		liquidityResult := results[i*2+1]
		if !slot0Result.Success || !liquidityResult.Success {
			continue
		}

		slot0Values, err := r.viewABI.Unpack("getSlot0", slot0Result.ReturnData)
		if err != nil || len(slot0Values) < 2 {
			continue
		}
		tick, err := abiInt24ToInt32(slot0Values[1])
		if err != nil {
			continue
		}
		liquidityValues, err := r.viewABI.Unpack("getLiquidity", liquidityResult.ReturnData)
		if err != nil || len(liquidityValues) < 1 {
			continue
		}
		liquidity, err := abiUintToBigInt(liquidityValues[0])
		if err != nil {
			continue
		}

		out[poolID] = &BasePoolState{
			SqrtPriceX96: new(big.Int).Set(slot0Values[0].(*big.Int)),
			Tick:         tick,
			Liquidity:    new(big.Int).Set(liquidity),
		}
	}
	return out, nil
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
	return r.multicall.Aggregate3WithDirectFallback(ctx, requests, blockNumber)
}

func (r *V4PoolReader) tickInfoReturnData(
	ctx context.Context,
	tick int32,
	blockNumber uint64,
	callData []byte,
	result MulticallResult,
) ([]byte, error) {
	returnData, err := multicallReturnDataOrDirect(ctx, r.client, r.stateView, callData, blockNumber, result)
	if err != nil {
		return nil, fmt.Errorf("tick %d: %w", tick, err)
	}
	return returnData, nil
}

func (r *V4PoolReader) tickBitmapReturnData(
	ctx context.Context,
	blockNumber uint64,
	callData []byte,
	result MulticallResult,
) ([]byte, error) {
	return multicallReturnDataOrDirect(ctx, r.client, r.stateView, callData, blockNumber, result)
}

const stateViewABIJSON = `[
  {"inputs":[{"name":"poolId","type":"bytes32"}],"name":"getSlot0","outputs":[{"type":"uint160"},{"type":"int24"},{"type":"uint24"},{"type":"uint24"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"poolId","type":"bytes32"}],"name":"getLiquidity","outputs":[{"type":"uint128"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"poolId","type":"bytes32"},{"name":"tick","type":"int16"}],"name":"getTickBitmap","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"poolId","type":"bytes32"},{"name":"tick","type":"int24"}],"name":"getTickInfo","outputs":[{"type":"uint128"},{"type":"int128"},{"type":"uint256"},{"type":"uint256"}],"stateMutability":"view","type":"function"}
]`
