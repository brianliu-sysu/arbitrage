package arbitrageapp

import (
	"sync"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	domainquote "github.com/brianliu-sysu/uniswapv3/internal/domain/quote"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

// ScanService finds routes affected by pool state changes.
type ScanService struct {
	mu               sync.Mutex
	graph            *domainarb.DependencyGraph
	triangleRouteIDs []string
	spreadRouteIDs   []string
}

func NewScanService(graph *domainarb.DependencyGraph) *ScanService {
	if graph == nil {
		graph = domainarb.NewDependencyGraph()
	}
	return &ScanService{graph: graph}
}

// RegisterRoutes adds monitored routes to the dependency graph.
func (s *ScanService) RegisterRoutes(routes []domainarb.RouteRef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, route := range routes {
		s.graph.Register(route)
	}
}

// RegisterRoute adds a single monitored route.
func (s *ScanService) RegisterRoute(route domainarb.RouteRef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.graph.Register(route)
}

// ClearTriangleRoutes removes previously registered triangle routes.
func (s *ScanService) ClearTriangleRoutes() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, routeID := range s.triangleRouteIDs {
		s.graph.Remove(routeID)
	}
	s.triangleRouteIDs = nil
}

// ClearSpreadRoutes removes previously registered spread routes.
func (s *ScanService) ClearSpreadRoutes() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, routeID := range s.spreadRouteIDs {
		s.graph.Remove(routeID)
	}
	s.spreadRouteIDs = nil
}

// ClearMonitoredRoutes removes all auto-registered triangle and spread routes.
func (s *ScanService) ClearMonitoredRoutes() {
	s.ClearTriangleRoutes()
	s.ClearSpreadRoutes()
}

// FindAffected returns routes that depend on any changed synced pool.
func (s *ScanService) FindAffected(univ3Pools, pancakePools, quickSwapPools []common.Address, univ4Pools []marketuniv4.PoolID, balancerPoolsArg ...[]marketbalancer.PoolID) []domainarb.RouteRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	var balancerPools []marketbalancer.PoolID
	if len(balancerPoolsArg) > 0 {
		balancerPools = balancerPoolsArg[0]
	}
	return s.graph.AffectedRoutes(univ3Pools, pancakePools, quickSwapPools, univ4Pools, balancerPools)
}

// Routes returns all registered routes.
func (s *ScanService) Routes() []domainarb.RouteRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.graph.Routes()
}

// RegisterUnifiedTriangleRoutes discovers and registers A->B->C->A routes on a unified graph.
func (s *ScanService) RegisterUnifiedTriangleRoutes(graph quoteunified.PoolGraph, startToken common.Address) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	routes := domainarb.FindUnifiedTriangleRoutes(graph, startToken)
	registered := make([]string, 0, len(routes))
	for _, route := range routes {
		routeRef := domainarb.RouteRef{
			ID:    domainarb.UnifiedTriangleRouteIDWithPools(route),
			Route: route,
		}
		s.graph.Register(routeRef)
		registered = append(registered, routeRef.ID)
	}

	s.triangleRouteIDs = append(s.triangleRouteIDs, registered...)
	return len(registered)
}

// RegisterTriangleRoutes discovers and registers triangle routes on a V3-only graph.
func (s *ScanService) RegisterTriangleRoutes(graph domainquote.PoolGraph, startToken common.Address) int {
	return s.RegisterUnifiedTriangleRoutes(domainarb.V3GraphToUnified(graph), startToken)
}

// RegisterUnifiedSpreadRoutes discovers and registers A->B->A spread routes on a unified graph.
func (s *ScanService) RegisterUnifiedSpreadRoutes(graph quoteunified.PoolGraph, startToken common.Address) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	routes := domainarb.FindUnifiedSpreadRoutes(graph, startToken)
	registered := make([]string, 0, len(routes))
	for _, route := range routes {
		routeRef := domainarb.RouteRef{
			ID:    domainarb.UnifiedSpreadRouteIDWithPools(route),
			Route: route,
		}
		s.graph.Register(routeRef)
		registered = append(registered, routeRef.ID)
	}

	s.spreadRouteIDs = append(s.spreadRouteIDs, registered...)
	return len(registered)
}
