package service

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// PoolEdge 表示一个池子的两条边：从 token0→token1 和 token1→token0。
type PoolEdge struct {
	Pool    *PoolQuoteService
	Token0  common.Address
	Token1  common.Address
	Address common.Address
}

// SwapHop 表示路径中的一跳（经过一个池子的一次 swap）。
type SwapHop struct {
	Pool    *PoolQuoteService
	TokenIn  common.Address
	TokenOut common.Address
}

// SwapPath 一条完整的跨池 swap 路径。
type SwapPath struct {
	Hops []SwapHop
}

// PathFinder 跨池路径搜索器。
type PathFinder struct {
	edges        []PoolEdge            // 所有池子边
	tokenToPools map[common.Address][]int // token → 包含该 token 的 PoolEdge 索引列表
	maxHops      int
	bridgeTokens map[common.Address]bool // 允许的中间代币（nil 表示允许所有）
}

// NewPathFinder 创建路径搜索器。
//
// poolServices 为所有可用的池子报价服务。
// maxHops 为最大跳数（经过的池子数量），2 表示最多经过 2 个池子（即最多 2 跳）。
// bridgeTokens 为允许的中间代币白名单。如果为空，则允许所有池子中的代币。
func NewPathFinder(poolServices map[common.Address]*PoolQuoteService, maxHops int, bridgeTokens []common.Address) *PathFinder {
	pf := &PathFinder{
		tokenToPools: make(map[common.Address][]int),
		maxHops:      maxHops,
	}

	// 构建 bridgeTokens 查找表
	if len(bridgeTokens) > 0 {
		pf.bridgeTokens = make(map[common.Address]bool)
		for _, t := range bridgeTokens {
			pf.bridgeTokens[t] = true
		}
	}

	// 构建池子边列表和 token→pool 索引
	for _, svc := range poolServices {
		state := svc.pool.GetStateCopy()
		edge := PoolEdge{
			Pool:    svc,
			Token0:  state.Token0,
			Token1:  state.Token1,
			Address: state.Address,
		}
		idx := len(pf.edges)
		pf.edges = append(pf.edges, edge)
		pf.tokenToPools[state.Token0] = append(pf.tokenToPools[state.Token0], idx)
		pf.tokenToPools[state.Token1] = append(pf.tokenToPools[state.Token1], idx)
	}

	return pf
}

// FindPaths 查找从 tokenIn 到 tokenOut 的所有有效路径。
//
// 返回按跳数升序排列的路径列表。如果无有效路径，返回空切片。
func (pf *PathFinder) FindPaths(tokenIn, tokenOut common.Address) []SwapPath {
	if tokenIn == tokenOut {
		return nil
	}

	var results []SwapPath

	// BFS：每个状态是 (当前token, 已访问池子集合, 路径)
	type bfsState struct {
		currentToken common.Address
		visitedPools map[common.Address]bool
		hops         []SwapHop
	}

	// 初始：从所有包含 tokenIn 的池子出发
	startIndices := pf.tokenToPools[tokenIn]
	for _, idx := range startIndices {
		edge := pf.edges[idx]

		// 确定该池子的输出 token
		var outToken common.Address
		if edge.Token0 == tokenIn {
			outToken = edge.Token1
		} else {
			outToken = edge.Token0
		}

		// 检查第一个中间 token 是否在 bridgeTokens 中（或就是 target）
		if outToken != tokenOut {
			if pf.bridgeTokens != nil && !pf.bridgeTokens[outToken] {
				continue
			}
		}

		queue := []bfsState{{
			currentToken: outToken,
			visitedPools: map[common.Address]bool{edge.Address: true},
			hops: []SwapHop{{
				Pool:     edge.Pool,
				TokenIn:  tokenIn,
				TokenOut: outToken,
			}},
		}}

		for len(queue) > 0 {
			state := queue[0]
			queue = queue[1:]

			// 到达目标？
			if state.currentToken == tokenOut {
				results = append(results, SwapPath{Hops: copyHops(state.hops)})
				continue
			}

			// 已达到最大跳数，不再扩展
			if len(state.hops) >= pf.maxHops {
				continue
			}

			// 扩展下一跳
			for _, nextIdx := range pf.tokenToPools[state.currentToken] {
				nextEdge := pf.edges[nextIdx]

				// 不重复经过同一个池子
				if state.visitedPools[nextEdge.Address] {
					continue
				}

				var nextToken common.Address
				if nextEdge.Token0 == state.currentToken {
					nextToken = nextEdge.Token1
				} else {
					nextToken = nextEdge.Token0
				}

				// 中间 token 必须是 bridge token，或者是目标 token
				if nextToken != tokenOut {
					if pf.bridgeTokens != nil && !pf.bridgeTokens[nextToken] {
						continue
					}
				}

				newVisited := make(map[common.Address]bool)
				for k, v := range state.visitedPools {
					newVisited[k] = v
				}
				newVisited[nextEdge.Address] = true

				queue = append(queue, bfsState{
					currentToken: nextToken,
					visitedPools: newVisited,
					hops: append(copyHops(state.hops), SwapHop{
						Pool:     nextEdge.Pool,
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

// QuoteResult 跨池报价结果。
type QuoteResult struct {
	Hops      []QuoteHop // 路径中的每一跳
	AmountIn  *big.Int   // 输入数量
	AmountOut *big.Int   // 输出数量
	TokenIn   common.Address
	TokenOut  common.Address
}

// QuoteHop 报价路径中的一跳（可序列化）。
type QuoteHop struct {
	Pool     common.Address // 池子地址
	TokenIn  common.Address // 输入代币
	TokenOut common.Address // 输出代币
}
