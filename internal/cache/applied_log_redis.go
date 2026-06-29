package cache

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// DefaultAppliedLogTTL 覆盖重连+回放窗口的默认去重保留时长。
	DefaultAppliedLogTTL = 30 * time.Minute
)

// RedisAppliedLogCache 使用 Redis SetNX 记录近期已应用日志。
type RedisAppliedLogCache struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedisAppliedLogCache(redisURL string) (*RedisAppliedLogCache, error) {
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

	return &RedisAppliedLogCache{
		client: client,
		ttl:    DefaultAppliedLogTTL,
	}, nil
}

func buildAppliedLogKey(chainName, poolAddress string, blockNumber uint64, txHash string, logIndex uint) string {
	return fmt.Sprintf(
		"applied_log:%s:%s:%d:%s:%d",
		chainName,
		strings.ToLower(poolAddress),
		blockNumber,
		strings.ToLower(txHash),
		logIndex,
	)
}

func (c *RedisAppliedLogCache) MarkAppliedIfNew(ctx context.Context, chainName, poolAddress string, blockNumber uint64, txHash string, logIndex uint) (bool, error) {
	if c == nil || c.client == nil {
		return true, nil
	}

	key := buildAppliedLogKey(chainName, poolAddress, blockNumber, txHash, logIndex)
	ok, err := c.client.SetNX(ctx, key, "1", c.ttl).Result()
	if err != nil {
		return false, fmt.Errorf("redis setnx %s: %w", key, err)
	}
	return ok, nil
}

func (c *RedisAppliedLogCache) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}
