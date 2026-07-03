package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
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
	parsed, err := abi.JSON(strings.NewReader(poolABIJSON))
	if err != nil {
		return nil, fmt.Errorf("parse pool abi: %w", err)
	}
	return &PoolReader{
		client:    client,
		multicall: multicall,
		poolABI:   parsed,
	}, nil
}

func (r *PoolReader) ReadBootstrapData(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*syncapp.BootstrapData, error) {
	baseResults, err := r.readBaseState(ctx, poolAddress, blockNumber)
	if err != nil {
		return nil, err
	}

	ticks, bitmap, err := r.readTickState(ctx, poolAddress, blockNumber, baseResults.tickSpacing)
	if err != nil {
		return nil, err
	}

	return &syncapp.BootstrapData{
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
	calls := make([]MulticallRequest, 0, 6)
	for _, method := range []string{"token0", "token1", "fee", "tickSpacing", "slot0", "liquidity"} {
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
		return nil, fmt.Errorf("expected %d base results, got %d", len(calls), len(results))
	}

	token0, err := unpackAddress(r.poolABI, "token0", results[0])
	if err != nil {
		return nil, err
	}
	token1, err := unpackAddress(r.poolABI, "token1", results[1])
	if err != nil {
		return nil, err
	}
	fee, err := unpackUint32(r.poolABI, "fee", results[2])
	if err != nil {
		return nil, err
	}
	tickSpacing, err := unpackInt24(r.poolABI, "tickSpacing", results[3])
	if err != nil {
		return nil, err
	}
	slot0, err := unpackSlot0(r.poolABI, results[4])
	if err != nil {
		return nil, err
	}
	liquidity, err := unpackBigInt(r.poolABI, "liquidity", results[5])
	if err != nil {
		return nil, err
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

type slot0Values struct {
	sqrtPriceX96 *big.Int
	tick         int32
}

func unpackSlot0(poolABI abi.ABI, result MulticallResult) (slot0Values, error) {
	if !result.Success {
		return slot0Values{}, fmt.Errorf("slot0 call failed")
	}
	values, err := poolABI.Unpack("slot0", result.ReturnData)
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

	initializedTicks := make([]int32, 0)
	for wordPos := minWord; wordPos <= maxWord; wordPos++ {
		data, err := r.poolABI.Pack("tickBitmap", wordPos)
		if err != nil {
			return ticks, bitmap, fmt.Errorf("pack tickBitmap: %w", err)
		}
		output, err := r.client.CallContract(ctx, poolAddress, data, blockNumber)
		if err != nil {
			return ticks, bitmap, fmt.Errorf("call tickBitmap: %w", err)
		}
		values, err := r.poolABI.Unpack("tickBitmap", output)
		if err != nil {
			return ticks, bitmap, fmt.Errorf("unpack tickBitmap: %w", err)
		}
		word := values[0].(*big.Int)
		if word.Sign() == 0 {
			continue
		}
		bitmapFlipFromWord(&bitmap, wordPos, word, tickSpacing, &initializedTicks)
	}

	if len(initializedTicks) == 0 {
		return ticks, bitmap, nil
	}

	requests := make([]MulticallRequest, len(initializedTicks))
	for i, tick := range initializedTicks {
		data, err := r.poolABI.Pack("ticks", tick)
		if err != nil {
			return ticks, bitmap, fmt.Errorf("pack ticks: %w", err)
		}
		requests[i] = MulticallRequest{Target: poolAddress, Data: data}
	}

	results, err := r.multicall.Aggregate3(ctx, requests, blockNumber)
	if err != nil {
		return ticks, bitmap, err
	}

	for i, result := range results {
		if !result.Success {
			continue
		}
		values, err := r.poolABI.Unpack("ticks", result.ReturnData)
		if err != nil {
			return ticks, bitmap, fmt.Errorf("unpack ticks: %w", err)
		}
		liquidityGross := values[0].(*big.Int)
		liquidityNet := values[1].(*big.Int)
		if liquidityGross.Sign() == 0 {
			continue
		}
		tick := ticks.GetOrCreate(initializedTicks[i])
		tick.LiquidityGross = new(big.Int).Set(liquidityGross)
		tick.LiquidityNet = new(big.Int).Set(liquidityNet)
	}

	return ticks, bitmap, nil
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

func unpackAddress(poolABI abi.ABI, method string, result MulticallResult) (common.Address, error) {
	if !result.Success {
		return common.Address{}, fmt.Errorf("%s call failed", method)
	}
	values, err := poolABI.Unpack(method, result.ReturnData)
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
	if !result.Success {
		return 0, fmt.Errorf("%s call failed", method)
	}
	values, err := poolABI.Unpack(method, result.ReturnData)
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
	if !result.Success {
		return 0, fmt.Errorf("%s call failed", method)
	}
	values, err := poolABI.Unpack(method, result.ReturnData)
	if err != nil {
		return 0, err
	}
	return abiInt24ToInt32(values[0])
}

func unpackBigInt(poolABI abi.ABI, method string, result MulticallResult) (*big.Int, error) {
	if !result.Success {
		return nil, fmt.Errorf("%s call failed", method)
	}
	values, err := poolABI.Unpack(method, result.ReturnData)
	if err != nil {
		return nil, err
	}
	value, ok := values[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected %s type %T", method, values[0])
	}
	return new(big.Int).Set(value), nil
}
