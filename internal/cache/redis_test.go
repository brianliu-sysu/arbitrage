package cache

import (
	"context"
	"testing"
	"time"
)

func TestNewRedisTokenCacheEmptyURL(t *testing.T) {
	c, err := NewRedisTokenCache("")
	if err != nil {
		t.Fatalf("NewRedisTokenCache with empty URL: %v", err)
	}
	if c != nil {
		t.Error("expected nil cache for empty URL")
	}
}

func TestNewRedisTokenCacheInvalidURL(t *testing.T) {
	_, err := NewRedisTokenCache("not-a-valid-url://!@#$")
	if err == nil {
		t.Error("expected error for invalid Redis URL")
	}
}

func TestNewRedisTokenCacheUnreachable(t *testing.T) {
	_, err := NewRedisTokenCache("redis://127.0.0.1:12345/0")
	if err == nil {
		t.Skip("unexpected success — Redis might be running locally")
	}
	t.Logf("expected error for unreachable Redis: %v", err)
}

func TestBuildKey(t *testing.T) {
	tests := []struct {
		chain    string
		token    string
		expected string
	}{
		{"ethereum", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", "token_metadata:ethereum:0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"},
		{"polygon", "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", "token_metadata:polygon:0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"},
		{"", "", "token_metadata::"},
	}
	for _, tt := range tests {
		got := buildKey(tt.chain, tt.token)
		if got != tt.expected {
			t.Errorf("buildKey(%q, %q) = %q, want %q", tt.chain, tt.token, got, tt.expected)
		}
	}
}

func TestGetTokenInfoOnNilReceiver(t *testing.T) {
	var c *RedisTokenCache
	ctx := context.Background()
	info, err := c.GetTokenInfo(ctx, "ethereum", "0x123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Error("expected nil on nil receiver")
	}
}

func TestSetTokenInfoOnNilReceiver(t *testing.T) {
	var c *RedisTokenCache
	ctx := context.Background()
	err := c.SetTokenInfo(ctx, "ethereum", "0x123", &TokenInfo{Symbol: "TEST", Decimals: 18})
	if err != nil {
		t.Fatalf("unexpected error on nil receiver: %v", err)
	}
}

func TestSetTokenInfoWithNilInfo(t *testing.T) {
	var c *RedisTokenCache
	ctx := context.Background()
	// nil receiver + nil info → still nil (nil receiver check comes first)
	err := c.SetTokenInfo(ctx, "ethereum", "0x123", nil)
	if err != nil {
		t.Fatalf("unexpected error on nil receiver with nil info: %v", err)
	}
}

func TestCloseOnNilReceiver(t *testing.T) {
	var c *RedisTokenCache
	if err := c.Close(); err != nil {
		t.Fatalf("Close on nil receiver: %v", err)
	}
}

func TestTokenInfoStruct(t *testing.T) {
	ti := &TokenInfo{
		Symbol:   "USDC",
		Decimals: 6,
	}
	if ti.Symbol != "USDC" {
		t.Errorf("Symbol = %q, want USDC", ti.Symbol)
	}
	if ti.Decimals != 6 {
		t.Errorf("Decimals = %d, want 6", ti.Decimals)
	}
}

func TestDefaultTokenCacheTTL(t *testing.T) {
	if DefaultTokenCacheTTL != 1*time.Hour {
		t.Errorf("DefaultTokenCacheTTL = %v, want 1h", DefaultTokenCacheTTL)
	}
}
