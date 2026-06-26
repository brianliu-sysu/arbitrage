// Package metrics 提供 Prometheus 监控指标。
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// EventsTotal 收到的事件总数，按池子和事件类型分组。
	EventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "arbitrage_events_total",
		Help: "Total number of blockchain events received.",
	}, []string{"pool", "event"})

	// WSReconnectsTotal WebSocket 重连总次数，按池子分组。
	WSReconnectsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "arbitrage_ws_reconnects_total",
		Help: "Total number of WebSocket reconnections.",
	}, []string{"pool"})

	// QuotesTotal 报价请求总数，按池子和报价方法分组。
	//
	// method 取值: rpc_simulate, local_cross_tick, simple
	QuotesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "arbitrage_quotes_total",
		Help: "Total number of quote requests, by pool and method.",
	}, []string{"pool", "method"})

	// HealthRepairsTotal 健康检查触发状态同步的总次数。
	HealthRepairsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "arbitrage_health_repairs_total",
		Help: "Total number of health check state repairs.",
	}, []string{"pool"})

	// Price 当前现货价格（已调整小数位）。
	// 例：ETH/USDC 池 (tick=202669, token0=USDC 6dec, token1=WETH 18dec)
	//   = 10^(18-6) / 1.0001^202669 ≈ 1580 USDC per ETH
	Price = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "arbitrage_price",
		Help: "Current spot price with decimals adjustment (1 token1 in token0).",
	}, []string{"pool"})

	// BlockNumber 当前同步到的区块高度。
	BlockNumber = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "arbitrage_block_number",
		Help: "Current block number for each pool.",
	}, []string{"pool"})

	// EventLatencySeconds 事件延迟：事件到达时间与当前时间的差值（秒）。
	EventLatencySeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "arbitrage_event_latency_seconds",
		Help:    "Latency of event processing in seconds.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
	}, []string{"pool", "event"})

	// HTTPRequestDuration HTTP API 请求耗时。
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "arbitrage_http_request_duration_seconds",
		Help:    "HTTP API request duration in seconds.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"method", "path", "status"})
)
