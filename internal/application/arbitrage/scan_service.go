package arbitrageapp

import (
	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	domainquote "github.com/brianliu-sysu/uniswapv3/internal/domain/quote"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// ScanService finds routes affected by pool state changes.
type ScanService struct {
	graph *domainarb.DependencyGraph
}

func NewScanService(graph *domainarb.DependencyGraph) *ScanService {
	if graph == nil {
		graph = domainarb.NewDependencyGraph()
	}
	return &ScanService{graph: graph}
}

// RegisterRoutes adds monitored routes to the dependency graph.
func (s *ScanService) RegisterRoutes(routes []domainarb.RouteRef) {
	for _, route := range routes {
		s.graph.Register(route)
	}
}

// RegisterRoute adds a single monitored route.
func (s *ScanService) RegisterRoute(route domainarb.RouteRef) {
	s.graph.Register(route)
}

// FindAffected returns routes that depend on any changed Uniswap V3, Pancake V3, or V4 pool.
func (s *ScanService) FindAffected(univ3Pools, pancakePools []common.Address, univ4Pools []marketuniv4.PoolID) []domainarb.RouteRef {
	return s.graph.AffectedRoutes(univ3Pools, pancakePools, univ4Pools)
}

// Routes returns all registered routes.
func (s *ScanService) Routes() []domainarb.RouteRef {
	return s.graph.Routes()
}

// RegisterUnifiedTriangleRoutes discovers and registers A->B->C->A routes on a unified graph.
func (s *ScanService) RegisterUnifiedTriangleRoutes(graph quoteunified.PoolGraph, startToken common.Address) int {
	routes := domainarb.FindUnifiedTriangleRoutes(graph, startToken)
	for _, route := range routes {
		s.RegisterRoute(domainarb.RouteRef{
			ID:    domainarb.UnifiedTriangleRouteIDWithPools(route),
			Route: route,
		})
	}
	return len(routes)
}

// RegisterTriangleRoutes discovers and registers triangle routes on a V3-only graph.
func (s *ScanService) RegisterTriangleRoutes(graph domainquote.PoolGraph, startToken common.Address) int {
	return s.RegisterUnifiedTriangleRoutes(domainarb.V3GraphToUnified(graph), startToken)
}
