package arbitrageapp

import (
	"strings"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

func TestEnsureRoutePoolsSyncedAtBlockRejectsStaleRoutePool(t *testing.T) {
	tokenA := common.HexToAddress("0x0000000000000000000000000000000000000001")
	tokenB := common.HexToAddress("0x0000000000000000000000000000000000000002")
	poolAddress := common.HexToAddress("0x0000000000000000000000000000000000000010")
	pool := marketuniv3.NewPool(poolAddress, tokenA, tokenB, 3000, 60)
	pool.LastBlockNumber = 99
	pool.Status = market.PoolStatusReady

	route := quoteunified.Route{
		TokenIn:  tokenA,
		TokenOut: tokenB,
		Hops: []quoteunified.RouteHop{
			{Version: quoteunified.PoolVersionV3, PoolV3: poolAddress, TokenIn: tokenA, TokenOut: tokenB},
		},
	}
	pools := quoteunified.RoutePools{
		V3: map[common.Address]*marketuniv3.Pool{poolAddress: pool},
	}

	err := ensureRoutePoolsSyncedAtBlock(route, pools, 100)
	if err == nil {
		t.Fatalf("expected stale pool error")
	}
	if !strings.Contains(err.Error(), "synced at block 99 before opportunity block 100") {
		t.Fatalf("unexpected error: %v", err)
	}

	pool.LastBlockNumber = 100
	if err := ensureRoutePoolsSyncedAtBlock(route, pools, 100); err != nil {
		t.Fatalf("expected synced pool to pass: %v", err)
	}
}

func TestEnsureRoutePoolsSyncedAtBlockAllowsVirtualHops(t *testing.T) {
	weth := common.HexToAddress("0x0000000000000000000000000000000000000001")
	route := quoteunified.Route{
		TokenIn:  common.Address{},
		TokenOut: weth,
		Hops: []quoteunified.RouteHop{
			{Version: quoteunified.PoolVersionWrapWETH, TokenIn: common.Address{}, TokenOut: weth},
			{Version: quoteunified.PoolVersionUnwrapWETH, TokenIn: weth, TokenOut: common.Address{}},
		},
	}

	if err := ensureRoutePoolsSyncedAtBlock(route, quoteunified.RoutePools{}, 100); err != nil {
		t.Fatalf("expected virtual hops to pass: %v", err)
	}
}

func TestEnsureRoutePoolsSyncedAtBlockChecksMixedProtocols(t *testing.T) {
	tokenA := common.HexToAddress("0x0000000000000000000000000000000000000001")
	tokenB := common.HexToAddress("0x0000000000000000000000000000000000000002")
	tokenC := common.HexToAddress("0x0000000000000000000000000000000000000003")
	v4Key := marketuniv4.PoolKey{Currency0: tokenA, Currency1: tokenB, Fee: 500, TickSpacing: 10}
	v4ID, err := marketuniv4.ComputePoolID(v4Key)
	if err != nil {
		t.Fatalf("compute v4 pool id: %v", err)
	}
	v4Pool := marketuniv4.NewPool(v4ID, v4Key)
	v4Pool.LastBlockNumber = 100
	balancerID := marketbalancer.PoolID(common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000010"))
	balancerPool, err := marketbalancer.NewPool(balancerID, common.Address{}, common.Address{}, marketbalancer.PoolTypeWeighted, []common.Address{tokenB, tokenC})
	if err != nil {
		t.Fatalf("new balancer pool: %v", err)
	}
	balancerPool.LastBlockNumber = 99

	route := quoteunified.Route{
		TokenIn:  tokenA,
		TokenOut: tokenC,
		Hops: []quoteunified.RouteHop{
			{Version: quoteunified.PoolVersionV4, PoolV4: v4ID, TokenIn: tokenA, TokenOut: tokenB},
			{Version: quoteunified.PoolVersionBalancer, PoolBalancer: balancerID, TokenIn: tokenB, TokenOut: tokenC},
		},
	}
	pools := quoteunified.RoutePools{
		V4:       map[marketuniv4.PoolID]*marketuniv4.Pool{v4ID: v4Pool},
		Balancer: map[marketbalancer.PoolID]*marketbalancer.Pool{balancerID: balancerPool},
	}

	err = ensureRoutePoolsSyncedAtBlock(route, pools, 100)
	if err == nil {
		t.Fatalf("expected stale balancer pool error")
	}
	if !strings.Contains(err.Error(), "balancer pool") || !strings.Contains(err.Error(), "synced at block 99") {
		t.Fatalf("unexpected error: %v", err)
	}

	balancerPool.LastBlockNumber = 100
	if err := ensureRoutePoolsSyncedAtBlock(route, pools, 100); err != nil {
		t.Fatalf("expected mixed route to pass: %v", err)
	}
}
