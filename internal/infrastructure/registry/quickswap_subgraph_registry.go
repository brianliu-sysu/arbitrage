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
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	"github.com/ethereum/go-ethereum/common"
)

var allowedQuickSwapOrderBy = map[string]string{
	"totalValueLockedUSD": "totalValueLockedUSD",
	"volumeUSD":           "cumulativeVolumeUSD",
	"volume24h":           "",
	"txCount":             "cumulativeSwapCount",
	"feesUSD":             "cumulativeTotalRevenueUSD",
	"liquidity":           "totalLiquidityUSD",
}

// QuickSwapSubgraphRegistry loads QuickSwap V3 pool addresses from a Messari-schema subgraph.
type QuickSwapSubgraphRegistry struct {
	cfg    config.SubgraphPoolConfig
	client *http.Client
	clock  func() time.Time

	mu        sync.RWMutex
	cached    []common.Address
	lastFetch time.Time
	added     map[common.Address]struct{}
	removed   map[common.Address]struct{}
}

func NewQuickSwapSubgraphRegistry(cfg config.SubgraphPoolConfig) *QuickSwapSubgraphRegistry {
	return &QuickSwapSubgraphRegistry{
		cfg:     cfg,
		client:  &http.Client{Timeout: defaultGraphQLTimeout},
		clock:   time.Now,
		added:   make(map[common.Address]struct{}),
		removed: make(map[common.Address]struct{}),
	}
}

func (r *QuickSwapSubgraphRegistry) List(ctx context.Context) ([]common.Address, error) {
	if r.cfg.IsEnabled() {
		if err := r.refreshIfNeeded(ctx); err != nil {
			return nil, err
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentAddressesLocked(), nil
}

func (r *QuickSwapSubgraphRegistry) Add(_ context.Context, address common.Address) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.removed, address)
	r.added[address] = struct{}{}
	return nil
}

func (r *QuickSwapSubgraphRegistry) Remove(_ context.Context, address common.Address) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.added, address)
	r.removed[address] = struct{}{}
	return nil
}

func (r *QuickSwapSubgraphRegistry) refreshIfNeeded(ctx context.Context) error {
	r.mu.RLock()
	stale := r.lastFetch.IsZero() || r.clock().Sub(r.lastFetch) >= r.cfg.RefreshInterval
	r.mu.RUnlock()
	if !stale {
		return nil
	}
	return r.fetch(ctx)
}

func (r *QuickSwapSubgraphRegistry) fetch(ctx context.Context) error {
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

func (r *QuickSwapSubgraphRegistry) currentAddressesLocked() []common.Address {
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

func (r *QuickSwapSubgraphRegistry) queryPools(ctx context.Context) ([]common.Address, error) {
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

	var response quickGraphQLResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("decode subgraph response: %w", err)
	}
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("subgraph graphql error: %s", response.Errors[0].Message)
	}

	switch mode {
	case quickQueryModeDailySnapshot:
		if response.Data.LiquidityPoolDailySnapshots == nil {
			return nil, fmt.Errorf("subgraph response missing liquidityPoolDailySnapshots")
		}
		return poolAddressesFromQuickSwapDailySnapshots(response.Data.LiquidityPoolDailySnapshots, r.cfg.FeeTiers), nil
	default:
		if response.Data.LiquidityPools == nil {
			return nil, fmt.Errorf("subgraph response missing liquidityPools")
		}
		return poolAddressesFromQuickSwapPools(response.Data.LiquidityPools, r.cfg.FeeTiers), nil
	}
}

func poolAddressesFromQuickSwapPools(pools []quickSubgraphPool, feeTiers []uint32) []common.Address {
	addresses := make([]common.Address, 0, len(pools))
	for _, pool := range pools {
		if pool.ID == "" || !quickPoolMatchesFeeTiers(pool, feeTiers) {
			continue
		}
		addresses = append(addresses, common.HexToAddress(pool.ID))
	}
	return addresses
}

func poolAddressesFromQuickSwapDailySnapshots(rows []quickSubgraphDailySnapshot, feeTiers []uint32) []common.Address {
	addresses := make([]common.Address, 0, len(rows))
	for _, row := range rows {
		if row.Pool.ID == "" || !quickPoolMatchesFeeTiers(row.Pool, feeTiers) {
			continue
		}
		addresses = append(addresses, common.HexToAddress(row.Pool.ID))
	}
	return addresses
}

type quickQueryMode int

const (
	quickQueryModeLiquidityPools quickQueryMode = iota
	quickQueryModeDailySnapshot
)

func (r *QuickSwapSubgraphRegistry) buildQuery() (string, map[string]any, quickQueryMode, error) {
	orderBy := strings.TrimSpace(r.cfg.OrderBy)
	subgraphOrderBy, ok := allowedQuickSwapOrderBy[orderBy]
	if !ok {
		return "", nil, quickQueryModeLiquidityPools, fmt.Errorf("unsupported quick subgraph order_by %q", orderBy)
	}
	orderDirection := strings.ToLower(strings.TrimSpace(r.cfg.OrderDirection))
	if _, ok := allowedOrderDirection[orderDirection]; !ok {
		return "", nil, quickQueryModeLiquidityPools, fmt.Errorf("unsupported quick subgraph order_direction %q", orderDirection)
	}

	if orderBy == "volume24h" {
		return r.buildDailySnapshotQuery(orderDirection)
	}
	return r.buildLiquidityPoolsQuery(subgraphOrderBy, orderDirection)
}

func (r *QuickSwapSubgraphRegistry) buildLiquidityPoolsQuery(orderBy, orderDirection string) (string, map[string]any, quickQueryMode, error) {
	where := r.buildLiquidityPoolWhereFilter()
	if r.cfg.MinVolume24hUSD != "" {
		// Messari schema has no nested poolDayData on LiquidityPool; volume24h must use daily snapshots.
		return "", nil, quickQueryModeLiquidityPools, fmt.Errorf("quick subgraph requires order_by volume24h when min_volume_24h_usd is set")
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
query LiquidityPools($first: Int!, $skip: Int!, $orderBy: LiquidityPool_orderBy!, $orderDirection: OrderDirection!, $where: LiquidityPool_filter) {
  liquidityPools(
    first: $first
    skip: $skip
    orderBy: $orderBy
    orderDirection: $orderDirection
    where: $where
  ) {
    id
    fees {
      feeType
      feePercentage
    }
  }
}`
	return query, variables, quickQueryModeLiquidityPools, nil
}

func (r *QuickSwapSubgraphRegistry) buildDailySnapshotQuery(orderDirection string) (string, map[string]any, quickQueryMode, error) {
	where := map[string]any{}
	if volumeFilter := strings.TrimSpace(r.cfg.MinVolume24hUSD); volumeFilter != "" {
		where["dailyVolumeUSD_gt"] = volumeFilter
	}
	if poolWhere := r.buildLiquidityPoolWhereFilter(); len(poolWhere) > 0 {
		where["pool_"] = poolWhere
	}

	variables := map[string]any{
		"first":          r.cfg.First,
		"skip":           r.cfg.Skip,
		"orderBy":        "dailyVolumeUSD",
		"orderDirection": orderDirection,
	}
	if len(where) > 0 {
		variables["where"] = where
	}

	query := `
query LiquidityPoolDailySnapshots($first: Int!, $skip: Int!, $orderBy: LiquidityPoolDailySnapshot_orderBy!, $orderDirection: OrderDirection!, $where: LiquidityPoolDailySnapshot_filter) {
  liquidityPoolDailySnapshots(
    first: $first
    skip: $skip
    orderBy: $orderBy
    orderDirection: $orderDirection
    where: $where
  ) {
    pool {
      id
      fees {
        feeType
        feePercentage
      }
    }
  }
}`
	return query, variables, quickQueryModeDailySnapshot, nil
}

func (r *QuickSwapSubgraphRegistry) buildLiquidityPoolWhereFilter() map[string]any {
	where := make(map[string]any)
	if r.cfg.MinTotalValueLockedUSD != "" {
		where["totalValueLockedUSD_gt"] = r.cfg.MinTotalValueLockedUSD
	}
	if token0 := strings.TrimSpace(r.cfg.Token0); token0 != "" {
		where["inputTokens_"] = map[string]any{
			"id": strings.ToLower(token0),
		}
	}
	if token1 := strings.TrimSpace(r.cfg.Token1); token1 != "" {
		where["inputTokens_"] = map[string]any{
			"id": strings.ToLower(token1),
		}
	}
	return where
}

func quickPoolMatchesFeeTiers(pool quickSubgraphPool, feeTiers []uint32) bool {
	if len(feeTiers) == 0 {
		return true
	}
	for _, fee := range pool.Fees {
		if fee.FeeType != "FIXED_TRADING_FEE" {
			continue
		}
		if quickTradingFeeMatchesTier(fee.FeePercentage, feeTiers) {
			return true
		}
	}
	return false
}

func quickTradingFeeMatchesTier(feePercentage string, feeTiers []uint32) bool {
	fee, err := parseQuickSwapFeePercentage(feePercentage)
	if err != nil {
		return false
	}
	for _, tier := range feeTiers {
		if fee == float64(tier)/10000 {
			return true
		}
	}
	return false
}

func parseQuickSwapFeePercentage(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("empty fee percentage")
	}
	var fee float64
	if _, err := fmt.Sscanf(value, "%f", &fee); err != nil {
		return 0, err
	}
	return fee, nil
}

type quickGraphQLResponse struct {
	Data struct {
		LiquidityPools              []quickSubgraphPool          `json:"liquidityPools"`
		LiquidityPoolDailySnapshots []quickSubgraphDailySnapshot `json:"liquidityPoolDailySnapshots"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type quickSubgraphPool struct {
	ID   string             `json:"id"`
	Fees []quickSubgraphFee `json:"fees"`
}

type quickSubgraphFee struct {
	FeeType       string `json:"feeType"`
	FeePercentage string `json:"feePercentage"`
}

type quickSubgraphDailySnapshot struct {
	Pool quickSubgraphPool `json:"pool"`
}

var _ marketquick.PoolRegistry = (*QuickSwapSubgraphRegistry)(nil)
