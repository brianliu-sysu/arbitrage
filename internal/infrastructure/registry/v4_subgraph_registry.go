package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
	"github.com/ethereum/go-ethereum/common"
)

// V4SubgraphRegistry loads V4 pool IDs and keys from a Uniswap V4 subgraph.
type V4SubgraphRegistry struct {
	cfg    config.V4SubgraphPoolConfig
	client *http.Client
	clock  func() time.Time

	mu        sync.RWMutex
	cached    []v4PoolEntry
	lastFetch time.Time
	added     map[marketv4.PoolID]v4PoolEntry
	removed   map[marketv4.PoolID]struct{}
}

func NewV4SubgraphRegistry(cfg config.V4SubgraphPoolConfig) *V4SubgraphRegistry {
	if cfg.First <= 0 {
		cfg.First = 100
	}
	if cfg.OrderBy == "" {
		cfg.OrderBy = "volume24h"
	}
	if cfg.OrderDirection == "" {
		cfg.OrderDirection = "desc"
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = 10 * time.Minute
	}
	if len(cfg.Hooks) == 0 {
		cfg.Hooks = config.DefaultV4SubgraphHooks()
	}
	return &V4SubgraphRegistry{
		cfg:     cfg,
		client:  &http.Client{Timeout: defaultGraphQLTimeout},
		clock:   time.Now,
		added:   make(map[marketv4.PoolID]v4PoolEntry),
		removed: make(map[marketv4.PoolID]struct{}),
	}
}

func (r *V4SubgraphRegistry) List(ctx context.Context) ([]v4PoolEntry, error) {
	if r.cfg.IsEnabled() {
		if err := r.refreshIfNeeded(ctx); err != nil {
			return nil, err
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentEntriesLocked(), nil
}

func (r *V4SubgraphRegistry) GetKey(_ context.Context, id marketv4.PoolID) (marketv4.PoolKey, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if entry, ok := r.lookupLocked(id); ok {
		return entry.key, true, nil
	}
	return marketv4.PoolKey{}, false, nil
}

func (r *V4SubgraphRegistry) Add(_ context.Context, id marketv4.PoolID, key marketv4.PoolKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.removed, id)
	r.added[id] = v4PoolEntry{id: id, key: key}
	return nil
}

func (r *V4SubgraphRegistry) Remove(_ context.Context, id marketv4.PoolID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.added, id)
	r.removed[id] = struct{}{}
	return nil
}

func (r *V4SubgraphRegistry) refreshIfNeeded(ctx context.Context) error {
	r.mu.RLock()
	stale := r.lastFetch.IsZero() || r.clock().Sub(r.lastFetch) >= r.cfg.RefreshInterval
	r.mu.RUnlock()
	if !stale {
		return nil
	}
	return r.fetch(ctx)
}

func (r *V4SubgraphRegistry) fetch(ctx context.Context) error {
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

func (r *V4SubgraphRegistry) lookupLocked(id marketv4.PoolID) (v4PoolEntry, bool) {
	for _, entry := range r.cached {
		if entry.id == id {
			return entry, true
		}
	}
	if entry, ok := r.added[id]; ok {
		return entry, true
	}
	return v4PoolEntry{}, false
}

func (r *V4SubgraphRegistry) currentEntriesLocked() []v4PoolEntry {
	seen := make(map[marketv4.PoolID]struct{}, len(r.cached)+len(r.added))
	entries := make([]v4PoolEntry, 0, len(r.cached)+len(r.added))

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

	sortV4PoolEntries(entries)
	return entries
}

func (r *V4SubgraphRegistry) queryPools(ctx context.Context) ([]v4PoolEntry, error) {
	query, variables, err := r.buildQuery()
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(graphQLRequest{
		Query:     query,
		Variables: variables,
	})
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
		return nil, fmt.Errorf("query v4 subgraph: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read v4 subgraph response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("v4 subgraph status %d: %s", resp.StatusCode, string(raw))
	}

	var response v4GraphQLResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("decode v4 subgraph response: %w", err)
	}
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("v4 subgraph graphql error: %s", response.Errors[0].Message)
	}
	if response.Data.Pools == nil {
		return nil, fmt.Errorf("v4 subgraph response missing pools")
	}

	entries := make([]v4PoolEntry, 0, len(response.Data.Pools))
	for i, pool := range response.Data.Pools {
		entry, err := poolEntryFromSubgraph(pool, i)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (r *V4SubgraphRegistry) buildQuery() (string, map[string]any, error) {
	orderBy := strings.TrimSpace(r.cfg.OrderBy)
	switch orderBy {
	case "totalValueLockedUSD", "volumeUSD", "txCount", "feesUSD", "liquidity", "volume24h":
	default:
		return "", nil, fmt.Errorf("unsupported v4 subgraph order_by %q", orderBy)
	}
	orderDirection := strings.ToLower(strings.TrimSpace(r.cfg.OrderDirection))
	if _, ok := allowedOrderDirection[orderDirection]; !ok {
		return "", nil, fmt.Errorf("unsupported v4 subgraph order_direction %q", orderDirection)
	}

	where := r.buildPoolWhereFilter()
	subgraphOrderBy := orderBy
	if orderBy == "volume24h" {
		subgraphOrderBy = "volumeUSD"
	}

	variables := map[string]any{
		"first":          r.cfg.First,
		"skip":           r.cfg.Skip,
		"orderBy":        subgraphOrderBy,
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
    token0 { id }
    token1 { id }
    feeTier
    tickSpacing
    hooks
  }
}`
	return query, variables, nil
}

func (r *V4SubgraphRegistry) buildPoolWhereFilter() map[string]any {
	where := make(map[string]any)
	if r.cfg.MinTotalValueLockedUSD != "" {
		where["totalValueLockedUSD_gt"] = r.cfg.MinTotalValueLockedUSD
	}
	if token0 := strings.TrimSpace(r.cfg.Token0); token0 != "" {
		where["token0"] = strings.ToLower(token0)
	}
	if token1 := strings.TrimSpace(r.cfg.Token1); token1 != "" {
		where["token1"] = strings.ToLower(token1)
	}
	if len(r.cfg.FeeTiers) > 0 {
		where["feeTier_in"] = r.cfg.FeeTiers
	}
	hooks := make([]string, 0, len(r.cfg.ResolvedHooks()))
	for _, hook := range r.cfg.ResolvedHooks() {
		hook = strings.ToLower(strings.TrimSpace(hook))
		if hook == "" {
			continue
		}
		hooks = append(hooks, hook)
	}
	if len(hooks) > 0 {
		where["hooks_in"] = hooks
	}
	return where
}

type v4GraphQLResponse struct {
	Data struct {
		Pools []v4SubgraphPool `json:"pools"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type v4SubgraphPool struct {
	ID          string        `json:"id"`
	Token0      subgraphToken `json:"token0"`
	Token1      subgraphToken `json:"token1"`
	FeeTier     json.Number   `json:"feeTier"`
	TickSpacing json.Number   `json:"tickSpacing"`
	Hooks       string        `json:"hooks"`
}

type subgraphToken struct {
	ID string `json:"id"`
}

func poolEntryFromSubgraph(pool v4SubgraphPool, index int) (v4PoolEntry, error) {
	if pool.ID == "" {
		return v4PoolEntry{}, fmt.Errorf("v4 subgraph pool[%d] missing id", index)
	}
	fee, err := parseSubgraphUint32(pool.FeeTier, "feeTier")
	if err != nil {
		return v4PoolEntry{}, fmt.Errorf("v4 subgraph pool[%d]: %w", index, err)
	}
	tickSpacing, err := parseSubgraphInt32(pool.TickSpacing, "tickSpacing")
	if err != nil {
		return v4PoolEntry{}, fmt.Errorf("v4 subgraph pool[%d]: %w", index, err)
	}

	key := marketv4.PoolKey{
		Currency0:   common.HexToAddress(pool.Token0.ID),
		Currency1:   common.HexToAddress(pool.Token1.ID),
		Fee:         fee,
		TickSpacing: tickSpacing,
		Hooks:       common.HexToAddress(pool.Hooks),
	}
	id := marketv4.PoolID(common.HexToHash(pool.ID))
	return v4PoolEntry{id: id, key: key}, nil
}

func parseSubgraphUint32(value json.Number, field string) (uint32, error) {
	parsed, err := parseSubgraphBigInt(value, field)
	if err != nil {
		return 0, err
	}
	if !parsed.IsUint64() || parsed.Uint64() > uint64(^uint32(0)) {
		return 0, fmt.Errorf("%s out of range", field)
	}
	return uint32(parsed.Uint64()), nil
}

func parseSubgraphInt32(value json.Number, field string) (int32, error) {
	parsed, err := parseSubgraphBigInt(value, field)
	if err != nil {
		return 0, err
	}
	if !parsed.IsInt64() {
		return 0, fmt.Errorf("%s out of range", field)
	}
	return int32(parsed.Int64()), nil
}

func parseSubgraphBigInt(value json.Number, field string) (*big.Int, error) {
	if value == "" {
		return nil, fmt.Errorf("%s is required", field)
	}
	parsed, ok := new(big.Int).SetString(value.String(), 10)
	if !ok {
		return nil, fmt.Errorf("invalid %s %q", field, value)
	}
	return parsed, nil
}
