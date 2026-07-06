package blockchain

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestSubscribeNewHeadRequiresWebSocket(t *testing.T) {
	client := &EthClient{}
	_, err := client.SubscribeNewHead(context.Background(), make(chan *types.Header))
	if err == nil {
		t.Fatal("expected error without websocket client")
	}
}

func TestCallContractAfterClose(t *testing.T) {
	client := &EthClient{}
	client.Close()

	_, err := client.CallContract(context.Background(), common.Address{}, nil, 0)
	if !errors.Is(err, ErrClientClosed) {
		t.Fatalf("expected ErrClientClosed, got %v", err)
	}
}
