package blockchain

import (
	"fmt"
	"math/big"
	"strings"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// CLV3PoolParser decodes concentrated-liquidity V3-style pool logs into domain events.
type CLV3PoolParser struct {
	poolABI   abi.ABI
	swapTopic common.Hash
}

func newCLV3PoolParser(abiJSON string, swapTopic common.Hash) (*CLV3PoolParser, error) {
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, fmt.Errorf("parse pool abi: %w", err)
	}
	return &CLV3PoolParser{
		poolABI:   parsed,
		swapTopic: swapTopic,
	}, nil
}

func (p *CLV3PoolParser) ParsePoolEvents(logs []syncapp.RawLog) ([]marketv3.PoolEvent, error) {
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

func (p *CLV3PoolParser) parseLog(log syncapp.RawLog) (*marketv3.PoolEvent, error) {
	meta := marketv3.EventMeta{
		PoolAddress: log.Address,
		BlockNumber: log.BlockNumber,
		TxIndex:     log.TxIndex,
		LogIndex:    log.LogIndex,
	}

	switch log.Topics[0] {
	case topicInitialize:
		return p.parseInitialize(meta, log)
	case p.swapTopic:
		return p.parseSwap(meta, log)
	case topicMint:
		return p.parseMint(meta, log)
	case topicBurn:
		return p.parseBurn(meta, log)
	default:
		return nil, nil
	}
}

func (p *CLV3PoolParser) parseInitialize(meta marketv3.EventMeta, log syncapp.RawLog) (*marketv3.PoolEvent, error) {
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

func (p *CLV3PoolParser) parseSwap(meta marketv3.EventMeta, log syncapp.RawLog) (*marketv3.PoolEvent, error) {
	if len(log.Topics) < 3 {
		return nil, fmt.Errorf("swap event missing indexed topics")
	}
	sender := common.BytesToAddress(log.Topics[1].Bytes())
	recipient := common.BytesToAddress(log.Topics[2].Bytes())

	values, err := p.poolABI.Unpack("Swap", log.Data)
	if err != nil {
		return nil, err
	}
	if len(values) < 5 {
		return nil, fmt.Errorf("swap event has %d values", len(values))
	}
	amount0 := values[0].(*big.Int)
	amount1 := values[1].(*big.Int)
	sqrtPriceX96 := values[2].(*big.Int)
	liquidity, err := abiUintToBigInt(values[3])
	if err != nil {
		return nil, fmt.Errorf("swap liquidity: %w", err)
	}
	tick, err := abiInt24ToInt32(values[4])
	if err != nil {
		return nil, err
	}
	event := marketv3.NewSwapEvent(meta, sender, recipient, amount0, amount1, sqrtPriceX96, liquidity, tick)
	return &event, nil
}

func (p *CLV3PoolParser) parseMint(meta marketv3.EventMeta, log syncapp.RawLog) (*marketv3.PoolEvent, error) {
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

func (p *CLV3PoolParser) parseBurn(meta marketv3.EventMeta, log syncapp.RawLog) (*marketv3.PoolEvent, error) {
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
