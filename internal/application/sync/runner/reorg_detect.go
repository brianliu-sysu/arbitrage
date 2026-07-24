package runner

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
)

type LocalBlockHistory interface {
	Header(uint64) (blockchain.BlockHeader, bool)
}

// DetectReorg checks whether remoteHead diverges from localHead and returns a reorg event.
func DetectReorg(
	ctx context.Context,
	blocks BlockBatchReader,
	history LocalBlockHistory,
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
	if remoteHead.Number > localHead.Number+1 {
		return nil, nil
	}
	if blocks == nil {
		return nil, fmt.Errorf("block batch reader is not configured")
	}
	if history == nil {
		return nil, fmt.Errorf("local block history is not configured")
	}

	ancestor, err := FindCommonAncestor(ctx, history, blocks, maxDepth, localHead, remoteHead)
	if err != nil {
		return nil, err
	}
	if ancestor >= localHead.Number {
		return nil, nil
	}
	reorg := blockchain.NewReorg(remoteHead.Number, localHead, remoteHead, ancestor)
	return &reorg, nil
}

// FindCommonAncestor compares committed local history with the remote canonical chain.
func FindCommonAncestor(
	ctx context.Context,
	local LocalBlockHistory,
	remote BlockBatchReader,
	maxDepth uint64,
	localHead, remoteHead blockchain.BlockHeader,
) (uint64, error) {
	searchTop := min(localHead.Number, remoteHead.Number)
	searchBottom := uint64(0)
	if searchTop > maxDepth {
		searchBottom = searchTop - maxDepth
	}
	blockNumbers := blockRange(searchBottom, remoteHead.Number)
	remoteHeaders, err := remote.GetBlockHeaders(ctx, blockNumbers)
	if err != nil {
		return 0, fmt.Errorf("load remote canonical headers: %w", err)
	}
	if err := validateRemoteChain(remoteHead, remoteHeaders, searchBottom); err != nil {
		return 0, err
	}
	if err := validateLocalChain(localHead, local, searchBottom); err != nil {
		return 0, err
	}
	for blockNumber := searchTop; ; blockNumber-- {
		localHeader, ok := local.Header(blockNumber)
		if !ok {
			return 0, fmt.Errorf("local block %d is not cached", blockNumber)
		}
		remoteHeader, ok := remoteHeaders[blockNumber]
		if !ok {
			return 0, fmt.Errorf("remote block %d is missing from batch", blockNumber)
		}
		if localHeader.Hash == remoteHeader.Hash {
			return blockNumber, nil
		}
		if blockNumber == searchBottom {
			break
		}
	}
	return 0, fmt.Errorf("common ancestor not found within depth %d", maxDepth)
}

func validateRemoteChain(
	head blockchain.BlockHeader,
	headers map[uint64]blockchain.BlockHeader,
	bottom uint64,
) error {
	canonicalHead, ok := headers[head.Number]
	if !ok {
		return fmt.Errorf("remote block %d is missing from batch", head.Number)
	}
	if canonicalHead.Hash != head.Hash {
		return fmt.Errorf("remote canonical head %d changed during reorg detection", head.Number)
	}
	current := head
	for current.Number > bottom {
		parentNumber := current.Number - 1
		parent, ok := headers[parentNumber]
		if !ok {
			return fmt.Errorf("remote block %d is missing from batch", parentNumber)
		}
		if current.ParentHash != parent.Hash {
			return fmt.Errorf("remote chain is discontinuous at block %d", current.Number)
		}
		current = parent
	}
	return nil
}

func validateLocalChain(
	head blockchain.BlockHeader,
	history LocalBlockHistory,
	bottom uint64,
) error {
	cachedHead, ok := history.Header(head.Number)
	if !ok {
		return fmt.Errorf("local block %d is not cached", head.Number)
	}
	if cachedHead.Hash != head.Hash {
		return fmt.Errorf("local cached head %d does not match committed head", head.Number)
	}
	current := head
	for current.Number > bottom {
		parentNumber := current.Number - 1
		parent, ok := history.Header(parentNumber)
		if !ok {
			return fmt.Errorf("local block %d is not cached", parentNumber)
		}
		if current.ParentHash != parent.Hash {
			return fmt.Errorf("local chain is discontinuous at block %d", current.Number)
		}
		current = parent
	}
	return nil
}
