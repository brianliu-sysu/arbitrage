// Package subgraph 提供 Uniswap V3 Subgraph 查询客户端，用于自动发现热门池子。
package subgraph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PoolInfo 从 Subgraph 返回的池子信息。
type PoolInfo struct {
	Address  string `json:"id"`
	Token0   TokenInfo `json:"token0"`
	Token1   TokenInfo `json:"token1"`
	FeeTier  string `json:"feeTier"`
	VolumeUSD string `json:"volumeUSD"`
	TVLUSD   string `json:"totalValueLockedUSD"`
}

// TokenInfo 代币信息。
type TokenInfo struct {
	ID       string `json:"id"`
	Symbol   string `json:"symbol"`
	Decimals string `json:"decimals"`
}

// Client Uniswap V3 Subgraph 客户端。
type Client struct {
	url    string
	http   *http.Client
}

// NewClient 创建 Subgraph 客户端。
func NewClient(subgraphURL string) *Client {
	return &Client{
		url: subgraphURL,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// FetchTopPools 查询 Top N 池子。
//
// orderBy: "volumeUSD" / "totalValueLockedUSD" / "txCount"
// minTVLUSD: 最低 TVL（美元），过滤低流动性池子
// minVolumeUSD: 最低 24h 交易量（美元）
// limit: 最多返回数量
//
// 额外过滤条件: liquidity_gt "0"（排除零流动性池子）
func (c *Client) FetchTopPools(orderBy string, minTVLUSD, minVolumeUSD, limit int) ([]PoolInfo, error) {
	orderField := "volumeUSD"
	switch orderBy {
	case "totalValueLockedUSD":
		orderField = "totalValueLockedUSD"
	case "txCount":
		orderField = "txCount"
	}

	query := fmt.Sprintf(`{
		pools(
			first: %d,
			orderBy: %s,
			orderDirection: desc,
			where: {
				totalValueLockedUSD_gt: %d,
				volumeUSD_gt: %d,
				liquidity_gt: "0"
			}
		) {
			id
			feeTier
			volumeUSD
			totalValueLockedUSD
			token0 { id symbol decimals }
			token1 { id symbol decimals }
		}
	}`, limit, orderField, minTVLUSD, minVolumeUSD)

	body := map[string]string{"query": query}
	jsonBody, _ := json.Marshal(body)

	resp, err := c.http.Post(c.url, "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("subgraph query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("subgraph returned %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var result struct {
		Data struct {
			Pools []PoolInfo `json:"pools"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode subgraph response: %w", err)
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("subgraph errors: %v", result.Errors)
	}

	return result.Data.Pools, nil
}

// SubgraphURLs 各主流链的 Uniswap V3 子图地址。
var SubgraphURLs = map[string]string{
	"ethereum":  "https://api.thegraph.com/subgraphs/name/uniswap/uniswap-v3",
	"arbitrum":  "https://api.thegraph.com/subgraphs/name/ianlapham/arbitrum-minimal",
	"optimism":  "https://api.thegraph.com/subgraphs/name/ianlapham/optimism-post-regenesis",
	"polygon":   "https://api.thegraph.com/subgraphs/name/ianlapham/uniswap-v3-polygon",
	"base":      "https://api.studio.thegraph.com/query/48211/uniswap-v3-base/version/latest",
	"celo":      "https://api.thegraph.com/subgraphs/name/jesse-sawa/uniswap-celo",
	"bsc":       "https://api.thegraph.com/subgraphs/name/ianlapham/uniswap-v3-bsc",
	"avalanche": "https://api.thegraph.com/subgraphs/name/lynnshaoyu/uniswap-v3-avax",
}
