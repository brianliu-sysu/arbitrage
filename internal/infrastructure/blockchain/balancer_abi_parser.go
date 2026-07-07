package blockchain

import (
	"fmt"
	"math/big"
	"strings"
	"sync"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	topicBalancerV2Swap               = crypto.Keccak256Hash([]byte("Swap(bytes32,address,address,uint256,uint256)"))
	topicBalancerV2PoolBalanceChanged = crypto.Keccak256Hash([]byte("PoolBalanceChanged(bytes32,address,address[],int256[],uint256[])"))
	topicBalancerV2PoolSwapFeeChanged = crypto.Keccak256Hash([]byte("SwapFeePercentageChanged(uint256)"))
	topicBalancerV2AmpUpdateStarted   = crypto.Keccak256Hash([]byte("AmpUpdateStarted(uint256,uint256,uint256,uint256)"))
	topicBalancerV2AmpUpdateStopped   = crypto.Keccak256Hash([]byte("AmpUpdateStopped(uint256)"))

	topicBalancerV3Swap             = crypto.Keccak256Hash([]byte("Swap(address,address,address,uint256,uint256,uint256,uint256)"))
	topicBalancerV3LiquidityAdded   = crypto.Keccak256Hash([]byte("LiquidityAdded(address,address,uint8,uint256,uint256[],uint256[])"))
	topicBalancerV3LiquidityRemoved = crypto.Keccak256Hash([]byte("LiquidityRemoved(address,address,uint8,uint256,uint256[],uint256[])"))
	topicBalancerV3SwapFeeChanged   = crypto.Keccak256Hash([]byte("SwapFeePercentageChanged(address,uint256)"))
	topicBalancerV3PoolPaused       = crypto.Keccak256Hash([]byte("PoolPausedStateChanged(address,bool)"))

	// Deprecated aliases kept for existing tests.
	topicBalancerSwap               = topicBalancerV2Swap
	topicBalancerPoolBalanceChanged = topicBalancerV2PoolBalanceChanged
	topicBalancerSwapFeeChanged     = topicBalancerV2PoolSwapFeeChanged
	topicBalancerAmpUpdateStopped   = topicBalancerV2AmpUpdateStopped
)

// BalancerABIParser decodes Balancer V2/V3 Vault and pool logs into domain events.
type BalancerABIParser struct {
	vaultV2ABI      abi.ABI
	vaultV3ABI      abi.ABI
	poolV2ABI       abi.ABI
	mu              sync.RWMutex
	poolIDByAddress map[common.Address]marketbalancer.PoolID
}

func NewBalancerABIParser() (*BalancerABIParser, error) {
	vaultV2ABI, err := abi.JSON(strings.NewReader(balancerVaultV2EventsABI))
	if err != nil {
		return nil, fmt.Errorf("parse balancer vault v2 events abi: %w", err)
	}
	vaultV3ABI, err := abi.JSON(strings.NewReader(balancerVaultV3EventsABI))
	if err != nil {
		return nil, fmt.Errorf("parse balancer vault v3 events abi: %w", err)
	}
	poolV2ABI, err := abi.JSON(strings.NewReader(balancerPoolV2EventsABI))
	if err != nil {
		return nil, fmt.Errorf("parse balancer pool v2 events abi: %w", err)
	}
	return &BalancerABIParser{
		vaultV2ABI:      vaultV2ABI,
		vaultV3ABI:      vaultV3ABI,
		poolV2ABI:       poolV2ABI,
		poolIDByAddress: make(map[common.Address]marketbalancer.PoolID),
	}, nil
}

func BalancerVaultV2LogTopics() []common.Hash {
	return []common.Hash{topicBalancerV2Swap, topicBalancerV2PoolBalanceChanged}
}

func BalancerVaultV3LogTopics() []common.Hash {
	return []common.Hash{
		topicBalancerV3Swap,
		topicBalancerV3LiquidityAdded,
		topicBalancerV3LiquidityRemoved,
		topicBalancerV3SwapFeeChanged,
		topicBalancerV3PoolPaused,
	}
}

func BalancerPoolV2LogTopics() []common.Hash {
	return []common.Hash{
		topicBalancerV2PoolSwapFeeChanged,
		topicBalancerV2AmpUpdateStarted,
		topicBalancerV2AmpUpdateStopped,
	}
}

func (p *BalancerABIParser) SetPoolAddressMap(poolIDByAddress map[common.Address]marketbalancer.PoolID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.poolIDByAddress = make(map[common.Address]marketbalancer.PoolID, len(poolIDByAddress))
	for address, poolID := range poolIDByAddress {
		p.poolIDByAddress[address] = poolID
	}
}

func (p *BalancerABIParser) ParsePoolEvents(logs []domainchain.RawLog) ([]marketbalancer.PoolEvent, error) {
	events := make([]marketbalancer.PoolEvent, 0, len(logs))
	for _, log := range logs {
		if len(log.Topics) == 0 {
			continue
		}
		event, err := p.parseLog(log)
		if err != nil {
			return nil, fmt.Errorf("parse balancer log %d in block %d: %w", log.LogIndex, log.BlockNumber, err)
		}
		if event != nil {
			events = append(events, *event)
		}
	}
	return events, nil
}

func (p *BalancerABIParser) parseLog(log domainchain.RawLog) (*marketbalancer.PoolEvent, error) {
	if len(log.Topics) == 0 {
		return nil, nil
	}
	switch log.Topics[0] {
	case topicBalancerV2Swap:
		return p.parseV2Swap(log)
	case topicBalancerV2PoolBalanceChanged:
		return p.parseV2PoolBalanceChanged(log)
	case topicBalancerV2PoolSwapFeeChanged:
		return p.parseV2SwapFeePercentageChanged(log)
	case topicBalancerV2AmpUpdateStarted:
		return p.parseV2AmpUpdateStarted(log)
	case topicBalancerV2AmpUpdateStopped:
		return p.parseV2AmpUpdateStopped(log)
	case topicBalancerV3Swap:
		return p.parseV3Swap(log)
	case topicBalancerV3LiquidityAdded:
		return p.parseV3LiquidityAdded(log)
	case topicBalancerV3LiquidityRemoved:
		return p.parseV3LiquidityRemoved(log)
	case topicBalancerV3SwapFeeChanged:
		return p.parseV3SwapFeePercentageChanged(log)
	case topicBalancerV3PoolPaused:
		return p.parseV3PoolPausedStateChanged(log)
	default:
		return nil, nil
	}
}

func (p *BalancerABIParser) parseV2Swap(log domainchain.RawLog) (*marketbalancer.PoolEvent, error) {
	if len(log.Topics) < 4 {
		return nil, nil
	}
	meta := marketbalancer.EventMeta{
		PoolID:      marketbalancer.PoolID(log.Topics[1]),
		BlockNumber: log.BlockNumber,
		TxIndex:     log.TxIndex,
		LogIndex:    log.LogIndex,
	}
	values, err := p.vaultV2ABI.Unpack("Swap", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) < 2 {
		return nil, fmt.Errorf("swap event has %d values", len(values))
	}
	amountIn, err := abiUintToBigInt(values[0])
	if err != nil {
		return nil, fmt.Errorf("amountIn: %w", err)
	}
	amountOut, err := abiUintToBigInt(values[1])
	if err != nil {
		return nil, fmt.Errorf("amountOut: %w", err)
	}
	event := marketbalancer.NewSwapEvent(
		meta,
		common.BytesToAddress(log.Topics[2].Bytes()),
		common.BytesToAddress(log.Topics[3].Bytes()),
		amountIn,
		amountOut,
	)
	return &event, nil
}

func (p *BalancerABIParser) parseV2PoolBalanceChanged(log domainchain.RawLog) (*marketbalancer.PoolEvent, error) {
	if len(log.Topics) < 2 {
		return nil, nil
	}
	meta := marketbalancer.EventMeta{
		PoolID:      marketbalancer.PoolID(log.Topics[1]),
		BlockNumber: log.BlockNumber,
		TxIndex:     log.TxIndex,
		LogIndex:    log.LogIndex,
	}
	values, err := p.vaultV2ABI.Unpack("PoolBalanceChanged", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) < 2 {
		return nil, fmt.Errorf("pool balance changed event has %d values", len(values))
	}
	tokens, ok := values[0].([]common.Address)
	if !ok {
		return nil, fmt.Errorf("tokens has unexpected type %T", values[0])
	}
	deltas, err := abiIntSliceToBigInts(values[1])
	if err != nil {
		return nil, fmt.Errorf("deltas: %w", err)
	}
	event := marketbalancer.NewPoolBalanceChangedEvent(meta, tokens, deltas)
	return &event, nil
}

func (p *BalancerABIParser) parseV2SwapFeePercentageChanged(log domainchain.RawLog) (*marketbalancer.PoolEvent, error) {
	meta, ok := p.metaFromPoolAddress(log)
	if !ok {
		return nil, nil
	}
	values, err := p.poolV2ABI.Unpack("SwapFeePercentageChanged", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) < 1 {
		return nil, fmt.Errorf("swap fee changed event has %d values", len(values))
	}
	swapFee, err := abiUintToBigInt(values[0])
	if err != nil {
		return nil, err
	}
	event := marketbalancer.NewSwapFeePercentageChangedEvent(meta, swapFee)
	return &event, nil
}

func (p *BalancerABIParser) parseV2AmpUpdateStarted(log domainchain.RawLog) (*marketbalancer.PoolEvent, error) {
	meta, ok := p.metaFromPoolAddress(log)
	if !ok {
		return nil, nil
	}
	values, err := p.poolV2ABI.Unpack("AmpUpdateStarted", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) < 2 {
		return nil, fmt.Errorf("amp update started event has %d values", len(values))
	}
	// Use endValue as the ramp target; exact intermediate values require timestamp interpolation.
	amp, err := abiUintToBigInt(values[1])
	if err != nil {
		return nil, err
	}
	event := marketbalancer.NewAmplificationUpdatedEvent(meta, amp)
	return &event, nil
}

func (p *BalancerABIParser) parseV2AmpUpdateStopped(log domainchain.RawLog) (*marketbalancer.PoolEvent, error) {
	meta, ok := p.metaFromPoolAddress(log)
	if !ok {
		return nil, nil
	}
	values, err := p.poolV2ABI.Unpack("AmpUpdateStopped", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) < 1 {
		return nil, fmt.Errorf("amp update stopped event has %d values", len(values))
	}
	amp, err := abiUintToBigInt(values[0])
	if err != nil {
		return nil, err
	}
	event := marketbalancer.NewAmplificationUpdatedEvent(meta, amp)
	return &event, nil
}

func (p *BalancerABIParser) parseV3Swap(log domainchain.RawLog) (*marketbalancer.PoolEvent, error) {
	meta, ok := p.metaFromPoolTopic(log, 1)
	if !ok {
		return nil, nil
	}
	if len(log.Topics) < 4 {
		return nil, fmt.Errorf("v3 swap event missing indexed topics")
	}
	values, err := p.vaultV3ABI.Unpack("Swap", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) < 2 {
		return nil, fmt.Errorf("v3 swap event has %d values", len(values))
	}
	amountIn, err := abiUintToBigInt(values[0])
	if err != nil {
		return nil, fmt.Errorf("amountIn: %w", err)
	}
	amountOut, err := abiUintToBigInt(values[1])
	if err != nil {
		return nil, fmt.Errorf("amountOut: %w", err)
	}
	event := marketbalancer.NewSwapEvent(
		meta,
		common.BytesToAddress(log.Topics[2].Bytes()),
		common.BytesToAddress(log.Topics[3].Bytes()),
		amountIn,
		amountOut,
	)
	return &event, nil
}

func (p *BalancerABIParser) parseV3LiquidityAdded(log domainchain.RawLog) (*marketbalancer.PoolEvent, error) {
	meta, ok := p.metaFromPoolTopic(log, 1)
	if !ok {
		return nil, nil
	}
	values, err := p.vaultV3ABI.Unpack("LiquidityAdded", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) < 2 {
		return nil, fmt.Errorf("liquidity added event has %d values", len(values))
	}
	amounts, err := abiUintSliceToBigInts(values[1])
	if err != nil {
		return nil, fmt.Errorf("amountsAddedRaw: %w", err)
	}
	event := marketbalancer.NewLiquidityAddedEvent(meta, amounts)
	return &event, nil
}

func (p *BalancerABIParser) parseV3LiquidityRemoved(log domainchain.RawLog) (*marketbalancer.PoolEvent, error) {
	meta, ok := p.metaFromPoolTopic(log, 1)
	if !ok {
		return nil, nil
	}
	values, err := p.vaultV3ABI.Unpack("LiquidityRemoved", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) < 2 {
		return nil, fmt.Errorf("liquidity removed event has %d values", len(values))
	}
	amounts, err := abiUintSliceToBigInts(values[1])
	if err != nil {
		return nil, fmt.Errorf("amountsRemovedRaw: %w", err)
	}
	event := marketbalancer.NewLiquidityRemovedEvent(meta, amounts)
	return &event, nil
}

func (p *BalancerABIParser) parseV3SwapFeePercentageChanged(log domainchain.RawLog) (*marketbalancer.PoolEvent, error) {
	meta, ok := p.metaFromPoolTopic(log, 1)
	if !ok {
		return nil, nil
	}
	values, err := p.vaultV3ABI.Unpack("SwapFeePercentageChanged", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) < 1 {
		return nil, fmt.Errorf("v3 swap fee changed event has %d values", len(values))
	}
	swapFee, err := abiUintToBigInt(values[0])
	if err != nil {
		return nil, err
	}
	event := marketbalancer.NewSwapFeePercentageChangedEvent(meta, swapFee)
	return &event, nil
}

func (p *BalancerABIParser) parseV3PoolPausedStateChanged(log domainchain.RawLog) (*marketbalancer.PoolEvent, error) {
	meta, ok := p.metaFromPoolTopic(log, 1)
	if !ok {
		return nil, nil
	}
	values, err := p.vaultV3ABI.Unpack("PoolPausedStateChanged", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) < 1 {
		return nil, fmt.Errorf("pool paused state changed event has %d values", len(values))
	}
	paused, ok := values[0].(bool)
	if !ok {
		return nil, fmt.Errorf("paused has unexpected type %T", values[0])
	}
	event := marketbalancer.NewPoolPausedStateChangedEvent(meta, paused)
	return &event, nil
}

func (p *BalancerABIParser) metaFromPoolAddress(log domainchain.RawLog) (marketbalancer.EventMeta, bool) {
	p.mu.RLock()
	poolID, ok := p.poolIDByAddress[log.Address]
	p.mu.RUnlock()
	if !ok {
		return marketbalancer.EventMeta{}, false
	}
	return marketbalancer.EventMeta{
		PoolID:      poolID,
		BlockNumber: log.BlockNumber,
		TxIndex:     log.TxIndex,
		LogIndex:    log.LogIndex,
	}, true
}

func (p *BalancerABIParser) metaFromPoolTopic(log domainchain.RawLog, topicIndex int) (marketbalancer.EventMeta, bool) {
	if len(log.Topics) <= topicIndex {
		return marketbalancer.EventMeta{}, false
	}
	poolAddress := common.BytesToAddress(log.Topics[topicIndex].Bytes())
	p.mu.RLock()
	poolID, ok := p.poolIDByAddress[poolAddress]
	p.mu.RUnlock()
	if !ok {
		poolID = marketbalancer.PoolID(common.HexToHash(poolAddress.Hex()))
	}
	return marketbalancer.EventMeta{
		PoolID:      poolID,
		BlockNumber: log.BlockNumber,
		TxIndex:     log.TxIndex,
		LogIndex:    log.LogIndex,
	}, true
}

func abiIntSliceToBigInts(value interface{}) ([]*big.Int, error) {
	values, ok := value.([]*big.Int)
	if !ok {
		return nil, fmt.Errorf("unsupported int slice type %T", value)
	}
	out := make([]*big.Int, len(values))
	for i, v := range values {
		if v == nil {
			return nil, fmt.Errorf("nil int at index %d", i)
		}
		out[i] = new(big.Int).Set(v)
	}
	return out, nil
}

func abiUintSliceToBigInts(value interface{}) ([]*big.Int, error) {
	raw, ok := value.([]*big.Int)
	if !ok {
		return nil, fmt.Errorf("unsupported uint slice type %T", value)
	}
	out := make([]*big.Int, len(raw))
	for i, v := range raw {
		if v == nil {
			return nil, fmt.Errorf("nil uint at index %d", i)
		}
		out[i] = new(big.Int).Set(v)
	}
	return out, nil
}

const balancerVaultV2EventsABI = `[
  {"anonymous":false,"inputs":[{"indexed":true,"name":"poolId","type":"bytes32"},{"indexed":true,"name":"tokenIn","type":"address"},{"indexed":true,"name":"tokenOut","type":"address"},{"indexed":false,"name":"amountIn","type":"uint256"},{"indexed":false,"name":"amountOut","type":"uint256"}],"name":"Swap","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"poolId","type":"bytes32"},{"indexed":true,"name":"liquidityProvider","type":"address"},{"indexed":false,"name":"tokens","type":"address[]"},{"indexed":false,"name":"deltas","type":"int256[]"},{"indexed":false,"name":"protocolFeeAmounts","type":"uint256[]"}],"name":"PoolBalanceChanged","type":"event"}
]`

const balancerVaultV3EventsABI = `[
  {"anonymous":false,"inputs":[{"indexed":true,"name":"pool","type":"address"},{"indexed":true,"name":"tokenIn","type":"address"},{"indexed":true,"name":"tokenOut","type":"address"},{"indexed":false,"name":"amountIn","type":"uint256"},{"indexed":false,"name":"amountOut","type":"uint256"},{"indexed":false,"name":"swapFeePercentage","type":"uint256"},{"indexed":false,"name":"swapFeeAmount","type":"uint256"}],"name":"Swap","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"pool","type":"address"},{"indexed":true,"name":"liquidityProvider","type":"address"},{"indexed":true,"name":"kind","type":"uint8"},{"indexed":false,"name":"totalSupply","type":"uint256"},{"indexed":false,"name":"amountsAddedRaw","type":"uint256[]"},{"indexed":false,"name":"swapFeeAmountsRaw","type":"uint256[]"}],"name":"LiquidityAdded","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"pool","type":"address"},{"indexed":true,"name":"liquidityProvider","type":"address"},{"indexed":true,"name":"kind","type":"uint8"},{"indexed":false,"name":"totalSupply","type":"uint256"},{"indexed":false,"name":"amountsRemovedRaw","type":"uint256[]"},{"indexed":false,"name":"swapFeeAmountsRaw","type":"uint256[]"}],"name":"LiquidityRemoved","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"pool","type":"address"},{"indexed":false,"name":"swapFeePercentage","type":"uint256"}],"name":"SwapFeePercentageChanged","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"pool","type":"address"},{"indexed":false,"name":"paused","type":"bool"}],"name":"PoolPausedStateChanged","type":"event"}
]`

const balancerPoolV2EventsABI = `[
  {"anonymous":false,"inputs":[{"indexed":false,"name":"swapFeePercentage","type":"uint256"}],"name":"SwapFeePercentageChanged","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":false,"name":"startValue","type":"uint256"},{"indexed":false,"name":"endValue","type":"uint256"},{"indexed":false,"name":"startTime","type":"uint256"},{"indexed":false,"name":"endTime","type":"uint256"}],"name":"AmpUpdateStarted","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":false,"name":"currentValue","type":"uint256"}],"name":"AmpUpdateStopped","type":"event"}
]`
