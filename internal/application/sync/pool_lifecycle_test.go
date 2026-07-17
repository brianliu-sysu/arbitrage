package syncapp

import (
	"context"
	"testing"
)

func TestNilPoolLifecycleListIsSafe(t *testing.T) {
	var lifecycle *PoolLifecycleService[int]

	if active := lifecycle.ListActive(); active != nil {
		t.Fatalf("expected nil active pools, got %v", active)
	}
	ids, err := lifecycle.List(context.Background())
	if err != nil {
		t.Fatalf("list nil lifecycle: %v", err)
	}
	if ids != nil {
		t.Fatalf("expected nil pool IDs, got %v", ids)
	}
}

func TestStartAllRegistersTrackedPoolsBeforeActivate(t *testing.T) {
	registered := make([]int, 0, 2)
	readiness := NewReadinessService[int]()
	lifecycle := NewPoolLifecycleService(readiness, LifecycleHooks[int]{
		ListTracked: func(context.Context) ([]int, error) {
			return []int{3, 5}, nil
		},
		Register: func(_ context.Context, poolID int) error {
			registered = append(registered, poolID)
			return nil
		},
		BootstrapAll: func(context.Context, []int, uint64) error {
			if len(registered) != 2 {
				t.Fatalf("expected register before bootstrap, got %v", registered)
			}
			return nil
		},
	})

	if err := lifecycle.StartAll(context.Background(), 10); err != nil {
		t.Fatalf("start all: %v", err)
	}
	if len(registered) != 2 || registered[0] != 3 || registered[1] != 5 {
		t.Fatalf("unexpected registered pools %v", registered)
	}
	active := lifecycle.ListActive()
	if len(active) != 2 {
		t.Fatalf("expected 2 active pools, got %v", active)
	}
}
