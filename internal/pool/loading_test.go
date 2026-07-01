package pool

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func noopApplier(_ *State, _ []types.Log) error { return nil }

func TestBackfillDirectThenDrainPendingEvents(t *testing.T) {
	st := NewState(common.HexToAddress("0x1"), common.Address{}, common.Address{}, 3000)
	st.UpdateFromSwap(big.NewInt(1), 0, big.NewInt(1), 100)

	var applied []uint64
	apply := func(p *State, logs []types.Log) error {
		for _, l := range logs {
			applied = append(applied, l.BlockNumber)
		}
		return nil
	}

	st.BeginLoading()
	if ok := st.ApplyBlockEvents(105, []types.Log{{BlockNumber: 105, Index: 1}}); !ok {
		t.Fatal("expected loading event to be buffered")
	}

	for _, b := range []uint64{102, 103, 104} {
		if err := st.ApplyBlockEventsDirect(b, []types.Log{{BlockNumber: b, Index: 1}}, apply); err != nil {
			t.Fatalf("direct apply block %d: %v", b, err)
		}
	}
	if st.BlockNumber != 104 {
		t.Fatalf("BlockNumber=%d want 104 after backfill", st.BlockNumber)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := st.DrainPendingBlockEvents(ctx, apply); err != nil {
		t.Fatalf("DrainPendingBlockEvents: %v", err)
	}

	want := []uint64{102, 103, 104, 105}
	if len(applied) != len(want) {
		t.Fatalf("applied=%v want=%v", applied, want)
	}
	for i, b := range want {
		if applied[i] != b {
			t.Fatalf("applied[%d]=%d want=%v", i, applied[i], want)
		}
	}
	if st.BlockNumber != 105 {
		t.Fatalf("BlockNumber=%d want 105", st.BlockNumber)
	}
	if st.Loading() {
		t.Fatal("state should no longer be loading after drain")
	}
}

func TestDrainPendingDiscardsStaleEvents(t *testing.T) {
	st := NewState(common.HexToAddress("0x1"), common.Address{}, common.Address{}, 3000)
	st.UpdateFromSwap(big.NewInt(1), 0, big.NewInt(1), 100)

	var applied []uint64
	apply := func(p *State, logs []types.Log) error {
		for _, l := range logs {
			applied = append(applied, l.BlockNumber)
		}
		return nil
	}

	st.BeginLoading()
	for _, b := range []uint64{102, 103, 104} {
		if err := st.ApplyBlockEventsDirect(b, []types.Log{{BlockNumber: b}}, apply); err != nil {
			t.Fatalf("direct apply block %d: %v", b, err)
		}
	}
	st.ApplyBlockEvents(103, []types.Log{{BlockNumber: 103}})
	st.ApplyBlockEvents(105, []types.Log{{BlockNumber: 105}})
	st.ApplyBlockEvents(106, []types.Log{{BlockNumber: 106}})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := st.DrainPendingBlockEvents(ctx, apply); err != nil {
		t.Fatalf("DrainPendingBlockEvents: %v", err)
	}

	want := []uint64{102, 103, 104, 105, 106}
	if len(applied) != len(want) {
		t.Fatalf("applied=%v want=%v", applied, want)
	}
	for i, b := range want {
		if applied[i] != b {
			t.Fatalf("applied[%d]=%d want=%v", i, applied[i], want)
		}
	}
}

func TestApplyBlockEventsDirectDiscardsStaleBlock(t *testing.T) {
	st := NewState(common.HexToAddress("0x1"), common.Address{}, common.Address{}, 3000)
	st.UpdateFromSwap(big.NewInt(1), 0, big.NewInt(1), 500)

	calls := 0
	apply := func(p *State, logs []types.Log) error {
		calls++
		return nil
	}
	if err := st.ApplyBlockEventsDirect(500, []types.Log{{Index: 1}}, apply); err != nil {
		t.Fatalf("ApplyBlockEventsDirect: %v", err)
	}
	if calls != 0 {
		t.Fatalf("stale block should be discarded, calls=%d", calls)
	}
	if err := st.ApplyBlockEventsDirect(501, []types.Log{{Index: 1}}, apply); err != nil {
		t.Fatalf("ApplyBlockEventsDirect: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 apply, calls=%d", calls)
	}
}

func TestApplyBlockEventsOnlyBuffersDuringHandoff(t *testing.T) {
	st := NewState(common.HexToAddress("0x1"), common.Address{}, common.Address{}, 3000)
	st.UpdateFromSwap(big.NewInt(1), 0, big.NewInt(1), 100)

	if ok := st.ApplyBlockEvents(101, []types.Log{{BlockNumber: 101}}); ok {
		t.Fatal("event should not be buffered outside handoff")
	}
	if st.PendingLen() != 0 {
		t.Fatalf("pending len=%d want 0", st.PendingLen())
	}

	st.BeginLoading()
	if ok := st.ApplyBlockEvents(101, []types.Log{{BlockNumber: 101}}); !ok {
		t.Fatal("event should be buffered while loading")
	}
	if st.PendingLen() != 1 {
		t.Fatalf("pending len=%d want 1", st.PendingLen())
	}
}
