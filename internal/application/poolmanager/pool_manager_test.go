package poolmanager_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/application/poolmanager"
)

type testSyncOnboarder struct {
	calls []int
}

func (s *testSyncOnboarder) AddPool(_ context.Context, poolID int) error {
	s.calls = append(s.calls, poolID)
	return nil
}

type testArbitrageRefresher struct {
	calls int
}

func (r *testArbitrageRefresher) RefreshArbitrageRoutes(context.Context) (int, error) {
	r.calls++
	return 12, nil
}

func TestPoolManagerAddPoolRunsSyncBeforeArbitrageRefresh(t *testing.T) {
	sync := &testSyncOnboarder{}
	arbitrage := &testArbitrageRefresher{}
	manager := poolmanager.NewPoolManager[int](sync, arbitrage)

	result, err := manager.AddPool(context.Background(), 7)
	if err != nil {
		t.Fatalf("add pool: %v", err)
	}
	if !reflect.DeepEqual(sync.calls, []int{7}) {
		t.Fatalf("expected sync add call [7], got %v", sync.calls)
	}
	if arbitrage.calls != 1 {
		t.Fatalf("expected one arbitrage refresh, got %d", arbitrage.calls)
	}
	if result.Routes != 12 {
		t.Fatalf("expected 12 refreshed routes, got %d", result.Routes)
	}
}
