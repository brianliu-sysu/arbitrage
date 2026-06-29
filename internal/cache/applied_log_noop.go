package cache

import "context"

// NoopAppliedLogCache 在未启用 Redis 时提供空实现。
type NoopAppliedLogCache struct{}

func NewNoopAppliedLogCache() AppliedLogCache { return NoopAppliedLogCache{} }

func (NoopAppliedLogCache) MarkAppliedIfNew(context.Context, string, string, uint64, string, uint) (bool, error) {
	return true, nil
}

func (NoopAppliedLogCache) Close() error { return nil }
