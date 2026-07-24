package protocol

import "testing"

func TestReorgReplayFromBlock(t *testing.T) {
	t.Parallel()

	if got := ReorgReplayFromBlock(25_471_000, 25_472_000, true); got != 25_471_001 {
		t.Fatalf("expected replay from snapshot+1, got %d", got)
	}
	if got := ReorgReplayFromBlock(25_472_000, 25_472_000, true); got != 25_472_001 {
		t.Fatalf("expected replay from ancestor+1 when snapshot at ancestor, got %d", got)
	}
	if got := ReorgReplayFromBlock(0, 25_472_000, false); got != 25_472_001 {
		t.Fatalf("expected replay from ancestor+1 without snapshot, got %d", got)
	}
}
