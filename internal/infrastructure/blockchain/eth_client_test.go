package blockchain

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
)

func TestSubscribeNewHeadRequiresWebSocket(t *testing.T) {
	client := &EthClient{}
	_, err := client.SubscribeNewHead(context.Background(), make(chan *types.Header))
	if err == nil {
		t.Fatal("expected error without websocket client")
	}
}
