package blockchain

import (
	"fmt"
	"math/big"
	"strings"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v3"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	topicInitialize = crypto.Keccak256Hash([]byte("Initialize(uint160,int24)"))
	topicSwap       = crypto.Keccak256Hash([]byte("Swap(address,address,int256,int256,uint160,uint128,int24)"))
	topicMint       = crypto.Keccak256Hash([]byte("Mint(address,address,int24,int24,uint128,uint256,uint256)"))
	topicBurn       = crypto.Keccak256Hash([]byte("Burn(address,int24,int24,uint128,uint256,uint256)"))
)

// ABIParser decodes Uniswap V3 pool logs into domain events.
type ABIParser struct {
	poolABI abi.ABI
}

func NewABIParser() (*ABIParser, error) {
	parsed, err := abi.JSON(strings.NewReader(poolABIJSON))
	if err != nil {
		return nil, fmt.Errorf("parse pool abi: %w", err)
	}
	return &ABIParser{poolABI: parsed}, nil
}

func PoolLogTopics() []common.Hash {
	return []common.Hash{topicInitialize, topicSwap, topicMint, topicBurn}
}

func (p *ABIParser) ParsePoolEvents(logs []syncapp.RawLog) ([]marketv3.PoolEvent, error) {
	events := make([]marketv3.PoolEvent, 0, len(logs))
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

func (p *ABIParser) parseLog(log syncapp.RawLog) (*marketv3.PoolEvent, error) {
	meta := marketv3.EventMeta{
		PoolAddress: log.Address,
		BlockNumber: log.BlockNumber,
		TxIndex:     log.TxIndex,
		LogIndex:    log.LogIndex,
	}

	switch log.Topics[0] {
	case topicInitialize:
		return p.parseInitialize(meta, log)
	case topicSwap:
		return p.parseSwap(meta, log)
	case topicMint:
		return p.parseMint(meta, log)
	case topicBurn:
		return p.parseBurn(meta, log)
	default:
		return nil, nil
	}
}

func (p *ABIParser) parseInitialize(meta marketv3.EventMeta, log syncapp.RawLog) (*marketv3.PoolEvent, error) {
	values, err := p.poolABI.Unpack("Initialize", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) != 2 {
		return nil, fmt.Errorf("initialize event has %d values", len(values))
	}
	sqrtPriceX96 := values[0].(*big.Int)
	tick, err := abiInt24ToInt32(values[1])
	if err != nil {
		return nil, err
	}
	event := marketv3.NewInitializeEvent(meta, sqrtPriceX96, tick)
	return &event, nil
}

func (p *ABIParser) parseSwap(meta marketv3.EventMeta, log syncapp.RawLog) (*marketv3.PoolEvent, error) {
	if len(log.Topics) < 3 {
		return nil, fmt.Errorf("swap event missing indexed topics")
	}
	sender := common.BytesToAddress(log.Topics[1].Bytes())
	recipient := common.BytesToAddress(log.Topics[2].Bytes())

	values, err := p.poolABI.Unpack("Swap", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) != 5 {
		return nil, fmt.Errorf("swap event has %d values", len(values))
	}
	amount0 := values[0].(*big.Int)
	amount1 := values[1].(*big.Int)
	sqrtPriceX96 := values[2].(*big.Int)
	liquidity := values[3].(*big.Int)
	tick, err := abiInt24ToInt32(values[4])
	if err != nil {
		return nil, err
	}
	event := marketv3.NewSwapEvent(meta, sender, recipient, amount0, amount1, sqrtPriceX96, liquidity, tick)
	return &event, nil
}

func (p *ABIParser) parseMint(meta marketv3.EventMeta, log syncapp.RawLog) (*marketv3.PoolEvent, error) {
	if len(log.Topics) < 4 {
		return nil, fmt.Errorf("mint event missing indexed topics")
	}
	owner := common.BytesToAddress(log.Topics[1].Bytes())
	tickLower, err := topicToInt24(log.Topics[2])
	if err != nil {
		return nil, err
	}
	tickUpper, err := topicToInt24(log.Topics[3])
	if err != nil {
		return nil, err
	}

	values, err := p.poolABI.Unpack("Mint", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) != 4 {
		return nil, fmt.Errorf("mint event has %d values", len(values))
	}
	sender := values[0].(common.Address)
	amount, err := abiUintToBigInt(values[1])
	if err != nil {
		return nil, fmt.Errorf("mint amount: %w", err)
	}
	if amount.Sign() <= 0 {
		return nil, nil
	}
	amount0, err := abiUintToBigInt(values[2])
	if err != nil {
		return nil, fmt.Errorf("mint amount0: %w", err)
	}
	amount1, err := abiUintToBigInt(values[3])
	if err != nil {
		return nil, fmt.Errorf("mint amount1: %w", err)
	}

	event := marketv3.NewMintEvent(meta, sender, owner, tickLower, tickUpper, amount, amount0, amount1)
	return &event, nil
}

func (p *ABIParser) parseBurn(meta marketv3.EventMeta, log syncapp.RawLog) (*marketv3.PoolEvent, error) {
	if len(log.Topics) < 4 {
		return nil, fmt.Errorf("burn event missing indexed topics")
	}
	owner := common.BytesToAddress(log.Topics[1].Bytes())
	tickLower, err := topicToInt24(log.Topics[2])
	if err != nil {
		return nil, err
	}
	tickUpper, err := topicToInt24(log.Topics[3])
	if err != nil {
		return nil, err
	}

	values, err := p.poolABI.Unpack("Burn", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) != 3 {
		return nil, fmt.Errorf("burn event has %d values", len(values))
	}
	amount, err := abiUintToBigInt(values[0])
	if err != nil {
		return nil, fmt.Errorf("burn amount: %w", err)
	}
	if amount.Sign() <= 0 {
		return nil, nil
	}
	amount0, err := abiUintToBigInt(values[1])
	if err != nil {
		return nil, fmt.Errorf("burn amount0: %w", err)
	}
	amount1, err := abiUintToBigInt(values[2])
	if err != nil {
		return nil, fmt.Errorf("burn amount1: %w", err)
	}

	event := marketv3.NewBurnEvent(meta, owner, tickLower, tickUpper, amount, amount0, amount1)
	return &event, nil
}

func topicToInt24(topic common.Hash) (int32, error) {
	return abiInt24ToInt32(new(big.Int).SetBytes(topic.Bytes()))
}

func int32ToABIInt24(value int32) *big.Int {
	return big.NewInt(int64(value))
}

func abiUintToBigInt(value interface{}) (*big.Int, error) {
	switch v := value.(type) {
	case *big.Int:
		if v == nil {
			return nil, fmt.Errorf("nil big.Int")
		}
		return new(big.Int).Set(v), nil
	case uint64:
		return new(big.Int).SetUint64(v), nil
	case uint32:
		return new(big.Int).SetUint64(uint64(v)), nil
	case uint16:
		return new(big.Int).SetUint64(uint64(v)), nil
	case uint8:
		return new(big.Int).SetUint64(uint64(v)), nil
	default:
		return nil, fmt.Errorf("unsupported uint type %T", value)
	}
}

func abiInt24ToInt32(value interface{}) (int32, error) {
	switch v := value.(type) {
	case int32:
		return v, nil
	case int64:
		return int32(v), nil
	case *big.Int:
		return bigIntToInt24(v)
	default:
		return 0, fmt.Errorf("unsupported int24 type %T", value)
	}
}

func bigIntToInt24(value *big.Int) (int32, error) {
	minInt24 := big.NewInt(-1 << 23)
	maxInt24 := big.NewInt(1<<23 - 1)

	working := new(big.Int).Set(value)
	if working.Cmp(minInt24) < 0 || working.Cmp(maxInt24) > 0 {
		two256 := new(big.Int).Lsh(big.NewInt(1), 256)
		if working.Bit(255) == 1 {
			working.Sub(working, two256)
		}
	}
	if working.Cmp(minInt24) < 0 || working.Cmp(maxInt24) > 0 {
		return 0, fmt.Errorf("value %s out of int24 range", value.String())
	}
	return int32(working.Int64()), nil
}
