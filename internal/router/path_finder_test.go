package router

import (
	"testing"

	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/ethereum/go-ethereum/common"
)

var (
	addrPool1 = common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	addrPool2 = common.HexToAddress("0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640")
	addrPool3 = common.HexToAddress("0x99ac8cA7087fA4A2A1FB6357269965A2014ABc35")

	tkUSDC = common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	tkWETH = common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	tkUSDT = common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7")
	tkWBTC = common.HexToAddress("0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599")
	tkDAI  = common.HexToAddress("0x6B175474E89094C44Da98b954EedeAC495271d0F")
)

func makeCache(entries map[common.Address]struct {
	token0, token1 common.Address
	fee            uint32
}) *pool.Cache {
	c := pool.NewCache()
	for addr, e := range entries {
		c.Set(addr, pool.NewState(addr, e.token0, e.token1, e.fee))
	}
	return c
}

func TestPathFinderSingleHop(t *testing.T) {
	cache := makeCache(map[common.Address]struct {
		token0, token1 common.Address
		fee            uint32
	}{addrPool1: {tkUSDC, tkWETH, 3000}})
	pf := NewPathFinder(cache, 2, nil)

	paths := pf.FindPaths(tkUSDC, tkWETH)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	if len(paths[0].Hops) != 1 {
		t.Errorf("expected 1 hop, got %d", len(paths[0].Hops))
	}
}

func TestPathFinderTwoHops(t *testing.T) {
	cache := makeCache(map[common.Address]struct {
		token0, token1 common.Address
		fee            uint32
	}{
		addrPool1: {tkUSDC, tkWETH, 3000},
		addrPool2: {tkWETH, tkUSDT, 500},
	})
	pf := NewPathFinder(cache, 3, nil)

	paths := pf.FindPaths(tkUSDC, tkUSDT)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	if len(paths[0].Hops) != 2 {
		t.Errorf("expected 2 hops, got %d", len(paths[0].Hops))
	}
}

func TestPathFinderSameToken(t *testing.T) {
	cache := makeCache(map[common.Address]struct {
		token0, token1 common.Address
		fee            uint32
	}{addrPool1: {tkUSDC, tkWETH, 3000}})
	pf := NewPathFinder(cache, 2, nil)
	if len(pf.FindPaths(tkUSDC, tkUSDC)) != 0 {
		t.Error("expected 0 paths for same token")
	}
}

func TestEmptyPathFinder(t *testing.T) {
	pf := NewPathFinder(pool.NewCache(), 2, nil)
	if len(pf.FindPaths(tkUSDC, tkWETH)) != 0 {
		t.Error("expected 0 paths from empty finder")
	}
}

func TestPathFinderNoPath(t *testing.T) {
	cache := makeCache(map[common.Address]struct {
		token0, token1 common.Address
		fee            uint32
	}{addrPool1: {tkUSDC, tkWETH, 3000}})
	pf := NewPathFinder(cache, 2, nil)
	if len(pf.FindPaths(tkWBTC, tkDAI)) != 0 {
		t.Error("expected 0 paths")
	}
}
