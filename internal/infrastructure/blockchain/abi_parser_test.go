package blockchain

import (
	"math/big"
	"strings"
	"testing"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

func TestABIParserInitializeEvent(t *testing.T) {
	parser := mustParser(t)
	poolABI := mustPoolABI(t)

	sqrtPrice, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	data, err := poolABI.Events["Initialize"].Inputs.NonIndexed().Pack(sqrtPrice, big.NewInt(0))
	if err != nil {
		t.Fatalf("pack initialize: %v", err)
	}

	poolAddress := common.HexToAddress("0x0000000000000000000000000000000000000001")
	events, err := parser.ParsePoolEvents([]syncapp.RawLog{{
		Address:     poolAddress,
		Topics:      []common.Hash{topicInitialize},
		Data:        data,
		BlockNumber: 100,
		TxIndex:     1,
		LogIndex:    2,
	}})
	if err != nil {
		t.Fatalf("parse initialize: %v", err)
	}
	if len(events) != 1 || events[0].Kind != market.EventKindInitialize {
		t.Fatalf("unexpected events: %#v", events)
	}
	if events[0].Initialize.Tick != 0 {
		t.Fatalf("expected tick 0, got %d", events[0].Initialize.Tick)
	}
}

func TestABIParserSwapEvent(t *testing.T) {
	parser := mustParser(t)
	poolABI := mustPoolABI(t)

	sqrtPrice, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	data, err := poolABI.Events["Swap"].Inputs.NonIndexed().Pack(
		big.NewInt(-100),
		big.NewInt(200),
		sqrtPrice,
		big.NewInt(1000),
		big.NewInt(0),
	)
	if err != nil {
		t.Fatalf("pack swap: %v", err)
	}

	sender := common.HexToAddress("0x0000000000000000000000000000000000000002")
	recipient := common.HexToAddress("0x0000000000000000000000000000000000000003")
	poolAddress := common.HexToAddress("0x0000000000000000000000000000000000000001")

	events, err := parser.ParsePoolEvents([]syncapp.RawLog{{
		Address: poolAddress,
		Topics: []common.Hash{
			topicSwap,
			common.BytesToHash(common.LeftPadBytes(sender.Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(recipient.Bytes(), 32)),
		},
		Data:        data,
		BlockNumber: 101,
	}})
	if err != nil {
		t.Fatalf("parse swap: %v", err)
	}
	if len(events) != 1 || events[0].Kind != market.EventKindSwap {
		t.Fatalf("unexpected events: %#v", events)
	}
	if events[0].Swap.Liquidity.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("unexpected swap liquidity: %s", events[0].Swap.Liquidity)
	}
}

func TestABIParserBurnEvent(t *testing.T) {
	parser := mustParser(t)
	poolABI := mustPoolABI(t)

	amount := big.NewInt(5_000_000)
	data, err := poolABI.Events["Burn"].Inputs.NonIndexed().Pack(amount, big.NewInt(0), big.NewInt(1))
	if err != nil {
		t.Fatalf("pack burn: %v", err)
	}

	owner := common.HexToAddress("0x0000000000000000000000000000000000000002")
	poolAddress := common.HexToAddress("0x0000000000000000000000000000000000000001")
	events, err := parser.ParsePoolEvents([]syncapp.RawLog{{
		Address: poolAddress,
		Topics: []common.Hash{
			topicBurn,
			common.BytesToHash(common.LeftPadBytes(owner.Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(int32ToABIInt24(-120).Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(int32ToABIInt24(120).Bytes(), 32)),
		},
		Data:        data,
		BlockNumber: 102,
	}})
	if err != nil {
		t.Fatalf("parse burn: %v", err)
	}
	if len(events) != 1 || events[0].Kind != market.EventKindBurn {
		t.Fatalf("unexpected events: %#v", events)
	}
	if events[0].Burn.Amount.Cmp(amount) != 0 {
		t.Fatalf("expected burn amount %s, got %s", amount, events[0].Burn.Amount)
	}
}

func TestABIParserSkipsZeroBurn(t *testing.T) {
	parser := mustParser(t)
	poolABI := mustPoolABI(t)

	data, err := poolABI.Events["Burn"].Inputs.NonIndexed().Pack(big.NewInt(0), big.NewInt(1), big.NewInt(2))
	if err != nil {
		t.Fatalf("pack burn: %v", err)
	}

	events, err := parser.ParsePoolEvents([]syncapp.RawLog{{
		Address: common.HexToAddress("0x0000000000000000000000000000000000000001"),
		Topics: []common.Hash{
			topicBurn,
			common.BytesToHash(common.LeftPadBytes(common.Address{}.Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(int32ToABIInt24(-120).Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(int32ToABIInt24(120).Bytes(), 32)),
		},
		Data:        data,
		BlockNumber: 103,
	}})
	if err != nil {
		t.Fatalf("parse burn: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected zero burn to be skipped, got %#v", events)
	}
}

func TestSortTokens(t *testing.T) {
	tokenA := common.HexToAddress("0x0000000000000000000000000000000000000002")
	tokenB := common.HexToAddress("0x0000000000000000000000000000000000000001")
	first, second := sortTokens(tokenA, tokenB)
	if first != tokenB || second != tokenA {
		t.Fatalf("tokens not sorted")
	}
}

func mustParser(t *testing.T) *ABIParser {
	t.Helper()
	parser, err := NewABIParser()
	if err != nil {
		t.Fatalf("new parser: %v", err)
	}
	return parser
}

func TestPackTicksCall(t *testing.T) {
	poolABI := mustPoolABI(t)

	_, err := poolABI.Pack("ticks", int32ToABIInt24(-887220))
	if err != nil {
		t.Fatalf("pack ticks: %v", err)
	}
}

func mustPoolABI(t *testing.T) abi.ABI {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(poolABIJSON))
	if err != nil {
		t.Fatalf("parse pool abi: %v", err)
	}
	return parsed
}
