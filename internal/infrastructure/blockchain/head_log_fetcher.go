package blockchain

import (
	"context"
	"fmt"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

// HeadLogFetcher fetches all supported protocol event logs with one RPC request.
type HeadLogFetcher struct {
	client *EthClient
	topics []common.Hash
}

func NewHeadLogFetcher(client *EthClient, topicGroups ...[]common.Hash) *HeadLogFetcher {
	return &HeadLogFetcher{
		client: client,
		topics: uniqueTopics(topicGroups...),
	}
}

func (f *HeadLogFetcher) FetchBlockLogs(ctx context.Context, blockHash common.Hash) ([]domainchain.RawLog, error) {
	if f == nil || f.client == nil {
		return nil, fmt.Errorf("head log fetcher is not configured")
	}
	logs, err := f.client.FilterLogs(ctx, ethereum.FilterQuery{
		BlockHash: &blockHash,
		Topics:    [][]common.Hash{f.topics},
	})
	if err != nil {
		return nil, fmt.Errorf("filter block %s logs: %w", blockHash.Hex(), err)
	}
	return rawLogsFromEth(logs), nil
}

func uniqueTopics(groups ...[]common.Hash) []common.Hash {
	seen := make(map[common.Hash]struct{})
	topics := make([]common.Hash, 0)
	for _, group := range groups {
		for _, topic := range group {
			if _, ok := seen[topic]; ok {
				continue
			}
			seen[topic] = struct{}{}
			topics = append(topics, topic)
		}
	}
	return topics
}
