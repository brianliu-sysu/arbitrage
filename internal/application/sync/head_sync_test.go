package syncapp_test

import (
	"context"
	"sync"
	"testing"
	"time"

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

func TestHeadSyncReconnectsAfterClosedSubscription(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	subscriber := &reconnectingHeadSubscriber{
		subscriptions: []chan blockchain.BlockHeader{
			closedHeadChannel(),
			bufferedHeadChannel(blockchain.BlockHeader{Number: 101, Hash: common.HexToHash("0x101")}),
		},
	}
	lifecycle := syncapp.NewPoolLifecycleService[int](
		syncapp.NewReadinessService[int](),
		syncapp.LifecycleHooks[int]{},
	)
	service := syncapp.NewHeadSyncService[int, struct{}](
		lifecycle,
		nil,
		nil,
		nil,
		subscriber,
		syncapp.HeadSyncHooks[int, struct{}]{
			FetchHeadLogs:  func(context.Context, []int, uint64) ([]syncapp.RawLog, error) { return nil, nil },
			ParseEvents:    func([]syncapp.RawLog) ([]struct{}, error) { return nil, nil },
			ApplyBlock:     func(context.Context, uint64, common.Hash, []struct{}, []int, bool) error { return nil },
			MarkPoolsReady: func(context.Context, []int) error { return nil },
		},
	)
	service.SetReconnectBackoff(time.Millisecond, time.Millisecond)

	errCh := make(chan error, 1)
	go func() {
		errCh <- service.Run(ctx)
	}()

	deadline := time.After(500 * time.Millisecond)
	for service.LocalHead().Number != 101 {
		select {
		case <-deadline:
			t.Fatalf("expected head 101 after reconnect, got %+v", service.LocalHead())
		case err := <-errCh:
			t.Fatalf("head sync stopped before reconnect: %v", err)
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	if err := <-errCh; err != context.Canceled {
		t.Fatalf("expected context canceled after stop, got %v", err)
	}
	if subscriber.calls() < 2 {
		t.Fatalf("expected at least two subscriptions, got %d", subscriber.calls())
	}
}

type reconnectingHeadSubscriber struct {
	mu            sync.Mutex
	subscriptions []chan blockchain.BlockHeader
	callCount     int
}

func (s *reconnectingHeadSubscriber) SubscribeNewHead(context.Context) (<-chan blockchain.BlockHeader, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callCount++
	idx := s.callCount - 1
	if idx >= len(s.subscriptions) {
		ch := make(chan blockchain.BlockHeader)
		return ch, nil
	}
	return s.subscriptions[idx], nil
}

func (s *reconnectingHeadSubscriber) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.callCount
}

func closedHeadChannel() chan blockchain.BlockHeader {
	ch := make(chan blockchain.BlockHeader)
	close(ch)
	return ch
}

func bufferedHeadChannel(head blockchain.BlockHeader) chan blockchain.BlockHeader {
	ch := make(chan blockchain.BlockHeader, 1)
	ch <- head
	return ch
}
