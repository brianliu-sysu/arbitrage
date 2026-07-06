package arbitrageapp_test

import (
	"fmt"
	"testing"

	arbitrageapp "github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

func overlapToken(index byte) common.Address {
	return common.HexToAddress(fmt.Sprintf("0x00000000000000000000000000000000000000%02x", index))
}

func TestTopPoolOverlapTokens(t *testing.T) {
	tokenA := overlapToken(1)
	tokenB := overlapToken(2)
	tokenC := overlapToken(3)
	tokenD := overlapToken(4)

	edges := []quoteunified.PoolEdge{
		{Token0: tokenA, Token1: tokenB},
		{Token0: tokenA, Token1: tokenC},
		{Token0: tokenA, Token1: tokenD},
		{Token0: tokenB, Token1: tokenC},
	}

	top := arbitrageapp.TopPoolOverlapTokens(edges, 3)
	if len(top) != 3 {
		t.Fatalf("expected 3 tokens, got %d", len(top))
	}
	if top[0] != tokenA {
		t.Fatalf("expected tokenA first, got %s", top[0].Hex())
	}
	if top[1] != tokenB || top[2] != tokenC {
		t.Fatalf("expected tokenB and tokenC next, got %s and %s", top[1].Hex(), top[2].Hex())
	}
}

func TestResolveTriangleStartTokensDedupesConfiguredAndAuto(t *testing.T) {
	tokenA := overlapToken(1)
	tokenB := overlapToken(2)
	tokenC := overlapToken(3)
	tokenD := overlapToken(4)

	edges := []quoteunified.PoolEdge{
		{Token0: tokenA, Token1: tokenB},
		{Token0: tokenA, Token1: tokenC},
		{Token0: tokenA, Token1: tokenD},
		{Token0: tokenB, Token1: tokenC},
	}

	configured := []common.Address{tokenC, tokenA}
	resolved := arbitrageapp.ResolveTriangleStartTokens(configured, edges, 3)
	if len(resolved) != 3 {
		t.Fatalf("expected 3 unique tokens, got %d: %+v", len(resolved), resolved)
	}
	if resolved[0] != tokenC || resolved[1] != tokenA {
		t.Fatalf("expected configured tokens first, got %+v", resolved)
	}
	if resolved[2] != tokenB {
		t.Fatalf("expected auto token B last, got %+v", resolved)
	}
}
