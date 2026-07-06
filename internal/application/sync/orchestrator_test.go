package syncapp_test

import (
	"context"
	"testing"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum/common"
)

type startupBlockReader struct {
	headers []blockchain.BlockHeader
	calls   int
}

func (r *startupBlockReader) GetLatestBlockHeader(context.Context) (blockchain.BlockHeader, error) {
	idx := r.calls
	if idx >= len(r.headers) {
		idx = len(r.headers) - 1
	}
	r.calls++
	return r.headers[idx], nil
}

func (r *startupBlockReader) GetBlockHeader(_ context.Context, blockNumber uint64) (blockchain.BlockHeader, error) {
	for _, header := range r.headers {
		if header.Number == blockNumber {
			return header, nil
		}
	}
	return blockchain.BlockHeader{}, nil
}

func TestRunStartupRefreshesBootstrapAtLatestHead(t *testing.T) {
	reader := &startupBlockReader{
		headers: []blockchain.BlockHeader{
			{Number: 100, Hash: common.HexToHash("0x100")},
			{Number: 105, Hash: common.HexToHash("0x105")},
		},
	}

	startCalls := make([]uint64, 0, 2)
	catchupCalls := make([]uint64, 0, 1)

	err := syncapp.RunStartup(context.Background(), reader, syncapp.SyncPhases{
		StartAll: func(_ context.Context, blockNumber uint64) error {
			startCalls = append(startCalls, blockNumber)
			return nil
		},
		CatchUpAll: func(_ context.Context, blockNumber uint64) error {
			catchupCalls = append(catchupCalls, blockNumber)
			return nil
		},
		MarkPoolsReady: func(context.Context) error { return nil },
		SetLocalHead:   func(blockchain.BlockHeader) {},
		SetSystemReady: func(bool) {},
		RunHeadSync:    func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("run startup: %v", err)
	}

	if len(startCalls) != 2 {
		t.Fatalf("expected StartAll twice, got %d calls: %v", len(startCalls), startCalls)
	}
	if startCalls[0] != 100 || startCalls[1] != 105 {
		t.Fatalf("unexpected StartAll blocks: %v", startCalls)
	}
	if len(catchupCalls) != 1 || catchupCalls[0] != 105 {
		t.Fatalf("expected CatchUpAll at refreshed head 105, got %v", catchupCalls)
	}
}
