package clv3sync

import (
	"context"
	"fmt"
	"testing"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
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
			if got := syncapp.CatchupStartBlock(tc.checkpointBlock, tc.poolLastBlock); got != tc.want {
				t.Fatalf("CatchupStartBlock() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestGroupCatchupPools(t *testing.T) {
	pool := func(i int) common.Address {
		return common.BigToAddress(common.Big1.Add(common.Big1, common.Big0.SetUint64(uint64(i))))
	}

	tasks := []syncapp.CatchupTask[common.Address]{
		{ID: pool(1), FromBlock: 1000},
		{ID: pool(2), FromBlock: 1050},
		{ID: pool(3), FromBlock: 1100},
		{ID: pool(4), FromBlock: 1200},
	}

	groups := syncapp.GroupCatchupTasks(tasks, 100, 100)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].MinFromBlock != 1000 || len(groups[0].Tasks) != 3 {
		t.Fatalf("unexpected first group: min=%d size=%d", groups[0].MinFromBlock, len(groups[0].Tasks))
	}
	if groups[1].MinFromBlock != 1200 || len(groups[1].Tasks) != 1 {
		t.Fatalf("unexpected second group: min=%d size=%d", groups[1].MinFromBlock, len(groups[1].Tasks))
	}
}

func TestGroupCatchupPoolsMaxPoolSize(t *testing.T) {
	tasks := make([]syncapp.CatchupTask[common.Address], 0, 101)
	for i := 0; i < 101; i++ {
		tasks = append(tasks, syncapp.CatchupTask[common.Address]{
			ID:        common.BigToAddress(common.Big0.SetUint64(uint64(i + 1))),
			FromBlock: 1000,
		})
	}

	groups := syncapp.GroupCatchupTasks(tasks, 100, 100)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if len(groups[0].Tasks) != 100 || len(groups[1].Tasks) != 1 {
		t.Fatalf("unexpected group sizes: %d and %d", len(groups[0].Tasks), len(groups[1].Tasks))
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

	tracked := syncapp.TrackedPoolsForBlock(pools, fromBlocks, 1025)
	if len(tracked) != 1 || tracked[0] != poolA {
		t.Fatalf("expected only pool A before its start block, got %#v", tracked)
	}

	tracked = syncapp.TrackedPoolsForBlock(pools, fromBlocks, 1050)
	if len(tracked) != 2 {
		t.Fatalf("expected both pools at block 1050, got %#v", tracked)
	}
}

func TestBlockHashesFromLogs(t *testing.T) {
	hash100 := common.HexToHash("0x100")
	hash101 := common.HexToHash("0x101")

	hashes := syncapp.BlockHashesFromLogs([]syncapp.RawLog{
		{BlockNumber: 100, BlockHash: hash100},
		{BlockNumber: 101, BlockHash: hash101},
		{BlockNumber: 101, BlockHash: hash101},
	})
	if len(hashes) != 2 {
		t.Fatalf("expected 2 block hashes, got %d", len(hashes))
	}
	if hashes[100] != hash100 || hashes[101] != hash101 {
		t.Fatalf("unexpected hashes: %#v", hashes)
	}
}

func TestFetchBlockHeadersConcurrent(t *testing.T) {
	reader := &stubBlockReader{
		headers: map[uint64]blockchain.BlockHeader{
			10: {Number: 10, Hash: common.HexToHash("0xa")},
			11: {Number: 11, Hash: common.HexToHash("0xb")},
			12: {Number: 12, Hash: common.HexToHash("0xc")},
		},
	}

	hashes, err := syncapp.FetchBlockHeaders(context.Background(), reader, []uint64{10, 11, 12}, 2)
	if err != nil {
		t.Fatalf("fetch block headers: %v", err)
	}
	if len(hashes) != 3 {
		t.Fatalf("expected 3 hashes, got %d", len(hashes))
	}
	if hashes[11] != common.HexToHash("0xb") {
		t.Fatalf("unexpected hash for block 11: %s", hashes[11].Hex())
	}
}

type stubBlockReader struct {
	headers map[uint64]blockchain.BlockHeader
}

func (r *stubBlockReader) GetBlockHeader(_ context.Context, blockNumber uint64) (blockchain.BlockHeader, error) {
	header, ok := r.headers[blockNumber]
	if !ok {
		return blockchain.BlockHeader{}, fmt.Errorf("header not found")
	}
	return header, nil
}

func (r *stubBlockReader) GetLatestBlockHeader(_ context.Context) (blockchain.BlockHeader, error) {
	return blockchain.BlockHeader{}, fmt.Errorf("not implemented")
}
