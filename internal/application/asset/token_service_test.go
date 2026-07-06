package assetapp_test

import (
	"context"
	"testing"

	assetapp "github.com/brianliu-sysu/uniswapv3/internal/application/asset"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	"github.com/ethereum/go-ethereum/common"
)

type memoryTokenRepo struct {
	tokens map[common.Address]*asset.Token
}

func (r *memoryTokenRepo) Save(_ context.Context, token *asset.Token) error {
	if r.tokens == nil {
		r.tokens = make(map[common.Address]*asset.Token)
	}
	copyToken := *token
	r.tokens[token.Address] = &copyToken
	return nil
}

func (r *memoryTokenRepo) Get(_ context.Context, address common.Address) (*asset.Token, error) {
	token, ok := r.tokens[address]
	if !ok {
		return nil, nil
	}
	copyToken := *token
	return &copyToken, nil
}

func (r *memoryTokenRepo) GetMany(_ context.Context, addresses []common.Address) (map[common.Address]*asset.Token, error) {
	out := make(map[common.Address]*asset.Token)
	for _, address := range addresses {
		if token, ok := r.tokens[address]; ok {
			copyToken := *token
			out[address] = &copyToken
		}
	}
	return out, nil
}

func TestTokenMetadataServiceResolvesNativeETH(t *testing.T) {
	service := assetapp.NewTokenMetadataService(&memoryTokenRepo{}, nil)
	tokens, err := service.Resolve(context.Background(), []common.Address{common.Address{}})
	if err != nil {
		t.Fatalf("resolve native eth: %v", err)
	}
	token := tokens[common.Address{}]
	if token == nil || token.Symbol != "ETH" || token.Decimal != 18 {
		t.Fatalf("unexpected native token: %#v", token)
	}
}
