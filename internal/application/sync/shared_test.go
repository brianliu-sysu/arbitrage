package syncapp_test

import (
	"testing"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
)

func TestGroupCatchupFromBlocks(t *testing.T) {
	fromBlocks := []uint64{1000, 1050, 1100, 1200}

	groups := syncapp.GroupCatchupFromBlocks(fromBlocks, 100, 100)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].MinFromBlock != 1000 || len(groups[0].Indices) != 3 {
		t.Fatalf("unexpected first group: min=%d size=%d", groups[0].MinFromBlock, len(groups[0].Indices))
	}
	if groups[1].MinFromBlock != 1200 || len(groups[1].Indices) != 1 {
		t.Fatalf("unexpected second group: min=%d size=%d", groups[1].MinFromBlock, len(groups[1].Indices))
	}
}

func TestGroupCatchupFromBlocksMaxPoolSize(t *testing.T) {
	fromBlocks := make([]uint64, 101)
	for i := range fromBlocks {
		fromBlocks[i] = 1000
	}

	groups := syncapp.GroupCatchupFromBlocks(fromBlocks, 100, 100)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if len(groups[0].Indices) != 100 || len(groups[1].Indices) != 1 {
		t.Fatalf("unexpected group sizes: %d and %d", len(groups[0].Indices), len(groups[1].Indices))
	}
}

func TestNeedsChainRebootstrap(t *testing.T) {
	if syncapp.NeedsChainRebootstrap(9000, 10_001, 1000) != true {
		t.Fatal("expected stale pool to require chain rebootstrap")
	}
	if syncapp.NeedsChainRebootstrap(9000, 9500, 1000) != false {
		t.Fatal("expected fresh pool to skip chain rebootstrap")
	}
}

func TestSnapshotPolicyShouldSnapshot(t *testing.T) {
	policy := syncapp.SnapshotPolicy{BlockInterval: 100}
	if policy.ShouldSnapshot(0, 50) {
		t.Fatal("expected first snapshot only after interval")
	}
	if !policy.ShouldSnapshot(0, 100) {
		t.Fatal("expected snapshot at interval boundary")
	}
	if policy.ShouldSnapshot(100, 150) {
		t.Fatal("expected snapshot only every interval")
	}
}
