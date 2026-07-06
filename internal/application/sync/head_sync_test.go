package syncapp_test

import (
	"testing"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum/common"
)

func TestShouldSkipHeadNotification(t *testing.T) {
	local := blockchain.BlockHeader{Number: 100, Hash: common.HexToHash("0x100")}

	cases := []struct {
		name   string
		remote blockchain.BlockHeader
		want   bool
	}{
		{
			name:   "fresh local head",
			remote: blockchain.BlockHeader{Number: 100, Hash: common.HexToHash("0x100")},
			want:   false,
		},
		{
			name:   "duplicate head",
			remote: blockchain.BlockHeader{Number: 100, Hash: common.HexToHash("0x100")},
			want:   true,
		},
		{
			name:   "stale head",
			remote: blockchain.BlockHeader{Number: 99, Hash: common.HexToHash("0x99")},
			want:   true,
		},
		{
			name:   "next head",
			remote: blockchain.BlockHeader{Number: 101, Hash: common.HexToHash("0x101")},
			want:   false,
		},
		{
			name:   "same height different hash",
			remote: blockchain.BlockHeader{Number: 100, Hash: common.HexToHash("0x101")},
			want:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			localHead := blockchain.BlockHeader{}
			if tc.name != "fresh local head" {
				localHead = local
			}
			if got := syncapp.ShouldSkipHeadNotification(localHead, tc.remote); got != tc.want {
				t.Fatalf("ShouldSkipHeadNotification() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNeedsHeadGapCatchup(t *testing.T) {
	local := blockchain.BlockHeader{Number: 100, Hash: common.HexToHash("0x100")}
	next := blockchain.BlockHeader{Number: 101, Hash: common.HexToHash("0x101")}
	gap := blockchain.BlockHeader{Number: 103, Hash: common.HexToHash("0x103")}

	if syncapp.NeedsHeadGapCatchup(blockchain.BlockHeader{}, next) {
		t.Fatal("expected no gap catchup from empty local head")
	}
	if syncapp.NeedsHeadGapCatchup(local, next) {
		t.Fatal("expected no gap catchup for consecutive head")
	}
	if !syncapp.NeedsHeadGapCatchup(local, gap) {
		t.Fatal("expected gap catchup when heads skip blocks")
	}
}
