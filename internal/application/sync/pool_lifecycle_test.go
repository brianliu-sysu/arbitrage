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
