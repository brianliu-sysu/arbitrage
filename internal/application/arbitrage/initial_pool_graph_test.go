package arbitrageapp

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestNewServicesDefersEmptyInitialPoolGraphWithoutErrorLog(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)

	services := NewServices(ServiceDeps{Logger: zap.New(core)})
	if services == nil {
		t.Fatal("expected services")
	}
	if logs.FilterMessage("build initial arbitrage pool graph failed").Len() != 0 {
		t.Fatal("empty initial pool graph must not be logged as an error")
	}
	if logs.FilterMessage("initial arbitrage pool graph deferred until pool bootstrap").Len() != 1 {
		t.Fatal("expected deferred initial graph debug log")
	}
}
