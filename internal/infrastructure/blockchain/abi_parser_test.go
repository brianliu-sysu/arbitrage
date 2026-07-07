package blockchain

import (
	"math/big"
	"strings"
	"testing"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
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
	if len(events) != 1 || events[0].Kind != marketv3.EventKindInitialize {
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
	if len(events) != 1 || events[0].Kind != marketv3.EventKindSwap {
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
	if len(events) != 1 || events[0].Kind != marketv3.EventKindBurn {
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

func TestV4ABIParserInitializeEvent(t *testing.T) {
	parser, managerABI := mustV4ParserAndABI(t)

	poolID := marketv4.PoolID(common.HexToHash("0x1000000000000000000000000000000000000000000000000000000000000000"))
	currency0 := common.HexToAddress("0x0000000000000000000000000000000000000001")
	currency1 := common.HexToAddress("0x0000000000000000000000000000000000000002")
	hooks := common.HexToAddress("0x0000000000000000000000000000000000000003")
	sqrtPrice, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	data, err := managerABI.Events["Initialize"].Inputs.NonIndexed().Pack(
		big.NewInt(500),
		int32ToABIInt24(10),
		hooks,
		sqrtPrice,
		int32ToABIInt24(-120),
	)
	if err != nil {
		t.Fatalf("pack v4 initialize: %v", err)
	}

	events, err := parser.ParsePoolEvents([]syncapp.RawLog{{
		Topics: []common.Hash{
			topicV4Initialize,
			poolID.Hash(),
			common.BytesToHash(common.LeftPadBytes(currency0.Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(currency1.Bytes(), 32)),
		},
		Data:        data,
		BlockNumber: 200,
		TxIndex:     1,
		LogIndex:    2,
	}})
	if err != nil {
		t.Fatalf("parse v4 initialize: %v", err)
	}
	if len(events) != 1 || events[0].Kind != marketv4.EventKindInitialize {
		t.Fatalf("unexpected events: %#v", events)
	}
	if events[0].Meta.PoolID != poolID || events[0].Meta.BlockNumber != 200 || events[0].Meta.TxIndex != 1 || events[0].Meta.LogIndex != 2 {
		t.Fatalf("unexpected event meta: %#v", events[0].Meta)
	}
	if events[0].Initialize.Tick != -120 || events[0].Initialize.SqrtPriceX96.Cmp(sqrtPrice) != 0 {
		t.Fatalf("unexpected initialize payload: %#v", events[0].Initialize)
	}
}

func TestV4ABIParserSwapEvent(t *testing.T) {
	parser, managerABI := mustV4ParserAndABI(t)

	poolID := marketv4.PoolID(common.HexToHash("0x2000000000000000000000000000000000000000000000000000000000000000"))
	sender := common.HexToAddress("0x0000000000000000000000000000000000000004")
	sqrtPrice, _ := new(big.Int).SetString("79228162514264337593543950336", 10)
	data, err := managerABI.Events["Swap"].Inputs.NonIndexed().Pack(
		big.NewInt(-100),
		big.NewInt(200),
		sqrtPrice,
		big.NewInt(1000),
		int32ToABIInt24(60),
		big.NewInt(3000),
	)
	if err != nil {
		t.Fatalf("pack v4 swap: %v", err)
	}

	events, err := parser.ParsePoolEvents([]syncapp.RawLog{{
		Topics: []common.Hash{
			topicV4Swap,
			poolID.Hash(),
			common.BytesToHash(common.LeftPadBytes(sender.Bytes(), 32)),
		},
		Data:        data,
		BlockNumber: 201,
	}})
	if err != nil {
		t.Fatalf("parse v4 swap: %v", err)
	}
	if len(events) != 1 || events[0].Kind != marketv4.EventKindSwap {
		t.Fatalf("unexpected events: %#v", events)
	}
	swap := events[0].Swap
	if swap == nil {
		t.Fatal("swap payload is nil")
	}
	if swap.Sender != sender || swap.Amount0.Cmp(big.NewInt(-100)) != 0 || swap.Amount1.Cmp(big.NewInt(200)) != 0 ||
		swap.SqrtPriceX96.Cmp(sqrtPrice) != 0 || swap.Liquidity.Cmp(big.NewInt(1000)) != 0 || swap.Tick != 60 || swap.Fee != 3000 {
		t.Fatalf("unexpected swap payload: %#v", swap)
	}
}

func TestV4ABIParserModifyLiquidityEvent(t *testing.T) {
	parser, managerABI := mustV4ParserAndABI(t)

	poolID := marketv4.PoolID(common.HexToHash("0x3000000000000000000000000000000000000000000000000000000000000000"))
	sender := common.HexToAddress("0x0000000000000000000000000000000000000005")
	salt := common.HexToHash("0xabc0000000000000000000000000000000000000000000000000000000000000")
	liquidityDelta := big.NewInt(-500)
	data, err := managerABI.Events["ModifyLiquidity"].Inputs.NonIndexed().Pack(
		int32ToABIInt24(-120),
		int32ToABIInt24(120),
		liquidityDelta,
		salt,
	)
	if err != nil {
		t.Fatalf("pack v4 modify liquidity: %v", err)
	}

	events, err := parser.ParsePoolEvents([]syncapp.RawLog{{
		Topics: []common.Hash{
			topicV4ModifyLiquidity,
			poolID.Hash(),
			common.BytesToHash(common.LeftPadBytes(sender.Bytes(), 32)),
		},
		Data:        data,
		BlockNumber: 202,
	}})
	if err != nil {
		t.Fatalf("parse v4 modify liquidity: %v", err)
	}
	if len(events) != 1 || events[0].Kind != marketv4.EventKindModifyLiquidity {
		t.Fatalf("unexpected events: %#v", events)
	}
	modify := events[0].ModifyLiquidity
	if modify == nil {
		t.Fatal("modify liquidity payload is nil")
	}
	if modify.Sender != sender || modify.TickLower != -120 || modify.TickUpper != 120 ||
		modify.LiquidityDelta.Cmp(liquidityDelta) != 0 || modify.Salt != salt {
		t.Fatalf("unexpected modify liquidity payload: %#v", modify)
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

func mustV4ManagerABI(t *testing.T) abi.ABI {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(poolManagerEventsABI))
	if err != nil {
		t.Fatalf("parse v4 manager abi: %v", err)
	}
	return parsed
}

func mustV4ParserAndABI(t *testing.T) (*V4ABIParser, abi.ABI) {
	t.Helper()
	parser, err := NewV4ABIParser()
	if err != nil {
		t.Fatalf("new v4 parser: %v", err)
	}
	return parser, mustV4ManagerABI(t)
}
