package cache

import "context"

// NoopTokenCache 在未启用 Redis 时提供空实现。
type NoopTokenCache struct{}

func NewNoopTokenCache() TokenCache { return NoopTokenCache{} }

func (NoopTokenCache) GetTokenInfo(context.Context, string, string) (*TokenInfo, error) {
	return nil, nil
}
func (NoopTokenCache) SetTokenInfo(context.Context, string, string, *TokenInfo) error {
	return nil
}
func (NoopTokenCache) Close() error { return nil }
