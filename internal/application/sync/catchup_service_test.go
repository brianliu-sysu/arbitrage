package syncapp

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestCatchupStartBlock(t *testing.T) {
	cases := []struct {
		name            string
		checkpointBlock uint64
		poolLastBlock   uint64
		want            uint64
	}{
		{name: "fresh pool", want: 1},
		{name: "checkpoint only", checkpointBlock: 100, want: 101},
		{name: "pool ahead of checkpoint", checkpointBlock: 100, poolLastBlock: 200, want: 201},
		{name: "checkpoint ahead of pool", checkpointBlock: 200, poolLastBlock: 100, want: 201},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := catchupStartBlock(tc.checkpointBlock, tc.poolLastBlock); got != tc.want {
				t.Fatalf("catchupStartBlock() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestGroupCatchupPools(t *testing.T) {
	pool := func(i int) common.Address {
		return common.BigToAddress(common.Big1.Add(common.Big1, common.Big0.SetUint64(uint64(i))))
	}

	tasks := []catchupPoolTask{
		{address: pool(1), fromBlock: 1000},
		{address: pool(2), fromBlock: 1050},
		{address: pool(3), fromBlock: 1100},
		{address: pool(4), fromBlock: 1200},
	}

	groups := groupCatchupPools(tasks, 100, 100)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].minFromBlock != 1000 || len(groups[0].tasks) != 3 {
		t.Fatalf("unexpected first group: min=%d size=%d", groups[0].minFromBlock, len(groups[0].tasks))
	}
	if groups[1].minFromBlock != 1200 || len(groups[1].tasks) != 1 {
		t.Fatalf("unexpected second group: min=%d size=%d", groups[1].minFromBlock, len(groups[1].tasks))
	}
}

func TestGroupCatchupPoolsMaxPoolSize(t *testing.T) {
	tasks := make([]catchupPoolTask, 0, 101)
	for i := 0; i < 101; i++ {
		tasks = append(tasks, catchupPoolTask{
			address:   common.BigToAddress(common.Big0.SetUint64(uint64(i + 1))),
			fromBlock: 1000,
		})
	}

	groups := groupCatchupPools(tasks, 100, 100)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if len(groups[0].tasks) != 100 || len(groups[1].tasks) != 1 {
		t.Fatalf("unexpected group sizes: %d and %d", len(groups[0].tasks), len(groups[1].tasks))
	}
}

func TestTrackedPoolsForBlock(t *testing.T) {
	poolA := common.HexToAddress("0x1")
	poolB := common.HexToAddress("0x2")
	pools := []common.Address{poolA, poolB}
	fromBlocks := map[common.Address]uint64{
		poolA: 1000,
		poolB: 1050,
	}

	tracked := trackedPoolsForBlock(pools, fromBlocks, 1025)
	if len(tracked) != 1 || tracked[0] != poolA {
		t.Fatalf("expected only pool A before its start block, got %#v", tracked)
	}

	tracked = trackedPoolsForBlock(pools, fromBlocks, 1050)
	if len(tracked) != 2 {
		t.Fatalf("expected both pools at block 1050, got %#v", tracked)
	}
}
