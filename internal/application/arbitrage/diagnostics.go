package arbitrageapp

import (
	"context"
	"fmt"

	quotecombined "github.com/brianliu-sysu/uniswapv3/internal/application/quote/combined"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"go.uber.org/zap"
)

// Diagnostics summarizes arbitrage scanner state for operators.
type Diagnostics struct {
	Routes         int
	StartTokens    int
	GraphEdges     int
	ArbitrageReady bool
	Readiness      quotecombined.ReadinessDiagnostics
	RefreshError   string
}

// CollectDiagnostics gathers the current arbitrage scanner state.
func (s *Services) CollectDiagnostics(ctx context.Context) Diagnostics {
	d := Diagnostics{
		Routes:      len(s.Scan.Routes()),
		StartTokens: len(s.StartTokens()),
	}
	if s.readiness != nil {
		if combined, ok := s.readiness.(*quotecombined.SyncReadiness); ok {
			d.Readiness = combined.Diagnostics()
			d.ArbitrageReady = d.Readiness.ArbitrageReady
		}
	}
	if edges, err := s.countGraphEdges(ctx); err != nil {
		d.RefreshError = err.Error()
	} else {
		d.GraphEdges = edges
	}
	return d
}

// LogDiagnostics writes arbitrage scanner state to the logger.
func (s *Services) LogDiagnostics(ctx context.Context, logger *zap.Logger, event string) {
	if logger == nil {
		return
	}
	d := s.CollectDiagnostics(ctx)
	fields := []zap.Field{
		zap.String("event", event),
		zap.Int("routes", d.Routes),
		zap.Int("start_tokens", d.StartTokens),
		zap.Int("graph_edges", d.GraphEdges),
		zap.Bool("arbitrage_ready", d.ArbitrageReady),
		zap.Bool("univ3_system_ready", d.Readiness.V3.SystemReady),
		zap.Int("univ3_ready_pools", d.Readiness.V3.ReadyPools),
		zap.Int("univ3_total_pools", d.Readiness.V3.TotalPools),
	}
	if d.Readiness.Pancake.Enabled {
		fields = append(fields,
			zap.Bool("pancakev3_system_ready", d.Readiness.Pancake.SystemReady),
			zap.Int("pancakev3_ready_pools", d.Readiness.Pancake.ReadyPools),
			zap.Int("pancakev3_total_pools", d.Readiness.Pancake.TotalPools),
		)
	}
	if d.Readiness.V4.Enabled {
		fields = append(fields,
			zap.Bool("univ4_system_ready", d.Readiness.V4.SystemReady),
			zap.Int("univ4_ready_pools", d.Readiness.V4.ReadyPools),
			zap.Int("univ4_total_pools", d.Readiness.V4.TotalPools),
		)
	}
	if d.RefreshError != "" {
		fields = append(fields, zap.String("graph_error", d.RefreshError))
	}
	logger.Info("arbitrage diagnostics", fields...)
}

func (s *Services) countGraphEdges(ctx context.Context) (int, error) {
	graph, err := BuildUnifiedPoolGraph(
		ctx,
		poolEdgeSources(routeRefreshDepsToServiceDeps(s.routeDeps))...,
	)
	if err != nil {
		return 0, err
	}
	if edgeGraph, ok := graph.(interface {
		Edges() []quoteunified.PoolEdge
	}); ok {
		return len(edgeGraph.Edges()), nil
	}
	return 0, fmt.Errorf("pool graph does not expose edges")
}
