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

func TestRunStartupUsesCaughtUpHeadForLiveSync(t *testing.T) {
	reader := &startupBlockReader{
		headers: []blockchain.BlockHeader{
			{Number: 100, Hash: common.HexToHash("0x100")},
			{Number: 105, Hash: common.HexToHash("0x105")},
		},
	}

	startCalls := make([]uint64, 0, 2)
	catchupCalls := make([]uint64, 0, 1)
	var localHead blockchain.BlockHeader

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
		SetLocalHead:   func(head blockchain.BlockHeader) { localHead = head },
		SetSystemReady: func(bool) {},
		RunHeadSync:    func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("run startup: %v", err)
	}

	if len(startCalls) != 1 || startCalls[0] != 100 {
		t.Fatalf("expected StartAll at initial head 100, got %v", startCalls)
	}
	if len(catchupCalls) != 1 || catchupCalls[0] != 100 {
		t.Fatalf("expected CatchUpAll at initial head 100, got %v", catchupCalls)
	}
	if localHead.Number != 100 || localHead.Hash != common.HexToHash("0x100") {
		t.Fatalf("expected local head to stay at caught-up block 100, got %+v", localHead)
	}
	if reader.calls != 1 {
		t.Fatalf("expected latest header to be read once, got %d", reader.calls)
	}
}
