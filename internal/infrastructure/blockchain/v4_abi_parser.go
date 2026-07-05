package blockchain

import (
	"fmt"
	"math/big"
	"strings"

	syncv4 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ4"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	topicV4Initialize       = crypto.Keccak256Hash([]byte("Initialize(bytes32,address,address,uint24,int24,address,uint160,int24)"))
	topicV4Swap             = crypto.Keccak256Hash([]byte("Swap(bytes32,address,int128,int128,uint160,uint128,int24,uint24)"))
	topicV4ModifyLiquidity  = crypto.Keccak256Hash([]byte("ModifyLiquidity(bytes32,address,int24,int24,int256,bytes32)"))
)

// V4ABIParser decodes Uniswap V4 PoolManager logs into domain events.
type V4ABIParser struct {
	managerABI abi.ABI
}

func NewV4ABIParser() (*V4ABIParser, error) {
	parsed, err := abi.JSON(strings.NewReader(poolManagerEventsABI))
	if err != nil {
		return nil, fmt.Errorf("parse pool manager events abi: %w", err)
	}
	return &V4ABIParser{managerABI: parsed}, nil
}

func V4PoolLogTopics() []common.Hash {
	return []common.Hash{topicV4Initialize, topicV4Swap, topicV4ModifyLiquidity}
}

func (p *V4ABIParser) ParsePoolEvents(logs []syncv4.RawLog) ([]marketv4.PoolEvent, error) {
	events := make([]marketv4.PoolEvent, 0, len(logs))
	for _, log := range logs {
		if len(log.Topics) == 0 {
			continue
		}
		event, err := p.parseLog(log)
		if err != nil {
			return nil, fmt.Errorf("parse log %d in block %d: %w", log.LogIndex, log.BlockNumber, err)
		}
		if event != nil {
			events = append(events, *event)
		}
	}
	return events, nil
}

func (p *V4ABIParser) parseLog(log syncv4.RawLog) (*marketv4.PoolEvent, error) {
	if len(log.Topics) < 2 {
		return nil, nil
	}
	meta := marketv4.EventMeta{
		PoolID:      marketv4.PoolID(log.Topics[1]),
		BlockNumber: log.BlockNumber,
		TxIndex:     log.TxIndex,
		LogIndex:    log.LogIndex,
	}

	switch log.Topics[0] {
	case topicV4Initialize:
		return p.parseInitialize(meta, log)
	case topicV4Swap:
		return p.parseSwap(meta, log)
	case topicV4ModifyLiquidity:
		return p.parseModifyLiquidity(meta, log)
	default:
		return nil, nil
	}
}

func (p *V4ABIParser) parseInitialize(meta marketv4.EventMeta, log syncv4.RawLog) (*marketv4.PoolEvent, error) {
	if len(log.Topics) < 4 {
		return nil, fmt.Errorf("initialize event missing indexed topics")
	}
	values, err := p.managerABI.Unpack("Initialize", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) < 5 {
		return nil, fmt.Errorf("initialize event has %d values", len(values))
	}
	sqrtPriceX96 := values[3].(*big.Int)
	tick, err := abiInt24ToInt32(values[4])
	if err != nil {
		return nil, err
	}
	event := marketv4.NewInitializeEvent(meta, sqrtPriceX96, tick)
	return &event, nil
}

func (p *V4ABIParser) parseSwap(meta marketv4.EventMeta, log syncv4.RawLog) (*marketv4.PoolEvent, error) {
	if len(log.Topics) < 3 {
		return nil, fmt.Errorf("swap event missing indexed topics")
	}
	sender := common.BytesToAddress(log.Topics[2].Bytes())

	values, err := p.managerABI.Unpack("Swap", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) < 6 {
		return nil, fmt.Errorf("swap event has %d values", len(values))
	}
	amount0, err := abiInt128ToBigInt(values[0])
	if err != nil {
		return nil, err
	}
	amount1, err := abiInt128ToBigInt(values[1])
	if err != nil {
		return nil, err
	}
	sqrtPriceX96 := values[2].(*big.Int)
	liquidity, err := abiUintToBigInt(values[3])
	if err != nil {
		return nil, err
	}
	tick, err := abiInt24ToInt32(values[4])
	if err != nil {
		return nil, err
	}
	fee, err := abiUintToBigInt(values[5])
	if err != nil {
		return nil, err
	}
	event := marketv4.NewSwapEvent(meta, sender, amount0, amount1, sqrtPriceX96, liquidity, tick, uint32(fee.Uint64()))
	return &event, nil
}

func (p *V4ABIParser) parseModifyLiquidity(meta marketv4.EventMeta, log syncv4.RawLog) (*marketv4.PoolEvent, error) {
	if len(log.Topics) < 3 {
		return nil, fmt.Errorf("modify liquidity event missing indexed topics")
	}
	sender := common.BytesToAddress(log.Topics[2].Bytes())

	values, err := p.managerABI.Unpack("ModifyLiquidity", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) < 4 {
		return nil, fmt.Errorf("modify liquidity event has %d values", len(values))
	}
	tickLower, err := abiInt24ToInt32(values[0])
	if err != nil {
		return nil, err
	}
	tickUpper, err := abiInt24ToInt32(values[1])
	if err != nil {
		return nil, err
	}
	liquidityDelta, ok := values[2].(*big.Int)
	if !ok || liquidityDelta == nil {
		return nil, fmt.Errorf("modify liquidity delta has unexpected type %T", values[2])
	}
	salt, ok := values[3].([32]byte)
	if !ok {
		return nil, fmt.Errorf("modify liquidity salt has unexpected type %T", values[3])
	}
	event := marketv4.NewModifyLiquidityEvent(meta, sender, tickLower, tickUpper, liquidityDelta, common.Hash(salt))
	return &event, nil
}

func abiInt128ToBigInt(value interface{}) (*big.Int, error) {
	switch v := value.(type) {
	case *big.Int:
		if v == nil {
			return nil, fmt.Errorf("nil big.Int")
		}
		return new(big.Int).Set(v), nil
	default:
		return nil, fmt.Errorf("unsupported int128 type %T", value)
	}
}

const poolManagerEventsABI = `[
  {"anonymous":false,"inputs":[{"indexed":true,"name":"id","type":"bytes32"},{"indexed":true,"name":"currency0","type":"address"},{"indexed":true,"name":"currency1","type":"address"},{"indexed":false,"name":"fee","type":"uint24"},{"indexed":false,"name":"tickSpacing","type":"int24"},{"indexed":false,"name":"hooks","type":"address"},{"indexed":false,"name":"sqrtPriceX96","type":"uint160"},{"indexed":false,"name":"tick","type":"int24"}],"name":"Initialize","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"id","type":"bytes32"},{"indexed":true,"name":"sender","type":"address"},{"indexed":false,"name":"amount0","type":"int128"},{"indexed":false,"name":"amount1","type":"int128"},{"indexed":false,"name":"sqrtPriceX96","type":"uint160"},{"indexed":false,"name":"liquidity","type":"uint128"},{"indexed":false,"name":"tick","type":"int24"},{"indexed":false,"name":"fee","type":"uint24"}],"name":"Swap","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"id","type":"bytes32"},{"indexed":true,"name":"sender","type":"address"},{"indexed":false,"name":"tickLower","type":"int24"},{"indexed":false,"name":"tickUpper","type":"int24"},{"indexed":false,"name":"liquidityDelta","type":"int256"},{"indexed":false,"name":"salt","type":"bytes32"}],"name":"ModifyLiquidity","type":"event"}
]`
