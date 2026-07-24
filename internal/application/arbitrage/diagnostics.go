package arbitrageapp

import (
	"context"
	"fmt"

	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"go.uber.org/zap"
)

// Diagnostics summarizes arbitrage scanner state for operators.
type Diagnostics struct {
	Routes         int
	StartTokens    int
	GraphEdges     int
	ArbitrageReady bool
	RefreshError   string
}

// CollectDiagnostics gathers the current arbitrage scanner state.
func (s *Services) CollectDiagnostics(ctx context.Context) Diagnostics {
	d := Diagnostics{
		Routes:      len(s.Scan.Routes()),
		StartTokens: len(s.StartTokens()),
	}
	if s.readiness != nil {
		d.ArbitrageReady = s.readiness.IsSystemReady()
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
