package contract

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// RequiredTokenApprovals derives ERC20 allowances the executor must grant before
// running plan routes. It unions fill-token hints with tokenIn decoded from known
// swap calldata (V3 / Pancake / Balancer), so incomplete embedded approval lists
// still cover later hops such as USDC -> SwapRouter02.
func RequiredTokenApprovals(plan ExecutionPlan) []TokenApproval {
	seen := make(map[string]struct{})
	out := make([]TokenApproval, 0, len(plan.Routes)+len(plan.SettlementRoutes))
	add := func(token, spender common.Address) {
		if token == (common.Address{}) || spender == (common.Address{}) {
			return
		}
		if token == NativeETHSentinel || token == spender {
			return
		}
		key := token.Hex() + ":" + spender.Hex()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, TokenApproval{
			Token:   token,
			Spender: spender,
			Amount:  maxUint256(),
		})
	}

	routes := make([]SwapRoute, 0, len(plan.Routes)+len(plan.SettlementRoutes))
	routes = append(routes, plan.Routes...)
	routes = append(routes, plan.SettlementRoutes...)
	for _, route := range routes {
		spender := route.RouterAddress
		if spender == (common.Address{}) {
			continue
		}
		if route.FillSource == FillSourceERC20Balance {
			add(route.FillToken, spender)
		}
		if token, ok := decodeRouteInputToken(route.Data); ok {
			add(token, spender)
		}
	}
	return out
}

// MergeTokenApprovals returns the union of base and extra approvals keyed by token+spender.
// When both list the same pair, the larger amount wins.
func MergeTokenApprovals(base, extra []TokenApproval) []TokenApproval {
	if len(extra) == 0 {
		return base
	}
	if len(base) == 0 {
		return append([]TokenApproval(nil), extra...)
	}
	seen := make(map[string]int, len(base)+len(extra))
	out := make([]TokenApproval, 0, len(base)+len(extra))
	add := func(item TokenApproval) {
		if item.Token == (common.Address{}) || item.Spender == (common.Address{}) {
			return
		}
		key := item.Token.Hex() + ":" + item.Spender.Hex()
		if idx, ok := seen[key]; ok {
			if item.Amount != nil && (out[idx].Amount == nil || item.Amount.Cmp(out[idx].Amount) > 0) {
				out[idx].Amount = new(big.Int).Set(item.Amount)
			}
			return
		}
		seen[key] = len(out)
		cloned := TokenApproval{
			Token:   item.Token,
			Spender: item.Spender,
			Amount:  maxUint256(),
		}
		if item.Amount != nil {
			cloned.Amount = new(big.Int).Set(item.Amount)
		}
		out = append(out, cloned)
	}
	for _, item := range base {
		add(item)
	}
	for _, item := range extra {
		add(item)
	}
	return out
}

func decodeRouteInputToken(data []byte) (common.Address, bool) {
	if len(data) < 4 {
		return common.Address{}, false
	}
	selector := data[:4]
	payload := data[4:]

	if method, err := swapRouter02ABI.MethodById(selector); err == nil && method.Name == "exactInputSingle" {
		// Static ExactInputSingleParams tuple is encoded in-place; tokenIn is word 0.
		return addressWord(payload, 0)
	}
	if method, err := pancakeV3RouterABI.MethodById(selector); err == nil && method.Name == "exactInputSingle" {
		return addressWord(payload, 0)
	}
	if method, err := balancerV3RouterABI.MethodById(selector); err == nil && method.Name == "swapSingleTokenExactIn" {
		// pool, tokenIn, tokenOut, ...
		return addressWord(payload, 1)
	}
	if method, err := balancerVaultABI.MethodById(selector); err == nil && method.Name == "swap" {
		return decodeBalancerV2AssetIn(payload)
	}
	return common.Address{}, false
}

func decodeBalancerV2AssetIn(payload []byte) (common.Address, bool) {
	// swap(SingleSwap, FundManagement, uint256, uint256)
	// First head word is the offset to the SingleSwap tuple.
	if len(payload) < 32 {
		return common.Address{}, false
	}
	offset := new(big.Int).SetBytes(payload[0:32]).Uint64()
	if offset > uint64(len(payload)) || uint64(len(payload))-offset < 5*32 {
		return common.Address{}, false
	}
	// SingleSwap: poolId, kind, assetIn, assetOut, amount, userData...
	return addressWord(payload[offset:], 2)
}

func addressWord(payload []byte, wordIndex int) (common.Address, bool) {
	start := wordIndex * 32
	if start < 0 || len(payload) < start+32 {
		return common.Address{}, false
	}
	addr := common.BytesToAddress(payload[start+12 : start+32])
	return addr, addr != (common.Address{})
}

func maxUint256() *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
}
