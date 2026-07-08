package syncapp

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum/common"
)

type linearBlockReader struct {
	headers map[uint64]blockchain.BlockHeader
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

	reorg, err := DetectReorg(context.Background(), nil, 128, local, remote)
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

	ancestor, err := FindCommonAncestor(context.Background(), reader, 128, local, remote)
	if err != nil {
		t.Fatalf("FindCommonAncestor() error = %v", err)
	}
	if ancestor != 100 {
		t.Fatalf("expected ancestor block 100, got %d", ancestor)
	}
}

func (r *linearBlockReader) GetBlockHeader(_ context.Context, blockNumber uint64) (blockchain.BlockHeader, error) {
	header, ok := r.headers[blockNumber]
	if !ok {
		return blockchain.BlockHeader{}, fmt.Errorf("block %d not found", blockNumber)
	}
	return header, nil
}

func (r *linearBlockReader) GetLatestBlockHeader(_ context.Context) (blockchain.BlockHeader, error) {
	return r.headers[uint64(len(r.headers)-1)], nil
}
