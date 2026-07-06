package assetapp

import (
	"context"
	"sort"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	"github.com/ethereum/go-ethereum/common"
)

// ERC20MetadataFetcher loads token metadata from chain.
type ERC20MetadataFetcher interface {
	FetchMany(ctx context.Context, addresses []common.Address) (map[common.Address]*asset.Token, error)
}

// TokenMetadataService resolves token metadata from cache and optional on-chain fetcher.
type TokenMetadataService struct {
	repo    asset.TokenRepository
	fetcher ERC20MetadataFetcher
}

func NewTokenMetadataService(repo asset.TokenRepository, fetcher ERC20MetadataFetcher) *TokenMetadataService {
	return &TokenMetadataService{repo: repo, fetcher: fetcher}
}

// Resolve returns token metadata for the given addresses, fetching and caching misses when possible.
func (s *TokenMetadataService) Resolve(ctx context.Context, addresses []common.Address) (map[common.Address]*asset.Token, error) {
	unique := dedupeAddresses(addresses)
	if len(unique) == 0 {
		return map[common.Address]*asset.Token{}, nil
	}
	if s == nil || s.repo == nil {
		return fallbackTokens(unique), nil
	}

	cached, err := s.repo.GetMany(ctx, unique)
	if err != nil {
		return nil, err
	}

	for _, address := range unique {
		if !asset.IsNativeETH(address) {
			continue
		}
		native := asset.NativeETHToken()
		cached[address] = native
		if err := s.repo.Save(ctx, native); err != nil {
			return nil, err
		}
	}

	missing := make([]common.Address, 0)
	for _, address := range unique {
		if asset.IsNativeETH(address) {
			continue
		}
		if _, ok := cached[address]; !ok {
			missing = append(missing, address)
		}
	}

	if len(missing) > 0 && s.fetcher != nil {
		fetched, err := s.fetcher.FetchMany(ctx, missing)
		if err != nil {
			return nil, err
		}
		for address, token := range fetched {
			if token == nil {
				token = &asset.Token{Address: address}
			}
			if err := s.repo.Save(ctx, token); err != nil {
				return nil, err
			}
			cached[address] = token
		}
	}

	for _, address := range unique {
		if _, ok := cached[address]; ok {
			continue
		}
		if asset.IsNativeETH(address) {
			cached[address] = asset.NativeETHToken()
			continue
		}
		cached[address] = &asset.Token{Address: address}
	}
	return cached, nil
}

func dedupeAddresses(addresses []common.Address) []common.Address {
	seen := make(map[common.Address]struct{}, len(addresses))
	out := make([]common.Address, 0, len(addresses))
	for _, address := range addresses {
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		out = append(out, address)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Hex() < out[j].Hex()
	})
	return out
}

func fallbackTokens(addresses []common.Address) map[common.Address]*asset.Token {
	out := make(map[common.Address]*asset.Token, len(addresses))
	for _, address := range addresses {
		if asset.IsNativeETH(address) {
			out[address] = asset.NativeETHToken()
			continue
		}
		out[address] = &asset.Token{Address: address}
	}
	return out
}
