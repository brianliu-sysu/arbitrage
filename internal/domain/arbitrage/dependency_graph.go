package arbitrage

import (
	"sort"

	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// RouteRef indexes a monitored route and its pool dependencies.
type RouteRef struct {
	ID    string
	Route quoteunified.Route
}

// Pools returns the unique pool refs used by the route.
func (r RouteRef) Pools() []PoolRef {
	if len(r.Route.Hops) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(r.Route.Hops))
	pools := make([]PoolRef, 0, len(r.Route.Hops))
	for _, hop := range r.Route.Hops {
		ref := PoolRefFromHop(hop)
		if ref.Key() == "" {
			continue
		}
		if _, ok := seen[ref.Key()]; ok {
			continue
		}
		seen[ref.Key()] = struct{}{}
		pools = append(pools, ref)
	}
	sort.Slice(pools, func(i, j int) bool {
		return pools[i].Key() < pools[j].Key()
	})
	return pools
}

// DependencyGraph tracks which routes are affected when pool state changes.
type DependencyGraph struct {
	routes       map[string]RouteRef
	poolToRoutes map[string]map[string]struct{}
}

func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		routes:       make(map[string]RouteRef),
		poolToRoutes: make(map[string]map[string]struct{}),
	}
}

// Register adds or replaces a route in the graph.
func (g *DependencyGraph) Register(route RouteRef) {
	if route.ID == "" {
		return
	}

	if existing, ok := g.routes[route.ID]; ok {
		for _, pool := range existing.Pools() {
			if ids, ok := g.poolToRoutes[pool.Key()]; ok {
				delete(ids, route.ID)
				if len(ids) == 0 {
					delete(g.poolToRoutes, pool.Key())
				}
			}
		}
	}

	g.routes[route.ID] = route
	for _, pool := range route.Pools() {
		key := pool.Key()
		if key == "" {
			continue
		}
		if _, ok := g.poolToRoutes[key]; !ok {
			g.poolToRoutes[key] = make(map[string]struct{})
		}
		g.poolToRoutes[key][route.ID] = struct{}{}
	}
}

// Remove deletes a route from the graph.
func (g *DependencyGraph) Remove(routeID string) {
	route, ok := g.routes[routeID]
	if !ok {
		return
	}
	for _, pool := range route.Pools() {
		if ids, ok := g.poolToRoutes[pool.Key()]; ok {
			delete(ids, routeID)
			if len(ids) == 0 {
				delete(g.poolToRoutes, pool.Key())
			}
		}
	}
	delete(g.routes, routeID)
}

// AffectedRoutes returns routes that depend on any of the changed pools.
func (g *DependencyGraph) AffectedRoutes(v3Pools, pancakePools []common.Address, v4Pools []marketv4.PoolID) []RouteRef {
	if len(v3Pools) == 0 && len(pancakePools) == 0 && len(v4Pools) == 0 {
		return nil
	}

	changedKeys := make([]string, 0, len(v3Pools)+len(pancakePools)+len(v4Pools))
	for _, pool := range v3Pools {
		changedKeys = append(changedKeys, PoolRefFromV3(pool).Key())
	}
	for _, pool := range pancakePools {
		changedKeys = append(changedKeys, PoolRefFromPancakeV3(pool).Key())
	}
	for _, pool := range v4Pools {
		changedKeys = append(changedKeys, PoolRefFromV4(pool).Key())
	}

	seen := make(map[string]struct{})
	routes := make([]RouteRef, 0)
	for _, key := range changedKeys {
		for routeID := range g.poolToRoutes[key] {
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
