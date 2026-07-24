package runner

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum/common"
)

type linearBlockReader struct {
	headers    map[uint64]blockchain.BlockHeader
	batchCalls int
}

func newLinearBlockReader(count uint64) *linearBlockReader {
	reader := &linearBlockReader{headers: make(map[uint64]blockchain.BlockHeader, count+1)}
	for i := uint64(0); i <= count; i++ {
		var parent common.Hash
		if i > 0 {
			parent = reader.headers[i-1].Hash
		}
		reader.headers[i] = blockchain.BlockHeader{
			Number:     i,
			Hash:       common.BigToHash(new(big.Int).SetUint64(i)),
			ParentHash: parent,
		}
	}
	return reader
}

func TestDetectReorgSkipsNonAdjacentHeadGap(t *testing.T) {
	local := blockchain.BlockHeader{Number: 100, Hash: common.HexToHash("0x100")}
	remote := blockchain.BlockHeader{Number: 150, Hash: common.HexToHash("0x150")}

	reorg, err := DetectReorg(context.Background(), nil, nil, 128, local, remote)
	if err != nil {
		t.Fatalf("DetectReorg() error = %v", err)
	}
	if reorg != nil {
		t.Fatalf("expected nil reorg for head gap, got %+v", reorg)
	}
}

func TestFindCommonAncestorAcrossLargeHeightGap(t *testing.T) {
	reader := newLinearBlockReader(200)
	local := reader.headers[100]
	remote := reader.headers[200]
	history := newBlockHistory(128)
	for blockNumber := uint64(0); blockNumber <= local.Number; blockNumber++ {
		history.Commit(reader.headers[blockNumber])
	}

	ancestor, err := FindCommonAncestor(context.Background(), history, reader, 128, local, remote)
	if err != nil {
		t.Fatalf("FindCommonAncestor() error = %v", err)
	}
	if ancestor != 100 {
		t.Fatalf("expected ancestor block 100, got %d", ancestor)
	}
	if reader.batchCalls != 1 {
		t.Fatalf("expected one batch header request, got %d", reader.batchCalls)
	}
}

func TestFindCommonAncestorUsesLocalForkHistory(t *testing.T) {
	ancestor := blockchain.BlockHeader{Number: 8, Hash: common.HexToHash("0x8")}
	local9 := blockchain.BlockHeader{Number: 9, Hash: common.HexToHash("0xa9"), ParentHash: ancestor.Hash}
	local10 := blockchain.BlockHeader{Number: 10, Hash: common.HexToHash("0xa10"), ParentHash: local9.Hash}
	remote9 := blockchain.BlockHeader{Number: 9, Hash: common.HexToHash("0xb9"), ParentHash: ancestor.Hash}
	remote10 := blockchain.BlockHeader{Number: 10, Hash: common.HexToHash("0xb10"), ParentHash: remote9.Hash}
	reader := &linearBlockReader{headers: map[uint64]blockchain.BlockHeader{
		8: ancestor, 9: remote9, 10: remote10,
	}}
	history := newBlockHistory(16)
	history.Commit(ancestor)
	history.Commit(local9)
	history.Commit(local10)

	got, err := FindCommonAncestor(context.Background(), history, reader, 2, local10, remote10)
	if err != nil {
		t.Fatalf("FindCommonAncestor() error = %v", err)
	}
	if got != ancestor.Number {
		t.Fatalf("expected ancestor block %d, got %d", ancestor.Number, got)
	}
	if reader.batchCalls != 1 {
		t.Fatalf("expected one batch header request, got %d", reader.batchCalls)
	}
}

func (r *linearBlockReader) GetBlockHeaders(_ context.Context, blockNumbers []uint64) (map[uint64]blockchain.BlockHeader, error) {
	r.batchCalls++
	headers := make(map[uint64]blockchain.BlockHeader, len(blockNumbers))
	for _, blockNumber := range blockNumbers {
		header, ok := r.headers[blockNumber]
		if !ok {
			return nil, fmt.Errorf("block %d not found", blockNumber)
		}
		headers[blockNumber] = header
	}
	return headers, nil
}
