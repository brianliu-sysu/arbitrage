package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	chainHeadNumber       = promauto.NewGauge(prometheus.GaugeOpts{Name: "arbitrage_chain_head_number", Help: "Latest observed chain head."})
	chainHeadTimestamp    = promauto.NewGauge(prometheus.GaugeOpts{Name: "arbitrage_chain_head_timestamp_seconds", Help: "Unix timestamp when the latest chain head was observed."})
	protocolLastApplied   = promauto.NewGaugeVec(prometheus.GaugeOpts{Name: "arbitrage_protocol_last_applied_block", Help: "Latest block applied by protocol."}, []string{"protocol"})
	protocolApplyDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{Name: "arbitrage_protocol_apply_duration_seconds", Help: "Protocol block apply duration."}, []string{"protocol"})
	protocolApplyErrors   = promauto.NewCounterVec(prometheus.CounterOpts{Name: "arbitrage_protocol_apply_errors_total", Help: "Protocol block apply errors."}, []string{"protocol"})
	barrierPending        = promauto.NewGauge(prometheus.GaugeOpts{Name: "arbitrage_barrier_pending_blocks", Help: "Blocks pending at the protocol barrier."})
	barrierLastFinalized  = promauto.NewGauge(prometheus.GaugeOpts{Name: "arbitrage_barrier_last_finalized_block", Help: "Latest finalized barrier block."})
	marketVersionBlock    = promauto.NewGauge(prometheus.GaugeOpts{Name: "arbitrage_market_version_block", Help: "Currently published market block."})
	marketPublish         = promauto.NewCounterVec(prometheus.CounterOpts{Name: "arbitrage_market_publish_total", Help: "Market snapshot publish attempts."}, []string{"result"})
	marketPublishDuration = promauto.NewHistogram(prometheus.HistogramOpts{Name: "arbitrage_market_publish_duration_seconds", Help: "Market snapshot publish duration."})
	marketPoolMismatch    = promauto.NewCounterVec(prometheus.CounterOpts{Name: "arbitrage_market_pool_block_mismatch_total", Help: "Pools not at the requested market block."}, []string{"protocol"})
	scanDuration          = promauto.NewHistogram(prometheus.HistogramOpts{Name: "arbitrage_scan_duration_seconds", Help: "Arbitrage scan duration."})
	scanAffectedRoutes    = promauto.NewHistogram(prometheus.HistogramOpts{Name: "arbitrage_scan_affected_routes", Help: "Routes affected per market scan."})
	opportunities         = promauto.NewCounterVec(prometheus.CounterOpts{Name: "arbitrage_opportunities_total", Help: "Generated arbitrage opportunities."}, []string{"strategy"})
	executions            = promauto.NewCounterVec(prometheus.CounterOpts{Name: "arbitrage_execution_total", Help: "Arbitrage execution outcomes."}, []string{"result"})
)

func ObserveChainHead(number uint64) {
	chainHeadNumber.Set(float64(number))
	chainHeadTimestamp.Set(float64(time.Now().Unix()))
}

func ObserveProtocolApply(protocol string, block uint64, started time.Time, err error) {
	protocolApplyDuration.WithLabelValues(protocol).Observe(time.Since(started).Seconds())
	if err != nil {
		protocolApplyErrors.WithLabelValues(protocol).Inc()
		return
	}
	protocolLastApplied.WithLabelValues(protocol).Set(float64(block))
}

func SetBarrier(pending int, finalized uint64) {
	barrierPending.Set(float64(pending))
	if finalized > 0 {
		barrierLastFinalized.Set(float64(finalized))
	}
}

func ObserveMarketPublish(block uint64, started time.Time, err error) {
	marketPublishDuration.Observe(time.Since(started).Seconds())
	if err != nil {
		marketPublish.WithLabelValues("error").Inc()
		return
	}
	marketPublish.WithLabelValues("success").Inc()
	marketVersionBlock.Set(float64(block))
}

func IncMarketPoolMismatch(protocol string) { marketPoolMismatch.WithLabelValues(protocol).Inc() }

func ObserveScan(started time.Time, affected int, generatedStrategies []string) {
	scanDuration.Observe(time.Since(started).Seconds())
	scanAffectedRoutes.Observe(float64(affected))
	for _, strategy := range generatedStrategies {
		opportunities.WithLabelValues(strategy).Inc()
	}
}

func IncExecution(result string) { executions.WithLabelValues(result).Inc() }
