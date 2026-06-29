package store

import (
	"context"
)

// NoopStore 在禁用 DB 时提供空实现，避免 nil 依赖分支。
type NoopStore struct{}

func NewNoopStore() Storer { return NoopStore{} }

func (NoopStore) Save(context.Context, *PoolSnapshot) error { return nil }
func (NoopStore) SaveHistory(context.Context, *PoolSnapshot) error {
	return nil
}
func (NoopStore) Load(context.Context, string, string) (*PoolSnapshot, error) { return nil, nil }
func (NoopStore) LoadAll(context.Context, string) (map[string]*PoolSnapshot, error) {
	return map[string]*PoolSnapshot{}, nil
}
func (NoopStore) LoadTokenMetadata(context.Context, string, string) (*TokenMetadata, error) {
	return nil, nil
}
func (NoopStore) SaveTokenMetadata(context.Context, *TokenMetadata) error { return nil }
func (NoopStore) Close()                                                   {}
