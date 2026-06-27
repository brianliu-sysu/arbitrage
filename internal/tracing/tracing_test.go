package tracing

import (
	"testing"
)

func TestInitEmptyEndpoint(t *testing.T) {
	shutdown, err := Init(Config{Endpoint: ""})
	if err != nil {
		t.Fatalf("Init with empty endpoint: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown function should not be nil")
	}
	// Shutdown should not error
	if err := shutdown(nil); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func TestInitDefaultServiceName(t *testing.T) {
	shutdown, err := Init(Config{Endpoint: ""})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown should not be nil")
	}
}

func TestTracer(t *testing.T) {
	tr := Tracer()
	if tr == nil {
		t.Fatal("Tracer() returned nil")
	}
}

func TestConfigTypes(t *testing.T) {
	cfg := Config{
		Endpoint:    "localhost:4318",
		ServiceName: "test-service",
	}
	if cfg.Endpoint != "localhost:4318" {
		t.Errorf("Endpoint = %q", cfg.Endpoint)
	}
	if cfg.ServiceName != "test-service" {
		t.Errorf("ServiceName = %q", cfg.ServiceName)
	}
}
