package blockchain

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// WSHeaderSubscriber 通过 WebSocket ethclient 订阅 newHeads。
type WSHeaderSubscriber struct {
	wsURL  string
	logger logx.Logger

	mu     sync.Mutex
	client *ethclient.Client
}

// NewWSHeaderSubscriber 创建 WS 区块头订阅器。
func NewWSHeaderSubscriber(wsURL string, logger logx.Logger) *WSHeaderSubscriber {
	return &WSHeaderSubscriber{wsURL: wsURL, logger: logger}
}

// SubscribeNewHead 订阅新区块头。
func (s *WSHeaderSubscriber) SubscribeNewHead(ctx context.Context) (<-chan *types.Header, func(), error) {
	client, err := ethclient.Dial(s.wsURL)
	if err != nil {
		return nil, nil, fmt.Errorf("dial ws: %w", err)
	}
	headers := make(chan *types.Header, 16)
	sub, err := client.SubscribeNewHead(ctx, headers)
	if err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("subscribe new head: %w", err)
	}
	unsub := func() {
		sub.Unsubscribe()
		client.Close()
	}
	go func() {
		for err := range sub.Err() {
			if err != nil {
				s.logger.Warn("new head subscription error", "error", err)
			}
		}
	}()
	return headers, unsub, nil
}

// RPCBlockNumber 通过 HTTP RPC 获取当前区块号（CatchUp 用）。
func RPCBlockNumber(ctx context.Context, client *Client) (uint64, error) {
	eth, err := client.Eth()
	if err != nil {
		return 0, err
	}
	return eth.BlockNumber(ctx)
}

// RetryDial 带退避重连 WS。
func RetryDial(ctx context.Context, wsURL string, maxAttempts int) (*ethclient.Client, error) {
	var lastErr error
	backoff := time.Second
	for i := 0; i < maxAttempts; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		c, err := ethclient.Dial(wsURL)
		if err == nil {
			return c, nil
		}
		lastErr = err
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
	return nil, lastErr
}
