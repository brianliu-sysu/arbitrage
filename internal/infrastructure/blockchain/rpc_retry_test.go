package blockchain

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCallBlockCandidates(t *testing.T) {
	cases := []struct {
		blockNumber uint64
		want        []string
	}{
		{blockNumber: 0, want: []string{"latest"}},
		{blockNumber: 1, want: []string{"1", "latest"}},
		{blockNumber: 100, want: []string{"100", "99", "latest"}},
	}
	for _, tc := range cases {
		got := callBlockCandidates(tc.blockNumber)
		if len(got) != len(tc.want) {
			t.Fatalf("block %d: expected %d candidates, got %d", tc.blockNumber, len(tc.want), len(got))
		}
		for i, block := range got {
			if blockLabel(block) != tc.want[i] {
				t.Fatalf("block %d candidate %d: expected %s, got %s", tc.blockNumber, i, tc.want[i], blockLabel(block))
			}
		}
	}
}

func TestSleepWithContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepWithContext(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}
