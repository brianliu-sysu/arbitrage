package blockchain

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/ethclient"
)

// Client 封装 HTTP RPC ethclient，复用连接并在连接错误时重置。
type Client struct {
	dialURL string
	maskURL string

	mu     sync.Mutex
	client *ethclient.Client
	inited bool
}

// NewClient 创建 RPC 客户端。maskURL 用于日志脱敏。
func NewClient(dialURL string) *Client {
	return &Client{
		dialURL: dialURL,
		maskURL: maskAPIKey(dialURL),
	}
}

// Eth 返回底层 ethclient，懒加载。
func (c *Client) Eth() (*ethclient.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inited && c.client != nil {
		return c.client, nil
	}
	client, err := ethclient.Dial(c.dialURL)
	if err != nil {
		c.inited = true
		return nil, fmt.Errorf("%s", maskAPIKey(err.Error()))
	}
	c.client = client
	c.inited = true
	return c.client, nil
}

// Reset 关闭并重置连接。
func (c *Client) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		c.client.Close()
		c.client = nil
	}
	c.inited = false
}

// Close 释放资源。
func (c *Client) Close() {
	c.Reset()
}

// MaskURL 返回脱敏后的 URL。
func (c *Client) MaskURL() string {
	return c.maskURL
}

func maskAPIKey(rawURL string) string {
	lastSlash := strings.LastIndex(rawURL, "/")
	if lastSlash < 0 || lastSlash >= len(rawURL)-1 {
		return rawURL
	}
	prefix := rawURL[:lastSlash+1]
	keyPart := rawURL[lastSlash+1:]
	if idx := strings.Index(keyPart, "?"); idx >= 0 {
		return prefix + "***" + keyPart[idx:]
	}
	if len(keyPart) < 16 {
		return rawURL
	}
	return prefix + "***"
}
