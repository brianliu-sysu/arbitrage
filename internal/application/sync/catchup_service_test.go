package syncapp

import "testing"

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
