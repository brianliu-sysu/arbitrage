package blockchain

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestNewHeadLogFetcherUsesConfiguredTopics(t *testing.T) {
	first := common.HexToHash("0x1")
	second := common.HexToHash("0x2")

	fetcher := NewHeadLogFetcher(nil, []common.Hash{first}, []common.Hash{second, first})

	if len(fetcher.topics) != 2 {
		t.Fatalf("expected 2 unique topics, got %d", len(fetcher.topics))
	}
	if fetcher.topics[0] != first || fetcher.topics[1] != second {
		t.Fatalf("unexpected configured topics: %v", fetcher.topics)
	}
}
