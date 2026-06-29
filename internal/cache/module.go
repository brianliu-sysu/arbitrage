package cache

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/arbitrage/internal/config"
	"go.uber.org/fx"
)

// Module 提供 token metadata cache 依赖。
var Module = fx.Module(
	"cache",
	fx.Provide(newTokenCache),
	fx.Provide(newAppliedLogCache),
	fx.Invoke(registerCacheLifecycle),
	fx.Invoke(registerAppliedLogCacheLifecycle),
)

func newTokenCache(cfg *config.AppConfig) (TokenCache, error) {
	if cfg.RedisURL == "" {
		return NewNoopTokenCache(), nil
	}
	c, err := NewRedisTokenCache(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("connect redis: %w", err)
	}
	if c == nil {
		return NewNoopTokenCache(), nil
	}
	return c, nil
}

func registerCacheLifecycle(lc fx.Lifecycle, c TokenCache) {
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			return c.Close()
		},
	})
}

func newAppliedLogCache(cfg *config.AppConfig) (AppliedLogCache, error) {
	if cfg.RedisURL == "" {
		return NewNoopAppliedLogCache(), nil
	}
	c, err := NewRedisAppliedLogCache(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("connect redis for applied-log cache: %w", err)
	}
	if c == nil {
		return NewNoopAppliedLogCache(), nil
	}
	return c, nil
}

func registerAppliedLogCacheLifecycle(lc fx.Lifecycle, c AppliedLogCache) {
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			return c.Close()
		},
	})
}
