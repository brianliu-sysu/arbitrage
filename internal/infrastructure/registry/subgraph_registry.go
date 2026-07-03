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
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

const defaultGraphQLTimeout = 30 * time.Second

var allowedOrderBy = map[string]struct{}{
	"totalValueLockedUSD": {},
	"volumeUSD":           {},
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
		cfg.OrderBy = "totalValueLockedUSD"
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
	if response.Data.Pools == nil {
		return nil, fmt.Errorf("subgraph response missing pools")
	}

	addresses := make([]common.Address, 0, len(response.Data.Pools))
	for _, pool := range response.Data.Pools {
		if pool.ID == "" {
			continue
		}
		addresses = append(addresses, common.HexToAddress(pool.ID))
	}
	return addresses, nil
}

func (r *SubgraphRegistry) buildQuery() (string, map[string]any, error) {
	orderBy := strings.TrimSpace(r.cfg.OrderBy)
	if _, ok := allowedOrderBy[orderBy]; !ok {
		return "", nil, fmt.Errorf("unsupported subgraph order_by %q", orderBy)
	}
	orderDirection := strings.ToLower(strings.TrimSpace(r.cfg.OrderDirection))
	if _, ok := allowedOrderDirection[orderDirection]; !ok {
		return "", nil, fmt.Errorf("unsupported subgraph order_direction %q", orderDirection)
	}

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
	return query, variables, nil
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data struct {
		Pools []subgraphPool `json:"pools"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type subgraphPool struct {
	ID string `json:"id"`
}

var _ market.PoolRegistry = (*SubgraphRegistry)(nil)
