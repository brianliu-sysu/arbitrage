package syncapp

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
)

// DetectReorg checks whether remoteHead diverges from localHead and returns a reorg event.
func DetectReorg(
	ctx context.Context,
	blocks BlockReader,
	maxDepth uint64,
	localHead, remoteHead blockchain.BlockHeader,
) (*blockchain.Reorg, error) {
	if localHead.Number == 0 || remoteHead.Number == 0 {
		return nil, nil
	}
	if remoteHead.Number == localHead.Number+1 && remoteHead.ParentHash == localHead.Hash {
		return nil, nil
	}
	if localHead.Hash == remoteHead.Hash {
		return nil, nil
	}
	// Non-adjacent heads are head gaps; catch up first instead of walking forks.
	if remoteHead.Number > localHead.Number+1 {
		return nil, nil
	}

	ancestor, err := FindCommonAncestor(ctx, blocks, maxDepth, localHead, remoteHead)
	if err != nil {
		return nil, err
	}
	if ancestor >= localHead.Number {
		return nil, nil
	}

	reorg := blockchain.NewReorg(remoteHead.Number, localHead, remoteHead, ancestor)
	return &reorg, nil
}

// FindCommonAncestor walks both chains backward until a shared block is found.
func FindCommonAncestor(
	ctx context.Context,
	blocks BlockReader,
	maxDepth uint64,
	localHead, remoteHead blockchain.BlockHeader,
) (uint64, error) {
	localBlock := localHead
	remoteBlock := remoteHead

	heightDiff := uint64(0)
	if localHead.Number < remoteHead.Number {
		heightDiff = remoteHead.Number - localHead.Number
	} else if localHead.Number > remoteHead.Number {
		heightDiff = localHead.Number - remoteHead.Number
	}
	iterLimit := maxDepth + heightDiff

	for depth := uint64(0); depth <= iterLimit; depth++ {
		if localBlock.Number == 0 || remoteBlock.Number == 0 {
			break
		}

		if localBlock.Number == remoteBlock.Number {
			if localBlock.Hash == remoteBlock.Hash {
				return localBlock.Number, nil
			}
			var err error
			localBlock, remoteBlock, err = stepBack(ctx, blocks, localBlock, remoteBlock)
			if err != nil {
				return 0, err
			}
			continue
		}

		if localBlock.Number > remoteBlock.Number {
			header, err := blocks.GetBlockHeader(ctx, localBlock.Number-1)
			if err != nil {
				return 0, fmt.Errorf("load local block %d: %w", localBlock.Number-1, err)
			}
			localBlock = header
			continue
		}

		header, err := blocks.GetBlockHeader(ctx, remoteBlock.Number-1)
		if err != nil {
			return 0, fmt.Errorf("load remote block %d: %w", remoteBlock.Number-1, err)
		}
		remoteBlock = header
	}
	return 0, fmt.Errorf("common ancestor not found within depth %d", iterLimit)
}

func stepBack(
	ctx context.Context,
	blocks BlockReader,
	localBlock, remoteBlock blockchain.BlockHeader,
) (blockchain.BlockHeader, blockchain.BlockHeader, error) {
	if localBlock.Number == 0 || remoteBlock.Number == 0 {
		return localBlock, remoteBlock, nil
	}

	nextLocal, err := blocks.GetBlockHeader(ctx, localBlock.Number-1)
	if err != nil {
		return blockchain.BlockHeader{}, blockchain.BlockHeader{}, fmt.Errorf("load local block %d: %w", localBlock.Number-1, err)
	}
	nextRemote, err := blocks.GetBlockHeader(ctx, remoteBlock.Number-1)
	if err != nil {
		return blockchain.BlockHeader{}, blockchain.BlockHeader{}, fmt.Errorf("load remote block %d: %w", remoteBlock.Number-1, err)
	}
	return nextLocal, nextRemote, nil
}
