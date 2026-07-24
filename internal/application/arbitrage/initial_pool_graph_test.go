package arbitrageapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type testPoolEdgeSource struct {
	name  string
	edges []quoteunified.PoolEdge
	err   error
}

func (s *testPoolEdgeSource) Name() string { return s.name }

func (s *testPoolEdgeSource) LoadEdges(context.Context) ([]quoteunified.PoolEdge, error) {
	return s.edges, s.err
}

func TestBuildUnifiedPoolGraphReportsNoPoolsAvailable(t *testing.T) {
	graph, err := BuildUnifiedPoolGraph(context.Background())
	if graph != nil {
		t.Fatalf("expected no graph, got %T", graph)
	}
	if !errors.Is(err, ErrNoPoolsAvailable) {
		t.Fatalf("expected ErrNoPoolsAvailable, got %v", err)
	}
}

func TestBuildUnifiedPoolGraphAggregatesSources(t *testing.T) {
	graph, err := BuildUnifiedPoolGraph(context.Background(), &testPoolEdgeSource{
		name: "test",
		edges: []quoteunified.PoolEdge{{
			Version: quoteunified.PoolVersionV3,
			PoolV3:  common.HexToAddress("0x1"),
			Token0:  common.HexToAddress("0x2"),
			Token1:  common.HexToAddress("0x3"),
		}},
	})
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	edgeGraph, ok := graph.(interface {
		Edges() []quoteunified.PoolEdge
	})
	if !ok || len(edgeGraph.Edges()) != 1 {
		t.Fatalf("expected one aggregated edge, got %T", graph)
	}
}

func TestBuildUnifiedPoolGraphNamesFailingSource(t *testing.T) {
	sourceErr := errors.New("load failed")
	_, err := BuildUnifiedPoolGraph(context.Background(), &testPoolEdgeSource{
		name: "broken",
		err:  sourceErr,
	})
	if !errors.Is(err, sourceErr) || !strings.Contains(err.Error(), "load broken pool graph edges") {
		t.Fatalf("expected named source error, got %v", err)
	}
}

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
