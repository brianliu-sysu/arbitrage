package protocol

import (
	"context"
	"testing"
)

type batchAdmissionTestBackend struct {
	t          *testing.T
	registered []int
}

func (p *batchAdmissionTestBackend) Bootstrap(context.Context, int, uint64) error {
	return nil
}

func (p *batchAdmissionTestBackend) BootstrapAll(context.Context, []int, uint64) error {
	if len(p.registered) != 2 {
		p.t.Fatalf("expected register before bootstrap, got %v", p.registered)
	}
	return nil
}

func (p *batchAdmissionTestBackend) ListTracked(context.Context) ([]int, error) {
	return []int{3, 5}, nil
}

func (p *batchAdmissionTestBackend) Register(_ context.Context, poolID int) error {
	p.registered = append(p.registered, poolID)
	return nil
}

func (p *batchAdmissionTestBackend) Unregister(context.Context, int) error {
	return nil
}

func TestNilPoolAdmissionListActiveIsSafe(t *testing.T) {
	var admission *PoolAdmissionService[int]

	if active := admission.ListActive(); active != nil {
		t.Fatalf("expected nil active pools, got %v", active)
	}
}

func TestAdmitTrackedPoolsRegistersBeforeActivate(t *testing.T) {
	protocol := &batchAdmissionTestBackend{t: t, registered: make([]int, 0, 2)}
	readiness := NewReadinessService[int]()
	admission := NewPoolAdmissionService(readiness, protocol, protocol)

	if err := admission.AdmitTrackedPools(context.Background(), 10); err != nil {
		t.Fatalf("start all: %v", err)
	}
	if len(protocol.registered) != 2 || protocol.registered[0] != 3 || protocol.registered[1] != 5 {
		t.Fatalf("unexpected registered pools %v", protocol.registered)
	}
	active := admission.ListActive()
	if len(active) != 2 {
		t.Fatalf("expected 2 active pools, got %v", active)
	}
}
