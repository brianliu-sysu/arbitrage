package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

// BalancerSubgraphRegistry loads weighted/stable pools from a Balancer subgraph.
type BalancerSubgraphRegistry struct {
	cfg          config.BalancerSubgraphPoolConfig
	defaultVault common.Address
	client       *http.Client
	clock        func() time.Time

	mu        sync.RWMutex
	cached    []balancerPoolEntry
	lastFetch time.Time
	added     map[marketbalancer.PoolID]balancerPoolEntry
	removed   map[marketbalancer.PoolID]struct{}
}

func NewBalancerSubgraphRegistry(cfg config.BalancerSubgraphPoolConfig, defaultVault common.Address) *BalancerSubgraphRegistry {
	if cfg.First <= 0 {
		cfg.First = 100
	}
	if cfg.OrderBy == "" {
		cfg.OrderBy = "totalLiquidity"
	}
	if cfg.OrderDirection == "" {
		cfg.OrderDirection = "desc"
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = 10 * time.Minute
	}
	return &BalancerSubgraphRegistry{
		cfg:          cfg,
		defaultVault: defaultVault,
		client:       &http.Client{Timeout: defaultGraphQLTimeout},
		clock:        time.Now,
		added:        make(map[marketbalancer.PoolID]balancerPoolEntry),
		removed:      make(map[marketbalancer.PoolID]struct{}),
	}
}

func (r *BalancerSubgraphRegistry) List(ctx context.Context) ([]balancerPoolEntry, error) {
	if r.cfg.IsEnabled() {
		if err := r.refreshIfNeeded(ctx); err != nil {
			return nil, err
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentEntriesLocked(), nil
}

func (r *BalancerSubgraphRegistry) GetSpec(_ context.Context, id marketbalancer.PoolID) (marketbalancer.PoolSpec, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if entry, ok := r.lookupLocked(id); ok {
		return entry.spec, true, nil
	}
	return marketbalancer.PoolSpec{}, false, nil
}

func (r *BalancerSubgraphRegistry) Add(_ context.Context, id marketbalancer.PoolID, spec marketbalancer.PoolSpec) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.removed, id)
	r.added[id] = balancerPoolEntry{id: id, spec: spec}
	return nil
}

func (r *BalancerSubgraphRegistry) Remove(_ context.Context, id marketbalancer.PoolID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.added, id)
	r.removed[id] = struct{}{}
	return nil
}

func (r *BalancerSubgraphRegistry) refreshIfNeeded(ctx context.Context) error {
	r.mu.RLock()
	stale := r.lastFetch.IsZero() || r.clock().Sub(r.lastFetch) >= r.cfg.RefreshInterval
	r.mu.RUnlock()
	if !stale {
		return nil
	}
	return r.fetch(ctx)
}

func (r *BalancerSubgraphRegistry) fetch(ctx context.Context) error {
	pools, err := r.queryPools(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cached = pools
	r.lastFetch = r.clock()
	return nil
}

func (r *BalancerSubgraphRegistry) lookupLocked(id marketbalancer.PoolID) (balancerPoolEntry, bool) {
	for _, entry := range r.cached {
		if entry.id == id {
			return entry, true
		}
	}
	if entry, ok := r.added[id]; ok {
		return entry, true
	}
	return balancerPoolEntry{}, false
}

func (r *BalancerSubgraphRegistry) currentEntriesLocked() []balancerPoolEntry {
	seen := make(map[marketbalancer.PoolID]struct{}, len(r.cached)+len(r.added))
	entries := make([]balancerPoolEntry, 0, len(r.cached)+len(r.added))
	for _, entry := range r.cached {
		if _, removed := r.removed[entry.id]; removed {
			continue
		}
		if _, ok := seen[entry.id]; ok {
			continue
		}
		seen[entry.id] = struct{}{}
		entries = append(entries, entry)
	}
	for id, entry := range r.added {
		if _, removed := r.removed[id]; removed {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		entries = append(entries, entry)
	}
	sortBalancerPoolEntries(entries)
	return entries
}

func (r *BalancerSubgraphRegistry) queryPools(ctx context.Context) ([]balancerPoolEntry, error) {
	query, variables, err := r.buildQuery()
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return nil, fmt.Errorf("marshal graphql request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create graphql request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query balancer subgraph: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read balancer subgraph response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("balancer subgraph status %d: %s", resp.StatusCode, string(raw))
	}

	var response balancerGraphQLResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("decode balancer subgraph response: %w", err)
	}
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("balancer subgraph graphql error: %s", response.Errors[0].Message)
	}
	if response.Data.Pools == nil {
		return nil, fmt.Errorf("balancer subgraph response missing pools")
	}

	entries := make([]balancerPoolEntry, 0, len(response.Data.Pools))
	for i, pool := range response.Data.Pools {
		entry, err := r.poolEntryFromSubgraph(pool, i)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (r *BalancerSubgraphRegistry) buildQuery() (string, map[string]any, error) {
	orderBy := strings.TrimSpace(r.cfg.OrderBy)
	switch orderBy {
	case "totalLiquidity", "totalSwapVolume", "totalSwapFee", "swapCount", "createTime":
	default:
		return "", nil, fmt.Errorf("unsupported balancer subgraph order_by %q", orderBy)
	}
	orderDirection := strings.ToLower(strings.TrimSpace(r.cfg.OrderDirection))
	if _, ok := allowedOrderDirection[orderDirection]; !ok {
		return "", nil, fmt.Errorf("unsupported balancer subgraph order_direction %q", orderDirection)
	}

	where := r.buildPoolWhereFilter()
	variables := map[string]any{
		"first":          r.cfg.First,
		"skip":           r.cfg.Skip,
		"orderBy":        orderBy,
		"orderDirection": orderDirection,
	}
	if len(where) > 0 {
		variables["where"] = where
	}

	query := `
query Pools($first: Int!, $skip: Int!, $orderBy: Pool_orderBy!, $orderDirection: OrderDirection!, $where: Pool_filter) {
  pools(
    first: $first
    skip: $skip
    orderBy: $orderBy
    orderDirection: $orderDirection
    where: $where
  ) {
    id
    address
    poolType
  }
}`
	return query, variables, nil
}

func (r *BalancerSubgraphRegistry) buildPoolWhereFilter() map[string]any {
	where := make(map[string]any)
	if r.cfg.MinTotalValueLockedUSD != "" {
		where["totalLiquidity_gt"] = r.cfg.MinTotalValueLockedUSD
	}
	poolTypes := make([]string, 0, len(r.cfg.ResolvedPoolTypes()))
	for _, poolType := range r.cfg.ResolvedPoolTypes() {
		poolType = strings.TrimSpace(poolType)
		if poolType == "" {
			continue
		}
		poolTypes = append(poolTypes, poolType)
	}
	if len(poolTypes) > 0 {
		where["poolType_in"] = poolTypes
	}
	return where
}

func (r *BalancerSubgraphRegistry) poolEntryFromSubgraph(pool balancerSubgraphPool, index int) (balancerPoolEntry, error) {
	if pool.ID == "" {
		return balancerPoolEntry{}, fmt.Errorf("balancer subgraph pool[%d] missing id", index)
	}
	poolType, err := balancerPoolTypeFromSubgraph(pool.PoolType)
	if err != nil {
		return balancerPoolEntry{}, fmt.Errorf("balancer subgraph pool[%d]: %w", index, err)
	}
	return balancerPoolEntry{
		id: marketbalancer.PoolID(common.HexToHash(pool.ID)),
		spec: marketbalancer.PoolSpec{
			Address: common.HexToAddress(pool.Address),
			Vault:   r.defaultVault,
			Type:    poolType,
		},
	}, nil
}

func balancerPoolTypeFromSubgraph(value string) (marketbalancer.PoolType, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "weighted", "weightedpool":
		return marketbalancer.PoolTypeWeighted, nil
	case "stable", "stablepool", "metastable", "metastablepool":
		return marketbalancer.PoolTypeStable, nil
	default:
		return "", fmt.Errorf("unsupported pool type %q", value)
	}
}

type balancerGraphQLResponse struct {
	Data struct {
		Pools []balancerSubgraphPool `json:"pools"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type balancerSubgraphPool struct {
	ID       string `json:"id"`
	Address  string `json:"address"`
	PoolType string `json:"poolType"`
}
