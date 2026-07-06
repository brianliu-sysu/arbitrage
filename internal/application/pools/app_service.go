package poolsapp

import (
	"context"
	"fmt"
	"sort"

	assetapp "github.com/brianliu-sysu/uniswapv3/internal/application/asset"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

const (
	PoolTypeUniv3      = "univ3"
	PoolTypeUniv4      = "univ4"
	PoolTypePancakeV3  = "pancakev3"
)

// TokenInfo is token metadata exposed by the pools API.
type TokenInfo struct {
	Address string `json:"address"`
	Symbol  string `json:"symbol"`
	Decimal uint8  `json:"decimal"`
}

// PoolInfo is pool metadata exposed by the pools API.
type PoolInfo struct {
	PoolAddress string    `json:"poolAddress,omitempty"`
	PoolID      string    `json:"poolId,omitempty"`
	PoolType    string    `json:"poolType"`
	Token0      TokenInfo `json:"token0"`
	Token1      TokenInfo `json:"token1"`
	Fee         uint32    `json:"fee"`
	Hooks       string    `json:"hooks,omitempty"`
}

// ListResponse is the pools API response payload.
type ListResponse struct {
	Items []PoolInfo `json:"items"`
	Count int        `json:"count"`
}

// AppService lists tracked pool metadata across protocols.
type AppService struct {
	univ3Pools       marketuniv3.PoolRepository
	pancakePools     marketpancake.PoolRepository
	v4Pools          marketuniv4.PoolRepository
	univ3Registry    marketuniv3.PoolRegistry
	pancakeRegistry  marketpancake.PoolRegistry
	v4Registry       marketuniv4.PoolRegistry
	tokens           *assetapp.TokenMetadataService
	chain            *ChainReaders
}

func NewAppService(
	univ3Pools marketuniv3.PoolRepository,
	pancakePools marketpancake.PoolRepository,
	v4Pools marketuniv4.PoolRepository,
	univ3Registry marketuniv3.PoolRegistry,
	pancakeRegistry marketpancake.PoolRegistry,
	v4Registry marketuniv4.PoolRegistry,
	tokens *assetapp.TokenMetadataService,
	chain *ChainReaders,
) *AppService {
	return &AppService{
		univ3Pools:      univ3Pools,
		pancakePools:    pancakePools,
		v4Pools:         v4Pools,
		univ3Registry:   univ3Registry,
		pancakeRegistry: pancakeRegistry,
		v4Registry:      v4Registry,
		tokens:          tokens,
		chain:           chain,
	}
}

// List returns metadata for all tracked pools in the system.
func (s *AppService) List(ctx context.Context) (*ListResponse, error) {
	if s == nil {
		return &ListResponse{}, nil
	}

	pools := make([]PoolInfo, 0)
	if err := s.appendUniv3Pools(ctx, &pools); err != nil {
		return nil, err
	}
	if err := s.appendPancakePools(ctx, &pools); err != nil {
		return nil, err
	}
	if err := s.appendV4Pools(ctx, &pools); err != nil {
		return nil, err
	}

	tokenAddresses := make([]common.Address, 0, len(pools)*2)
	for _, pool := range pools {
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

func (s *AppService) appendUniv3Pools(ctx context.Context, pools *[]PoolInfo) error {
	if s.univ3Registry == nil || s.univ3Pools == nil {
		return nil
	}
	addresses, err := s.univ3Registry.List(ctx)
	if err != nil {
		return fmt.Errorf("list univ3 pools: %w", err)
	}
	for _, address := range addresses {
		pool, err := s.univ3Pools.Get(ctx, address)
		if err != nil {
			return fmt.Errorf("load univ3 pool %s: %w", address.Hex(), err)
		}
		if pool == nil {
			continue
		}
		*pools = append(*pools, PoolInfo{
			PoolAddress: address.Hex(),
			PoolType:    PoolTypeUniv3,
			Token0:      tokenInfoFromAddress(pool.Token0),
			Token1:      tokenInfoFromAddress(pool.Token1),
			Fee:         pool.Fee,
		})
	}
	return nil
}

func (s *AppService) appendPancakePools(ctx context.Context, pools *[]PoolInfo) error {
	if s.pancakeRegistry == nil || s.pancakePools == nil {
		return nil
	}
	addresses, err := s.pancakeRegistry.List(ctx)
	if err != nil {
		return fmt.Errorf("list pancakev3 pools: %w", err)
	}
	for _, address := range addresses {
		pool, err := s.pancakePools.Get(ctx, address)
		if err != nil {
			return fmt.Errorf("load pancakev3 pool %s: %w", address.Hex(), err)
		}
		if pool == nil {
			continue
		}
		*pools = append(*pools, PoolInfo{
			PoolAddress: address.Hex(),
			PoolType:    PoolTypePancakeV3,
			Token0:      tokenInfoFromAddress(pool.Token0),
			Token1:      tokenInfoFromAddress(pool.Token1),
			Fee:         pool.Fee,
		})
	}
	return nil
}

func (s *AppService) appendV4Pools(ctx context.Context, pools *[]PoolInfo) error {
	if s.v4Registry == nil || s.v4Pools == nil {
		return nil
	}

	poolIDs, err := s.v4Registry.List(ctx)
	if err != nil {
		return fmt.Errorf("list univ4 pools: %w", err)
	}
	for _, poolID := range poolIDs {
		pool, err := s.v4Pools.Get(ctx, poolID)
		if err != nil {
			return fmt.Errorf("load univ4 pool %s: %w", poolID.String(), err)
		}
		if pool == nil {
			continue
		}
		hooks := ""
		if pool.Key.Hooks != (common.Address{}) {
			hooks = pool.Key.Hooks.Hex()
		}
		*pools = append(*pools, PoolInfo{
			PoolID:   poolID.String(),
			PoolType: PoolTypeUniv4,
			Token0:   tokenInfoFromAddress(pool.Key.Currency0),
			Token1:   tokenInfoFromAddress(pool.Key.Currency1),
			Fee:      pool.Key.Fee,
			Hooks:    hooks,
		})
	}
	return nil
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
