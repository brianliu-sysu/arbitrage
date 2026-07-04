package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v3"
	"github.com/ethereum/go-ethereum/common"
)

const defaultGraphQLTimeout = 30 * time.Second

var allowedOrderBy = map[string]struct{}{
	"totalValueLockedUSD": {},
	"volumeUSD":           {},
	"volume24h":           {},
	"txCount":             {},
	"feesUSD":             {},
	"liquidity":           {},
}

var allowedOrderDirection = map[string]struct{}{
	"asc":  {},
	"desc": {},
}

// SubgraphRegistry loads pool addresses from a Uniswap V3 subgraph.
type SubgraphRegistry struct {
	cfg    config.SubgraphPoolConfig
	client *http.Client
	clock  func() time.Time

	mu        sync.RWMutex
	cached    []common.Address
	lastFetch time.Time
	added     map[common.Address]struct{}
	removed   map[common.Address]struct{}
}

func NewSubgraphRegistry(cfg config.SubgraphPoolConfig) *SubgraphRegistry {
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
	return &SubgraphRegistry{
		cfg:     cfg,
		client:  &http.Client{Timeout: defaultGraphQLTimeout},
		clock:   time.Now,
		added:   make(map[common.Address]struct{}),
		removed: make(map[common.Address]struct{}),
	}
}

func (r *SubgraphRegistry) List(ctx context.Context) ([]common.Address, error) {
	if r.cfg.IsEnabled() {
		if err := r.refreshIfNeeded(ctx); err != nil {
			return nil, err
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentAddressesLocked(), nil
}

func (r *SubgraphRegistry) Add(_ context.Context, address common.Address) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.removed, address)
	r.added[address] = struct{}{}
	return nil
}

func (r *SubgraphRegistry) Remove(_ context.Context, address common.Address) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.added, address)
	r.removed[address] = struct{}{}
	return nil
}

func (r *SubgraphRegistry) Refresh(ctx context.Context) error {
	if !r.cfg.IsEnabled() {
		return fmt.Errorf("subgraph registry is disabled")
	}
	return r.fetch(ctx)
}

func (r *SubgraphRegistry) refreshIfNeeded(ctx context.Context) error {
	r.mu.RLock()
	stale := r.lastFetch.IsZero() || r.clock().Sub(r.lastFetch) >= r.cfg.RefreshInterval
	r.mu.RUnlock()
	if !stale {
		return nil
	}
	return r.fetch(ctx)
}

func (r *SubgraphRegistry) fetch(ctx context.Context) error {
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

func (r *SubgraphRegistry) currentAddressesLocked() []common.Address {
	seen := make(map[common.Address]struct{}, len(r.cached)+len(r.added))
	addresses := make([]common.Address, 0, len(r.cached)+len(r.added))

	for _, address := range r.cached {
		if _, removed := r.removed[address]; removed {
			continue
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address)
	}
	for address := range r.added {
		if _, removed := r.removed[address]; removed {
			continue
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address)
	}

	sort.Slice(addresses, func(i, j int) bool {
		return addresses[i].Hex() < addresses[j].Hex()
	})
	return addresses
}

func (r *SubgraphRegistry) queryPools(ctx context.Context) ([]common.Address, error) {
	query, variables, mode, err := r.buildQuery()
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
		return nil, fmt.Errorf("query subgraph: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read subgraph response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("subgraph status %d: %s", resp.StatusCode, string(raw))
	}

	var response graphQLResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("decode subgraph response: %w", err)
	}
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("subgraph graphql error: %s", response.Errors[0].Message)
	}

	switch mode {
	case queryModePoolDayData:
		if response.Data.PoolDayDatas == nil {
			return nil, fmt.Errorf("subgraph response missing poolDayDatas")
		}
		return poolAddressesFromDayData(response.Data.PoolDayDatas), nil
	default:
		if response.Data.Pools == nil {
			return nil, fmt.Errorf("subgraph response missing pools")
		}
		return poolAddressesFromPools(response.Data.Pools), nil
	}
}

func poolAddressesFromPools(pools []subgraphPool) []common.Address {
	addresses := make([]common.Address, 0, len(pools))
	for _, pool := range pools {
		if pool.ID == "" {
			continue
		}
		addresses = append(addresses, common.HexToAddress(pool.ID))
	}
	return addresses
}

func poolAddressesFromDayData(rows []subgraphPoolDayData) []common.Address {
	addresses := make([]common.Address, 0, len(rows))
	for _, row := range rows {
		if row.Pool.ID == "" {
			continue
		}
		addresses = append(addresses, common.HexToAddress(row.Pool.ID))
	}
	return addresses
}

type queryMode int

const (
	queryModePools queryMode = iota
	queryModePoolDayData
)

func (r *SubgraphRegistry) buildQuery() (string, map[string]any, queryMode, error) {
	orderBy := strings.TrimSpace(r.cfg.OrderBy)
	if _, ok := allowedOrderBy[orderBy]; !ok {
		return "", nil, queryModePools, fmt.Errorf("unsupported subgraph order_by %q", orderBy)
	}
	orderDirection := strings.ToLower(strings.TrimSpace(r.cfg.OrderDirection))
	if _, ok := allowedOrderDirection[orderDirection]; !ok {
		return "", nil, queryModePools, fmt.Errorf("unsupported subgraph order_direction %q", orderDirection)
	}

	if orderBy == "volume24h" {
		return r.buildPoolDayDataQuery(orderDirection)
	}
	return r.buildPoolsQuery(orderBy, orderDirection)
}

func (r *SubgraphRegistry) buildPoolsQuery(orderBy, orderDirection string) (string, map[string]any, queryMode, error) {
	where := r.buildPoolWhereFilter()
	if r.cfg.MinVolume24hUSD != "" {
		dayWhere := map[string]any{
			"date": poolDayDate(r.clock()),
		}
		if volumeFilter := strings.TrimSpace(r.cfg.MinVolume24hUSD); volumeFilter != "" {
			dayWhere["volumeUSD_gt"] = volumeFilter
		}
		where["poolDayData_"] = dayWhere
	}

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
  }
}`
	return query, variables, queryModePools, nil
}

func (r *SubgraphRegistry) buildPoolDayDataQuery(orderDirection string) (string, map[string]any, queryMode, error) {
	where := map[string]any{
		"date": poolDayDate(r.clock()),
	}
	if volumeFilter := strings.TrimSpace(r.cfg.MinVolume24hUSD); volumeFilter != "" {
		where["volumeUSD_gt"] = volumeFilter
	}
	if poolWhere := r.buildPoolWhereFilter(); len(poolWhere) > 0 {
		where["pool_"] = poolWhere
	}

	variables := map[string]any{
		"first":          r.cfg.First,
		"skip":           r.cfg.Skip,
		"orderBy":        "volumeUSD",
		"orderDirection": orderDirection,
		"where":          where,
	}

	query := `
query PoolDayDatas($first: Int!, $skip: Int!, $orderBy: PoolDayData_orderBy!, $orderDirection: OrderDirection!, $where: PoolDayData_filter) {
  poolDayDatas(
    first: $first
    skip: $skip
    orderBy: $orderBy
    orderDirection: $orderDirection
    where: $where
  ) {
    pool {
      id
    }
  }
}`
	return query, variables, queryModePoolDayData, nil
}

func (r *SubgraphRegistry) buildPoolWhereFilter() map[string]any {
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
	return where
}

func poolDayDate(now time.Time) int {
	return int(now.Unix()/86400) * 86400
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data struct {
		Pools        []subgraphPool         `json:"pools"`
		PoolDayDatas []subgraphPoolDayData  `json:"poolDayDatas"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type subgraphPool struct {
	ID string `json:"id"`
}

type subgraphPoolDayData struct {
	Pool subgraphPool `json:"pool"`
}

var _ marketv3.PoolRegistry = (*SubgraphRegistry)(nil)
