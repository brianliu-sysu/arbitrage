package marketstore

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

func loadAddressPools[P any](
	ctx context.Context,
	logger *zap.Logger,
	blockNumber uint64,
	registry interface {
		List(context.Context) ([]common.Address, error)
	},
	repository interface {
		Get(context.Context, common.Address) (*P, error)
	},
	dst map[common.Address]*P,
	changed map[common.Address]struct{},
	fullRegistryCommit bool,
	label string,
) error {
	if registry == nil || repository == nil {
		return nil
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	ids, err := registry.List(ctx)
	if err != nil {
		return fmt.Errorf("list %s pools: %w", label, err)
	}
	var firstErr error
	mismatches := 0
	for _, id := range ids {
		pool, err := repository.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("load %s pool %s: %w", label, id.Hex(), err)
		}
		if pool == nil {
			return fmt.Errorf("%s pool %s not found", label, id.Hex())
		}
		lastBlock, clone, status, err := addressPoolSnapshot(pool)
		if err != nil {
			return fmt.Errorf("snapshot %s pool %s: %w", label, id.Hex(), err)
		}
		if lastBlock != blockNumber {
			mismatches++
			_, inChanged := changed[id]
			_, inPreviousSnapshot := dst[id]
			logger.Debug("committed market view pool block mismatch",
				zap.String("protocol", label),
				zap.String("pool", id.Hex()),
				zap.Uint64("pool_block", lastBlock),
				zap.Uint64("want_block", blockNumber),
				zap.Int64("lag", blockLag(lastBlock, blockNumber)),
				zap.Bool("in_changed_set", inChanged),
				zap.Bool("in_previous_snapshot", inPreviousSnapshot),
				zap.Bool("full_registry_commit", fullRegistryCommit),
				zap.String("status", string(status)),
				zap.Int("load_set_size", len(ids)),
			)
			if firstErr == nil {
				firstErr = fmt.Errorf("%s pool %s is at block %d, want %d", label, id.Hex(), lastBlock, blockNumber)
			}
			continue
		}
		dst[id] = clone.(*P)
	}
	if firstErr != nil {
		logger.Debug("committed market view protocol load failed",
			zap.String("protocol", label),
			zap.Uint64("want_block", blockNumber),
			zap.Bool("full_registry_commit", fullRegistryCommit),
			zap.Int("load_set_size", len(ids)),
			zap.Int("mismatches", mismatches),
			zap.Error(firstErr),
		)
		return firstErr
	}
	return nil
}

func addressPoolSnapshot[P any](pool *P) (uint64, any, market.PoolStatus, error) {
	switch value := any(pool).(type) {
	case *marketuniv3.Pool:
		return value.LastBlockNumber, value.Clone(), value.Status, nil
	case *marketpancake.Pool:
		return value.LastBlockNumber, value.Clone(), value.Status, nil
	case *marketquick.Pool:
		return value.LastBlockNumber, value.Clone(), value.Status, nil
	default:
		return 0, nil, "", fmt.Errorf("unsupported pool type %T", pool)
	}
}

func loadIDPools[ID comparable, P any](
	ctx context.Context,
	logger *zap.Logger,
	blockNumber uint64,
	registry interface {
		List(context.Context) ([]ID, error)
	},
	repository interface {
		Get(context.Context, ID) (*P, error)
	},
	dst map[ID]*P,
	changed map[ID]struct{},
	fullRegistryCommit bool,
	label string,
) error {
	if registry == nil || repository == nil {
		return nil
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	ids, err := registry.List(ctx)
	if err != nil {
		return fmt.Errorf("list %s pools: %w", label, err)
	}
	var firstErr error
	mismatches := 0
	for _, id := range ids {
		pool, err := repository.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("load %s pool: %w", label, err)
		}
		if pool == nil {
			return fmt.Errorf("%s pool not found", label)
		}
		lastBlock, clone, status, poolKey, err := idPoolSnapshot(pool)
		if err != nil {
			return fmt.Errorf("snapshot %s pool: %w", label, err)
		}
		if lastBlock != blockNumber {
			mismatches++
			_, inChanged := changed[id]
			_, inPreviousSnapshot := dst[id]
			logger.Debug("committed market view pool block mismatch",
				zap.String("protocol", label),
				zap.String("pool", poolKey),
				zap.Uint64("pool_block", lastBlock),
				zap.Uint64("want_block", blockNumber),
				zap.Int64("lag", blockLag(lastBlock, blockNumber)),
				zap.Bool("in_changed_set", inChanged),
				zap.Bool("in_previous_snapshot", inPreviousSnapshot),
				zap.Bool("full_registry_commit", fullRegistryCommit),
				zap.String("status", string(status)),
				zap.Int("load_set_size", len(ids)),
			)
			if firstErr == nil {
				firstErr = fmt.Errorf("%s pool %s is at block %d, want %d", label, poolKey, lastBlock, blockNumber)
			}
			continue
		}
		dst[id] = clone.(*P)
	}
	if firstErr != nil {
		logger.Debug("committed market view protocol load failed",
			zap.String("protocol", label),
			zap.Uint64("want_block", blockNumber),
			zap.Bool("full_registry_commit", fullRegistryCommit),
			zap.Int("load_set_size", len(ids)),
			zap.Int("mismatches", mismatches),
			zap.Error(firstErr),
		)
		return firstErr
	}
	return nil
}

func idPoolSnapshot[P any](pool *P) (uint64, any, market.PoolStatus, string, error) {
	switch value := any(pool).(type) {
	case *marketuniv4.Pool:
		return value.LastBlockNumber, value.Clone(), value.Status, value.ID.String(), nil
	case *marketbalancer.Pool:
		return value.LastBlockNumber, value.Clone(), value.Status, value.ID.String(), nil
	default:
		return 0, nil, "", "", fmt.Errorf("unsupported pool type %T", pool)
	}
}

func blockLag(poolBlock, wantBlock uint64) int64 {
	if wantBlock >= poolBlock {
		return int64(wantBlock - poolBlock)
	}
	return -int64(poolBlock - wantBlock)
}

func poolIDSet[ID comparable](ids []ID) map[ID]struct{} {
	out := make(map[ID]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}
