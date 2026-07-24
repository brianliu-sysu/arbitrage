package poolsapp

import (
	"context"
	"sort"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	"github.com/ethereum/go-ethereum/common"
)

const (
	PoolTypeUniv3     = "univ3"
	PoolTypeUniv4     = "univ4"
	PoolTypePancakeV3 = "pancakev3"
	PoolTypeBalancer  = "balancer"
)

// TokenInfo is token metadata exposed by the pools API.
type TokenInfo struct {
	Address string `json:"address"`
	Symbol  string `json:"symbol"`
	Decimal uint8  `json:"decimal"`
}

// PoolInfo is pool metadata exposed by the pools API.
type PoolInfo struct {
	PoolAddress  string      `json:"poolAddress,omitempty"`
	PoolID       string      `json:"poolId,omitempty"`
	PoolType     string      `json:"poolType"`
	BalancerType string      `json:"balancerType,omitempty"`
	Token0       TokenInfo   `json:"token0,omitempty"`
	Token1       TokenInfo   `json:"token1,omitempty"`
	Tokens       []TokenInfo `json:"tokens,omitempty"`
	Fee          uint32      `json:"fee,omitempty"`
	Hooks        string      `json:"hooks,omitempty"`
}

// ListResponse is the pools API response payload.
type ListResponse struct {
	Items []PoolInfo `json:"items"`
	Count int        `json:"count"`
}

// AppService lists tracked pool metadata across protocols.
type AppService struct {
	protocols []ProtocolAdapter
	tokens    TokenService
	head      HeadBlockReader
}

func NewAppService(deps ServiceDeps) *AppService {
	seen := make(map[string]struct{}, len(deps.Protocols))
	protocols := make([]ProtocolAdapter, 0, len(deps.Protocols))
	for _, protocol := range deps.Protocols {
		if protocol == nil {
			continue
		}
		poolType := protocol.Type()
		if _, exists := seen[poolType]; exists {
			continue
		}
		seen[poolType] = struct{}{}
		protocols = append(protocols, protocol)
	}
	return &AppService{
		protocols: protocols,
		tokens:    deps.Tokens,
		head:      deps.Head,
	}
}

// List returns metadata for all tracked pools in the system.
func (s *AppService) List(ctx context.Context) (*ListResponse, error) {
	if s == nil {
		return &ListResponse{}, nil
	}

	pools := make([]PoolInfo, 0)
	for _, protocol := range s.protocols {
		items, err := protocol.List(ctx)
		if err != nil {
			return nil, err
		}
		pools = append(pools, items...)
	}

	tokenAddresses := make([]common.Address, 0, len(pools)*2)
	for _, pool := range pools {
		if pool.PoolType == PoolTypeBalancer {
			tokenAddresses = append(tokenAddresses, balancerTokenAddresses([]PoolInfo{pool})...)
			continue
		}
		if pool.Token0.Address != "" {
			tokenAddresses = append(tokenAddresses, common.HexToAddress(pool.Token0.Address))
		}
		if pool.Token1.Address != "" {
			tokenAddresses = append(tokenAddresses, common.HexToAddress(pool.Token1.Address))
		}
	}

	var tokenMeta map[common.Address]*asset.Token
	if s.tokens != nil {
		var err error
		tokenMeta, err = s.tokens.Resolve(ctx, tokenAddresses)
		if err != nil {
			return nil, err
		}
	} else {
		tokenMeta = map[common.Address]*asset.Token{}
	}

	for i := range pools {
		if pools[i].PoolType == PoolTypeBalancer {
			for j := range pools[i].Tokens {
				pools[i].Tokens[j] = enrichToken(pools[i].Tokens[j], tokenMeta)
			}
			if len(pools[i].Tokens) > 0 {
				pools[i].Token0 = enrichToken(pools[i].Token0, tokenMeta)
			}
			if len(pools[i].Tokens) > 1 {
				pools[i].Token1 = enrichToken(pools[i].Token1, tokenMeta)
			}
			continue
		}
		pools[i].Token0 = enrichToken(pools[i].Token0, tokenMeta)
		pools[i].Token1 = enrichToken(pools[i].Token1, tokenMeta)
	}

	sort.Slice(pools, func(i, j int) bool {
		left := pools[i].PoolAddress
		if left == "" {
			left = pools[i].PoolID
		}
		right := pools[j].PoolAddress
		if right == "" {
			right = pools[j].PoolID
		}
		if pools[i].PoolType != pools[j].PoolType {
			return pools[i].PoolType < pools[j].PoolType
		}
		return left < right
	})

	return &ListResponse{Items: pools, Count: len(pools)}, nil
}

func tokenInfoFromAddress(address common.Address) TokenInfo {
	return TokenInfo{Address: address.Hex()}
}

func enrichToken(info TokenInfo, tokens map[common.Address]*asset.Token) TokenInfo {
	if info.Address == "" {
		return info
	}
	address := common.HexToAddress(info.Address)
	if asset.IsNativeETH(address) {
		native := asset.NativeETHToken()
		info.Symbol = native.Symbol
		info.Decimal = native.Decimal
		return info
	}
	token, ok := tokens[address]
	if !ok || token == nil {
		return info
	}
	info.Symbol = token.Symbol
	info.Decimal = token.Decimal
	return info
}
