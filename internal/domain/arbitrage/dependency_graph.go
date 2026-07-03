package arbitrage

import (
	"sort"

	domainquote "github.com/brianliu-sysu/uniswapv3/internal/domain/quote"
	"github.com/ethereum/go-ethereum/common"
)

// RouteRef indexes a monitored route and its pool dependencies.
type RouteRef struct {
	ID    string
	Route domainquote.Route
}

// Pools returns the unique pool addresses used by the route.
func (r RouteRef) Pools() []common.Address {
	if len(r.Route.Hops) == 0 {
		return nil
	}

	seen := make(map[common.Address]struct{}, len(r.Route.Hops))
	pools := make([]common.Address, 0, len(r.Route.Hops))
	for _, hop := range r.Route.Hops {
		if _, ok := seen[hop.PoolAddress]; ok {
			continue
		}
		seen[hop.PoolAddress] = struct{}{}
		pools = append(pools, hop.PoolAddress)
	}
	sort.Slice(pools, func(i, j int) bool {
		return pools[i].Hex() < pools[j].Hex()
	})
	return pools
}

// DependencyGraph tracks which routes are affected when pool state changes.
type DependencyGraph struct {
	routes       map[string]RouteRef
	poolToRoutes map[common.Address]map[string]struct{}
}

func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		routes:       make(map[string]RouteRef),
		poolToRoutes: make(map[common.Address]map[string]struct{}),
	}
}

// Register adds or replaces a route in the graph.
func (g *DependencyGraph) Register(route RouteRef) {
	if route.ID == "" {
		return
	}

	if existing, ok := g.routes[route.ID]; ok {
		for _, pool := range existing.Pools() {
			if ids, ok := g.poolToRoutes[pool]; ok {
				delete(ids, route.ID)
				if len(ids) == 0 {
					delete(g.poolToRoutes, pool)
				}
			}
		}
	}

	g.routes[route.ID] = route
	for _, pool := range route.Pools() {
		if _, ok := g.poolToRoutes[pool]; !ok {
			g.poolToRoutes[pool] = make(map[string]struct{})
		}
		g.poolToRoutes[pool][route.ID] = struct{}{}
	}
}

// Remove deletes a route from the graph.
func (g *DependencyGraph) Remove(routeID string) {
	route, ok := g.routes[routeID]
	if !ok {
		return
	}
	for _, pool := range route.Pools() {
		if ids, ok := g.poolToRoutes[pool]; ok {
			delete(ids, routeID)
			if len(ids) == 0 {
				delete(g.poolToRoutes, pool)
			}
		}
	}
	delete(g.routes, routeID)
}

// AffectedRoutes returns routes that depend on any of the changed pools.
func (g *DependencyGraph) AffectedRoutes(changedPools []common.Address) []RouteRef {
	if len(changedPools) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	routes := make([]RouteRef, 0)
	for _, pool := range changedPools {
		for routeID := range g.poolToRoutes[pool] {
			if _, ok := seen[routeID]; ok {
				continue
			}
			route, ok := g.routes[routeID]
			if !ok {
				continue
			}
			seen[routeID] = struct{}{}
			routes = append(routes, route)
		}
	}

	sort.Slice(routes, func(i, j int) bool {
		return routes[i].ID < routes[j].ID
	})
	return routes
}

// Routes returns all registered routes sorted by id.
func (g *DependencyGraph) Routes() []RouteRef {
	routes := make([]RouteRef, 0, len(g.routes))
	for _, route := range g.routes {
		routes = append(routes, route)
	}
	sort.Slice(routes, func(i, j int) bool {
		return routes[i].ID < routes[j].ID
	})
	return routes
}
