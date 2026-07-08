package arbitrageapp

import (
	"sort"

	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

const autoStartTokenCount = 3

type tokenOverlap struct {
	address common.Address
	count   int
}

// ResolveTriangleStartTokens merges configured start tokens with the top pool-overlap tokens.
func ResolveTriangleStartTokens(configured []common.Address, edges []quoteunified.PoolEdge, autoLimit int) []common.Address {
	if autoLimit <= 0 {
		return dedupeStartTokens(configured)
	}
	auto := TopPoolOverlapTokens(edges, autoLimit)
	return mergeStartTokens(configured, auto)
}

// TopPoolOverlapTokens returns tokens that appear in the most pools.
func TopPoolOverlapTokens(edges []quoteunified.PoolEdge, limit int) []common.Address {
	if limit <= 0 || len(edges) == 0 {
		return nil
	}

	counts := make(map[common.Address]int)
	for _, edge := range edges {
		if edge.Token0 != (common.Address{}) {
			counts[edge.Token0]++
		}
		if edge.Token1 != (common.Address{}) {
			counts[edge.Token1]++
		}
	}
	if len(counts) == 0 {
		return nil
	}

	ranked := make([]tokenOverlap, 0, len(counts))
	for address, count := range counts {
		ranked = append(ranked, tokenOverlap{address: address, count: count})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].address.Hex() < ranked[j].address.Hex()
	})

	if limit > len(ranked) {
		limit = len(ranked)
	}
	tokens := make([]common.Address, 0, limit)
	for i := 0; i < limit; i++ {
		tokens = append(tokens, ranked[i].address)
	}
	return tokens
}

func dedupeStartTokens(tokens []common.Address) []common.Address {
	return mergeStartTokens(tokens, nil)
}

func mergeStartTokens(configured, auto []common.Address) []common.Address {
	merged := make([]common.Address, 0, len(configured)+len(auto))
	seen := make(map[common.Address]struct{}, len(configured)+len(auto))

	for _, token := range configured {
		if token == (common.Address{}) {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		merged = append(merged, token)
	}
	for _, token := range auto {
		if token == (common.Address{}) {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		merged = append(merged, token)
	}
	return merged
}

// ResolveSpreadStartTokens merges configured spread tokens with triangle tokens and multi-pool pair tokens.
func ResolveSpreadStartTokens(configured, triangleTokens []common.Address, edges []quoteunified.PoolEdge) []common.Address {
	merged := mergeStartTokens(configured, triangleTokens)
	auto := TokensWithParallelPools(edges)
	return mergeStartTokens(merged, auto)
}

// TokensWithParallelPools returns tokens that appear in token pairs with at least two distinct pools.
func TokensWithParallelPools(edges []quoteunified.PoolEdge) []common.Address {
	if len(edges) == 0 {
		return nil
	}

	type pairKey struct {
		low  common.Address
		high common.Address
	}
	poolsByPair := make(map[pairKey]map[string]struct{})
	for _, edge := range edges {
		if edge.Token0 == (common.Address{}) || edge.Token1 == (common.Address{}) {
			continue
		}
		if quoteunified.IsWETHBridgeVersion(edge.Version) {
			continue
		}
		key := pairKey{low: edge.Token0, high: edge.Token1}
		if edge.Token0.Hex() > edge.Token1.Hex() {
			key = pairKey{low: edge.Token1, high: edge.Token0}
		}
		poolKey := edgeKey(edge)
		if poolKey == "" {
			continue
		}
		if _, ok := poolsByPair[key]; !ok {
			poolsByPair[key] = make(map[string]struct{})
		}
		poolsByPair[key][poolKey] = struct{}{}
	}

	counts := make(map[common.Address]int)
	for key, pools := range poolsByPair {
		if len(pools) < 2 {
			continue
		}
		counts[key.low]++
		counts[key.high]++
	}
	if len(counts) == 0 {
		return nil
	}

	ranked := make([]tokenOverlap, 0, len(counts))
	for address, count := range counts {
		ranked = append(ranked, tokenOverlap{address: address, count: count})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].address.Hex() < ranked[j].address.Hex()
	})

	tokens := make([]common.Address, 0, len(ranked))
	for _, item := range ranked {
		tokens = append(tokens, item.address)
	}
	return tokens
}

func edgeKey(edge quoteunified.PoolEdge) string {
	switch edge.Version {
	case quoteunified.PoolVersionV3:
		return "v3:" + edge.PoolV3.Hex()
	case quoteunified.PoolVersionPancakeV3:
		return "pancakev3:" + edge.PoolPancakeV3.Hex()
	case quoteunified.PoolVersionQuickSwapV3:
		return "quickswapv3:" + edge.PoolQuickSwapV3.Hex()
	case quoteunified.PoolVersionV4:
		return "v4:" + edge.PoolV4.String()
	case quoteunified.PoolVersionBalancer:
		return "balancer:" + edge.PoolBalancer.String()
	default:
		return ""
	}
}
