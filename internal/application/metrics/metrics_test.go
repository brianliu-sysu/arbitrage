package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestDefaultRegistryContainsCoreArbitrageMetrics(t *testing.T) {
	started := time.Now()
	ObserveChainHead(1)
	ObserveProtocolApply("test", 1, started, nil)
	ObserveProtocolApply("test", 1, started, assertError{})
	SetBarrier(1, 1)
	ObserveMarketPublish(1, started, nil)
	IncMarketPoolMismatch("test")
	ObserveScan(started, 1, []string{"test"})
	IncExecution("test")

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	found := make(map[string]struct{}, len(families))
	for _, family := range families {
		found[family.GetName()] = struct{}{}
	}
	expected := []string{
		"arbitrage_chain_head_number",
		"arbitrage_chain_head_timestamp_seconds",
		"arbitrage_protocol_last_applied_block",
		"arbitrage_protocol_apply_duration_seconds",
		"arbitrage_protocol_apply_errors_total",
		"arbitrage_barrier_pending_blocks",
		"arbitrage_barrier_last_finalized_block",
		"arbitrage_market_version_block",
		"arbitrage_market_publish_total",
		"arbitrage_market_publish_duration_seconds",
		"arbitrage_market_pool_block_mismatch_total",
		"arbitrage_scan_duration_seconds",
		"arbitrage_scan_affected_routes",
		"arbitrage_opportunities_total",
		"arbitrage_execution_total",
	}
	for _, name := range expected {
		if _, ok := found[name]; !ok {
			t.Errorf("metric %s is not registered", name)
		}
	}
}

type assertError struct{}

func (assertError) Error() string { return "test" }
