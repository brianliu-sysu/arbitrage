package blockchain

import (
	"math/big"
	"strings"
	"testing"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestPancakeSwapTopicDiffersFromUniswap(t *testing.T) {
	if topicPancakeSwap == topicSwap {
		t.Fatal("expected pancake and uniswap swap topics to differ")
	}
	expected := crypto.Keccak256Hash([]byte("Swap(address,address,int256,int256,uint160,uint128,int24,uint128,uint128)"))
	if topicPancakeSwap != expected {
		t.Fatalf("unexpected pancake swap topic %s, want %s", topicPancakeSwap.Hex(), expected.Hex())
	}
}

func TestPancakeABIParserSwapEvent(t *testing.T) {
	parser := mustPancakeParser(t)
	poolABI := mustPancakePoolABI(t)

	sqrtPrice, _ := new(big.Int).SetString("3340425863164323947729804", 10)
	liquidity, _ := new(big.Int).SetString("118472721364473142", 10)
	data, err := poolABI.Events["Swap"].Inputs.NonIndexed().Pack(
		big.NewInt(-100),
		big.NewInt(200),
		sqrtPrice,
		liquidity,
		int32ToABIInt24(-201490),
		big.NewInt(11),
		big.NewInt(22),
	)
	if err != nil {
		t.Fatalf("pack swap: %v", err)
	}

	sender := common.HexToAddress("0x0000000000000000000000000000000000000002")
	recipient := common.HexToAddress("0x0000000000000000000000000000000000000003")
	poolAddress := common.HexToAddress("0x6CA298D2983aB03Aa1dA7679389D955A4eFEE15C")

	events, err := parser.ParsePoolEvents([]syncapp.RawLog{{
		Address: poolAddress,
		Topics: []common.Hash{
			topicPancakeSwap,
			common.BytesToHash(common.LeftPadBytes(sender.Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(recipient.Bytes(), 32)),
		},
		Data:        data,
		BlockNumber: 25_477_930,
	}})
	if err != nil {
		t.Fatalf("parse swap: %v", err)
	}
	if len(events) != 1 || events[0].Kind != marketv3.EventKindSwap {
		t.Fatalf("unexpected events: %#v", events)
	}
	swap := events[0].Swap
	if swap == nil {
		t.Fatal("swap payload is nil")
	}
	if swap.Tick != -201490 || swap.SqrtPriceX96.Cmp(sqrtPrice) != 0 || swap.Liquidity.Cmp(liquidity) != 0 {
		t.Fatalf("unexpected swap payload: %#v", swap)
	}
}

func TestPancakePoolLogTopicsExcludeUniswapSwap(t *testing.T) {
	topics := PancakePoolLogTopics()
	for _, topic := range topics {
		if topic == topicSwap {
			t.Fatal("pancake log topics must not include uniswap swap topic")
		}
	}
	found := false
	for _, topic := range topics {
		if topic == topicPancakeSwap {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("pancake log topics must include pancake swap topic")
	}
}

func mustPancakeParser(t *testing.T) *PancakeABIParser {
	t.Helper()
	parser, err := NewPancakeABIParser()
	if err != nil {
		t.Fatalf("new pancake parser: %v", err)
	}
	return parser
}

func mustPancakePoolABI(t *testing.T) abi.ABI {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(pancakePoolABIJSON))
	if err != nil {
		t.Fatalf("parse pancake pool abi: %v", err)
	}
	return parsed
}
