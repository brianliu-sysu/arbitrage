package blockchain

import (
	"math/big"
	"strings"
	"testing"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

func TestQuickSwapABIParserSwapEvent(t *testing.T) {
	parser, err := NewQuickSwapABIParser()
	if err != nil {
		t.Fatalf("new quickswap parser: %v", err)
	}
	poolABI := mustQuickSwapPoolABI(t)

	sqrtPrice, _ := new(big.Int).SetString("3340425863164323947729804", 10)
	liquidity, _ := new(big.Int).SetString("118472721364473142", 10)
	data, err := poolABI.Events["Swap"].Inputs.NonIndexed().Pack(
		big.NewInt(-100),
		big.NewInt(200),
		sqrtPrice,
		liquidity,
		int32ToABIInt24(-201490),
	)
	if err != nil {
		t.Fatalf("pack swap: %v", err)
	}

	sender := common.HexToAddress("0x0000000000000000000000000000000000000002")
	recipient := common.HexToAddress("0x0000000000000000000000000000000000000003")
	poolAddress := common.HexToAddress("0x45dDa9cb7c25131DF268515131f647d726f50608")

	events, err := parser.ParsePoolEvents([]domainchain.RawLog{{
		Address: poolAddress,
		Topics: []common.Hash{
			topicSwap,
			common.BytesToHash(common.LeftPadBytes(sender.Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(recipient.Bytes(), 32)),
		},
		Data:        data,
		BlockNumber: 60_000_000,
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

func TestQuickSwapPoolLogTopicsUseUniswapStyleSwap(t *testing.T) {
	topics := QuickSwapPoolLogTopics()
	found := false
	for _, topic := range topics {
		if topic == topicPancakeSwap {
			t.Fatal("quickswap log topics must not include pancake swap topic")
		}
		if topic == topicSwap {
			found = true
		}
	}
	if !found {
		t.Fatal("quickswap log topics must include uniswap-style swap topic")
	}
}

func mustQuickSwapPoolABI(t *testing.T) abi.ABI {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(algebraPoolABIJSON))
	if err != nil {
		t.Fatalf("parse quickswap pool abi: %v", err)
	}
	return parsed
}
