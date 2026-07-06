package memory

import (
	"context"
	"sync"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	"github.com/ethereum/go-ethereum/common"
)

// TokenRepository is an in-memory asset.TokenRepository.
type TokenRepository struct {
	mu     sync.RWMutex
	tokens map[common.Address]*asset.Token
}

func NewTokenRepository() *TokenRepository {
	return &TokenRepository{tokens: make(map[common.Address]*asset.Token)}
}

func (r *TokenRepository) Save(_ context.Context, token *asset.Token) error {
	if token == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	copyToken := *token
	r.tokens[token.Address] = &copyToken
	return nil
}

func (r *TokenRepository) Get(_ context.Context, address common.Address) (*asset.Token, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	token, ok := r.tokens[address]
	if !ok {
		return nil, nil
	}
	copyToken := *token
	return &copyToken, nil
}

func (r *TokenRepository) GetMany(_ context.Context, addresses []common.Address) (map[common.Address]*asset.Token, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[common.Address]*asset.Token, len(addresses))
	for _, address := range addresses {
		token, ok := r.tokens[address]
		if !ok {
			continue
		}
		copyToken := *token
		out[address] = &copyToken
	}
	return out, nil
}
