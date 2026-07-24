package protocol_test

import (
	"context"
	"testing"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

type bootstrapPool struct {
	initialized bool
	lastBlock   uint64
	status      market.PoolStatus
}

type bootstrapProtocol struct {
	pool       *bootstrapPool
	readCalls  int
	applyCalls int
}

func (p *bootstrapProtocol) IsNilPool(pool *bootstrapPool) bool { return pool == nil }
func (p *bootstrapProtocol) LoadPool(context.Context, int) (*bootstrapPool, error) {
	return p.pool, nil
}
func (p *bootstrapProtocol) SavePool(_ context.Context, pool *bootstrapPool) error {
	p.pool = pool
	return nil
}
func (p *bootstrapProtocol) ReadChainData(context.Context, int, uint64) (int, error) {
	p.readCalls++
	return 1, nil
}
func (p *bootstrapProtocol) NewPoolFromChain(int, int) (*bootstrapPool, error) {
	return &bootstrapPool{}, nil
}
func (p *bootstrapProtocol) ApplyChainData(pool *bootstrapPool, _ int, blockNumber uint64) {
	p.applyCalls++
	pool.lastBlock = blockNumber
}
func (p *bootstrapProtocol) IsInitialized(pool *bootstrapPool) bool { return pool.initialized }
func (p *bootstrapProtocol) PoolLastBlock(pool *bootstrapPool) uint64 {
	return pool.lastBlock
}
func (p *bootstrapProtocol) SetStatus(pool *bootstrapPool, status market.PoolStatus) {
	pool.status = status
}

func TestBootstrapAppliesChainDataOnlyOnceForNewPool(t *testing.T) {
	protocol := &bootstrapProtocol{}
	service := syncapp.NewBootstrapService[int, *bootstrapPool, int](1000, protocol)

	pool, err := service.Bootstrap(context.Background(), 1, 100)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if protocol.readCalls != 1 {
		t.Fatalf("expected one chain read, got %d", protocol.readCalls)
	}
	if protocol.applyCalls != 1 {
		t.Fatalf("expected one chain apply, got %d", protocol.applyCalls)
	}
	if pool.status != market.PoolStatusCatchingUp {
		t.Fatalf("expected catching_up status, got %s", pool.status)
	}
}

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
