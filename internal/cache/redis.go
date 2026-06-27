// Package cache provides a Redis-backed token metadata cache.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// DefaultTokenCacheTTL is the default TTL for cached token metadata.
	DefaultTokenCacheTTL = 1 * time.Hour
)

// TokenInfo represents cached token metadata.
type TokenInfo struct {
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
}

// TokenCache is the caching interface for token metadata.
type TokenCache interface {
	// GetTokenInfo retrieves token metadata from the cache.
	// Returns nil, nil on cache miss (key not found).
	GetTokenInfo(ctx context.Context, chainName, tokenAddress string) (*TokenInfo, error)
	// SetTokenInfo stores token metadata in the cache with the default TTL.
	SetTokenInfo(ctx context.Context, chainName, tokenAddress string, info *TokenInfo) error
	// Close releases the Redis connection.
	Close() error
}

// RedisTokenCache implements TokenCache using Redis.
type RedisTokenCache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisTokenCache creates a new Redis-backed token cache.
// redisURL is the Redis connection string (e.g., "redis://localhost:6379/0").
// Returns nil if redisURL is empty (cache disabled).
func NewRedisTokenCache(redisURL string) (*RedisTokenCache, error) {
	if redisURL == "" {
		return nil, nil
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &RedisTokenCache{
		client: client,
		ttl:    DefaultTokenCacheTTL,
	}, nil
}

// buildKey constructs the Redis key for a token metadata entry.
func buildKey(chainName, tokenAddress string) string {
	return fmt.Sprintf("token_metadata:%s:%s", chainName, strings.ToLower(tokenAddress))
}

// GetTokenInfo retrieves token metadata from the Redis cache.
// Returns nil, nil on cache miss.
func (c *RedisTokenCache) GetTokenInfo(ctx context.Context, chainName, tokenAddress string) (*TokenInfo, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}

	key := buildKey(chainName, tokenAddress)
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // cache miss
		}
		return nil, fmt.Errorf("redis get %s: %w", key, err)
	}

	var info TokenInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("unmarshal token info for %s: %w", key, err)
	}
	return &info, nil
}

// SetTokenInfo stores token metadata in the Redis cache with the configured TTL.
func (c *RedisTokenCache) SetTokenInfo(ctx context.Context, chainName, tokenAddress string, info *TokenInfo) error {
	if c == nil || c.client == nil {
		return nil
	}
	if info == nil {
		return fmt.Errorf("token info is nil")
	}

	key := buildKey(chainName, tokenAddress)
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal token info for %s: %w", key, err)
	}

	if err := c.client.Set(ctx, key, data, c.ttl).Err(); err != nil {
		return fmt.Errorf("redis set %s: %w", key, err)
	}
	return nil
}

// Close releases the Redis connection pool.
func (c *RedisTokenCache) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}
