package router

import (
	"math/big"

	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/ethereum/go-ethereum/common"
)

// QuoteFunc 对单个池子执行 exact input 报价。
type QuoteFunc func(poolAddr common.Address, amountIn *big.Int, tokenIn common.Address) (*big.Int, error)

// PoolEdge 表示一个池子的两条边。
type PoolEdge struct {
	PoolAddr common.Address
	Token0   common.Address
	Token1   common.Address
}

// SwapHop 路径中的一跳。
type SwapHop struct {
	PoolAddr common.Address
	TokenIn  common.Address
	TokenOut common.Address
}

// SwapPath 完整跨池路径。
type SwapPath struct {
	Hops []SwapHop
}

// PathFinder 跨池路径 BFS 搜索。
type PathFinder struct {
	cache        *pool.Cache
	edges        []PoolEdge
	tokenToPools map[common.Address][]int
	maxHops      int
	bridgeTokens map[common.Address]bool
}

// NewPathFinder 从 pool.Cache 构建路径搜索器。
func NewPathFinder(cache *pool.Cache, maxHops int, bridgeTokens []common.Address) *PathFinder {
	pf := &PathFinder{
		cache:        cache,
		tokenToPools: make(map[common.Address][]int),
		maxHops:      maxHops,
	}
	if len(bridgeTokens) > 0 {
		pf.bridgeTokens = make(map[common.Address]bool, len(bridgeTokens))
		for _, t := range bridgeTokens {
			pf.bridgeTokens[t] = true
		}
	}
	if cache == nil {
		return pf
	}
	cache.Range(func(addr common.Address, state *pool.State) bool {
		if state.Token0 == (common.Address{}) || state.Token1 == (common.Address{}) {
			return true
		}
		edge := PoolEdge{
			PoolAddr: addr,
			Token0:   state.Token0,
			Token1:   state.Token1,
		}
		idx := len(pf.edges)
		pf.edges = append(pf.edges, edge)
		pf.tokenToPools[state.Token0] = append(pf.tokenToPools[state.Token0], idx)
		pf.tokenToPools[state.Token1] = append(pf.tokenToPools[state.Token1], idx)
		return true
	})
	return pf
}

// FindPaths 查找 tokenIn → tokenOut 的所有有效路径。
func (pf *PathFinder) FindPaths(tokenIn, tokenOut common.Address) []SwapPath {
	if pf == nil || tokenIn == tokenOut {
		return nil
	}

	var results []SwapPath

	type bfsState struct {
		currentToken common.Address
		visitedPools map[common.Address]bool
		hops         []SwapHop
	}

	for _, idx := range pf.tokenToPools[tokenIn] {
		edge := pf.edges[idx]
		var outToken common.Address
		if edge.Token0 == tokenIn {
			outToken = edge.Token1
		} else {
			outToken = edge.Token0
		}
		if outToken != tokenOut {
			if pf.bridgeTokens != nil && !pf.bridgeTokens[outToken] {
				continue
			}
		}

		queue := []bfsState{{
			currentToken: outToken,
			visitedPools: map[common.Address]bool{edge.PoolAddr: true},
			hops: []SwapHop{{
				PoolAddr: edge.PoolAddr,
				TokenIn:  tokenIn,
				TokenOut: outToken,
			}},
		}}

		for len(queue) > 0 {
			state := queue[0]
			queue = queue[1:]

			if state.currentToken == tokenOut {
				results = append(results, SwapPath{Hops: copyHops(state.hops)})
				continue
			}
			if len(state.hops) >= pf.maxHops {
				continue
			}

			for _, nextIdx := range pf.tokenToPools[state.currentToken] {
				nextEdge := pf.edges[nextIdx]
				if state.visitedPools[nextEdge.PoolAddr] {
					continue
				}
				var nextToken common.Address
				if nextEdge.Token0 == state.currentToken {
					nextToken = nextEdge.Token1
				} else {
					nextToken = nextEdge.Token0
				}
				if nextToken != tokenOut {
					if pf.bridgeTokens != nil && !pf.bridgeTokens[nextToken] {
						continue
					}
				}

				newVisited := make(map[common.Address]bool, len(state.visitedPools))
				for k, v := range state.visitedPools {
					newVisited[k] = v
				}
				newVisited[nextEdge.PoolAddr] = true

				queue = append(queue, bfsState{
					currentToken: nextToken,
					visitedPools: newVisited,
					hops: append(copyHops(state.hops), SwapHop{
						PoolAddr: nextEdge.PoolAddr,
						TokenIn:  state.currentToken,
						TokenOut: nextToken,
					}),
				})
			}
		}
	}
	return results
}

func copyHops(hops []SwapHop) []SwapHop {
	cp := make([]SwapHop, len(hops))
	copy(cp, hops)
	return cp
}
