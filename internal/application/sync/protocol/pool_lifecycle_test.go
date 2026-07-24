package protocol

import (
	"context"
	"testing"
)

type batchLifecycleTestProtocol struct {
	t          *testing.T
	registered []int
}

func (p *batchLifecycleTestProtocol) Bootstrap(context.Context, int, uint64) error {
	return nil
}

func (p *batchLifecycleTestProtocol) BootstrapAll(context.Context, []int, uint64) error {
	if len(p.registered) != 2 {
		p.t.Fatalf("expected register before bootstrap, got %v", p.registered)
	}
	return nil
}

func (p *batchLifecycleTestProtocol) ListTracked(context.Context) ([]int, error) {
	return []int{3, 5}, nil
}

func (p *batchLifecycleTestProtocol) Register(_ context.Context, poolID int) error {
	p.registered = append(p.registered, poolID)
	return nil
}

func (p *batchLifecycleTestProtocol) Unregister(context.Context, int) error {
	return nil
}

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
	protocol := &batchLifecycleTestProtocol{t: t, registered: make([]int, 0, 2)}
	readiness := NewReadinessService[int]()
	lifecycle := NewPoolLifecycleService(readiness, protocol)

	if err := lifecycle.StartAll(context.Background(), 10); err != nil {
		t.Fatalf("start all: %v", err)
	}
	if len(protocol.registered) != 2 || protocol.registered[0] != 3 || protocol.registered[1] != 5 {
		t.Fatalf("unexpected registered pools %v", protocol.registered)
	}
	active := lifecycle.ListActive()
	if len(active) != 2 {
		t.Fatalf("expected 2 active pools, got %v", active)
	}
}
