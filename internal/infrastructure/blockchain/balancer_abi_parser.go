package blockchain

import (
	"fmt"
	"math/big"
	"strings"
	"sync"

	syncbalancer "github.com/brianliu-sysu/uniswapv3/internal/application/sync/balancer"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	topicBalancerSwap               = crypto.Keccak256Hash([]byte("Swap(bytes32,address,address,uint256,uint256)"))
	topicBalancerPoolBalanceChanged = crypto.Keccak256Hash([]byte("PoolBalanceChanged(bytes32,address,address[],int256[],uint256[])"))
	topicBalancerSwapFeeChanged     = crypto.Keccak256Hash([]byte("SwapFeePercentageChanged(uint256)"))
	topicBalancerAmpUpdateStopped   = crypto.Keccak256Hash([]byte("AmpUpdateStopped(uint256)"))
)

// BalancerABIParser decodes Balancer Vault logs into domain events.
type BalancerABIParser struct {
	vaultABI        abi.ABI
	poolABI         abi.ABI
	mu              sync.RWMutex
	poolIDByAddress map[common.Address]marketbalancer.PoolID
}

func NewBalancerABIParser() (*BalancerABIParser, error) {
	vaultABI, err := abi.JSON(strings.NewReader(balancerVaultEventsABI))
	if err != nil {
		return nil, fmt.Errorf("parse balancer vault events abi: %w", err)
	}
	poolABI, err := abi.JSON(strings.NewReader(balancerPoolEventsABI))
	if err != nil {
		return nil, fmt.Errorf("parse balancer pool events abi: %w", err)
	}
	return &BalancerABIParser{
		vaultABI:        vaultABI,
		poolABI:         poolABI,
		poolIDByAddress: make(map[common.Address]marketbalancer.PoolID),
	}, nil
}

func BalancerVaultLogTopics() []common.Hash {
	return []common.Hash{topicBalancerSwap, topicBalancerPoolBalanceChanged}
}

func BalancerPoolLogTopics() []common.Hash {
	return []common.Hash{topicBalancerSwapFeeChanged, topicBalancerAmpUpdateStopped}
}

func (p *BalancerABIParser) SetPoolAddressMap(poolIDByAddress map[common.Address]marketbalancer.PoolID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.poolIDByAddress = make(map[common.Address]marketbalancer.PoolID, len(poolIDByAddress))
	for address, poolID := range poolIDByAddress {
		p.poolIDByAddress[address] = poolID
	}
}

func (p *BalancerABIParser) ParsePoolEvents(logs []syncbalancer.RawLog) ([]marketbalancer.PoolEvent, error) {
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

func (p *BalancerABIParser) parseLog(log syncbalancer.RawLog) (*marketbalancer.PoolEvent, error) {
	if len(log.Topics) == 0 {
		return nil, nil
	}

	switch log.Topics[0] {
	case topicBalancerSwap:
		if len(log.Topics) < 2 {
			return nil, nil
		}
		meta := marketbalancer.EventMeta{
			PoolID:      marketbalancer.PoolID(log.Topics[1]),
			BlockNumber: log.BlockNumber,
			TxIndex:     log.TxIndex,
			LogIndex:    log.LogIndex,
		}
		return p.parseSwap(meta, log)
	case topicBalancerPoolBalanceChanged:
		if len(log.Topics) < 2 {
			return nil, nil
		}
		meta := marketbalancer.EventMeta{
			PoolID:      marketbalancer.PoolID(log.Topics[1]),
			BlockNumber: log.BlockNumber,
			TxIndex:     log.TxIndex,
			LogIndex:    log.LogIndex,
		}
		return p.parsePoolBalanceChanged(meta, log)
	case topicBalancerSwapFeeChanged:
		return p.parseSwapFeePercentageChanged(log)
	case topicBalancerAmpUpdateStopped:
		return p.parseAmpUpdateStopped(log)
	default:
		return nil, nil
	}
}

func (p *BalancerABIParser) parseSwap(meta marketbalancer.EventMeta, log syncbalancer.RawLog) (*marketbalancer.PoolEvent, error) {
	if len(log.Topics) < 4 {
		return nil, fmt.Errorf("swap event missing indexed topics")
	}
	values, err := p.vaultABI.Unpack("Swap", log.Data)
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

func (p *BalancerABIParser) parsePoolBalanceChanged(meta marketbalancer.EventMeta, log syncbalancer.RawLog) (*marketbalancer.PoolEvent, error) {
	values, err := p.vaultABI.Unpack("PoolBalanceChanged", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) < 3 {
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

func (p *BalancerABIParser) parseSwapFeePercentageChanged(log syncbalancer.RawLog) (*marketbalancer.PoolEvent, error) {
	meta, ok := p.metaFromPoolAddress(log)
	if !ok {
		return nil, nil
	}
	values, err := p.poolABI.Unpack("SwapFeePercentageChanged", log.Data)
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

func (p *BalancerABIParser) parseAmpUpdateStopped(log syncbalancer.RawLog) (*marketbalancer.PoolEvent, error) {
	meta, ok := p.metaFromPoolAddress(log)
	if !ok {
		return nil, nil
	}
	values, err := p.poolABI.Unpack("AmpUpdateStopped", log.Data)
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

func (p *BalancerABIParser) metaFromPoolAddress(log syncbalancer.RawLog) (marketbalancer.EventMeta, bool) {
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

const balancerVaultEventsABI = `[
  {"anonymous":false,"inputs":[{"indexed":true,"name":"poolId","type":"bytes32"},{"indexed":true,"name":"tokenIn","type":"address"},{"indexed":true,"name":"tokenOut","type":"address"},{"indexed":false,"name":"amountIn","type":"uint256"},{"indexed":false,"name":"amountOut","type":"uint256"}],"name":"Swap","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"poolId","type":"bytes32"},{"indexed":true,"name":"liquidityProvider","type":"address"},{"indexed":false,"name":"tokens","type":"address[]"},{"indexed":false,"name":"deltas","type":"int256[]"},{"indexed":false,"name":"protocolFeeAmounts","type":"uint256[]"}],"name":"PoolBalanceChanged","type":"event"}
]`

const balancerPoolEventsABI = `[
  {"anonymous":false,"inputs":[{"indexed":false,"name":"swapFeePercentage","type":"uint256"}],"name":"SwapFeePercentageChanged","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":false,"name":"currentValue","type":"uint256"}],"name":"AmpUpdateStopped","type":"event"}
]`
