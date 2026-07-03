package blockchain

import (
	"context"
	"fmt"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum/core/types"
)

// HeadSubscriber streams canonical block headers.
type HeadSubscriber struct {
	client *EthClient
}

func NewHeadSubscriber(client *EthClient) *HeadSubscriber {
	return &HeadSubscriber{client: client}
}

func (s *HeadSubscriber) SubscribeNewHead(ctx context.Context) (<-chan domainchain.BlockHeader, error) {
	source := make(chan *types.Header)
	subscription, err := s.client.SubscribeNewHead(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("subscribe new head: %w", err)
	}

	headers := make(chan domainchain.BlockHeader)
	go func() {
		defer close(headers)
		defer subscription.Unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-subscription.Err():
				if err != nil {
					return
				}
			case header, ok := <-source:
				if !ok {
					return
				}
				select {
				case headers <- headerToDomain(header):
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return headers, nil
}
