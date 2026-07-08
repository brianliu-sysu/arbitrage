package postgres

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

// QuickSwapCheckpointRepository persists QuickSwap V3 sync checkpoints in PostgreSQL.
type QuickSwapCheckpointRepository struct {
	db *DB
}

func NewQuickSwapCheckpointRepository(db *DB) *QuickSwapCheckpointRepository {
	return &QuickSwapCheckpointRepository{db: db}
}

func (r *QuickSwapCheckpointRepository) Save(ctx context.Context, checkpoint *blockchain.Checkpoint) error {
	return r.SaveMany(ctx, []*blockchain.Checkpoint{checkpoint})
}

func (r *QuickSwapCheckpointRepository) SaveMany(ctx context.Context, checkpoints []*blockchain.Checkpoint) error {
	if len(checkpoints) == 0 {
		return nil
	}
	if len(checkpoints) == 1 {
		return r.saveOne(ctx, checkpoints[0])
	}

	poolAddresses := make([][]byte, len(checkpoints))
	blockNumbers := make([]int64, len(checkpoints))
	blockHashes := make([][]byte, len(checkpoints))
	for i, checkpoint := range checkpoints {
		if checkpoint == nil {
			return fmt.Errorf("checkpoint at index %d is nil", i)
		}
		poolAddresses[i] = codec.AddressToBytes(checkpoint.PoolAddress)
		blockNumbers[i] = int64(checkpoint.BlockNumber)
		blockHashes[i] = checkpoint.BlockHash.Bytes()
	}

	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO quickswap_checkpoints (pool_address, block_number, block_hash, updated_at)
		SELECT pool_address, block_number, block_hash, NOW()
		FROM UNNEST($1::bytea[], $2::bigint[], $3::bytea[]) AS t(pool_address, block_number, block_hash)
		ON CONFLICT (pool_address) DO UPDATE SET
			block_number = EXCLUDED.block_number,
			block_hash = EXCLUDED.block_hash,
			updated_at = NOW()
	`, poolAddresses, blockNumbers, blockHashes)
	if err != nil {
		return fmt.Errorf("save quick checkpoints: %w", err)
	}
	return nil
}

func (r *QuickSwapCheckpointRepository) saveOne(ctx context.Context, checkpoint *blockchain.Checkpoint) error {
	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO quickswap_checkpoints (pool_address, block_number, block_hash, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (pool_address) DO UPDATE SET
			block_number = EXCLUDED.block_number,
			block_hash = EXCLUDED.block_hash,
			updated_at = NOW()
	`,
		codec.AddressToBytes(checkpoint.PoolAddress),
		checkpoint.BlockNumber,
		checkpoint.BlockHash.Bytes(),
	)
	if err != nil {
		return fmt.Errorf("save quick checkpoint: %w", err)
	}
	return nil
}

func (r *QuickSwapCheckpointRepository) Get(ctx context.Context, poolAddress common.Address) (*blockchain.Checkpoint, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT block_number, block_hash
		FROM quickswap_checkpoints
		WHERE pool_address = $1
	`, codec.AddressToBytes(poolAddress))

	var blockNumber uint64
	var blockHash []byte
	if err := row.Scan(&blockNumber, &blockHash); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan quick checkpoint: %w", err)
	}
	return &blockchain.Checkpoint{
		PoolAddress: poolAddress,
		BlockNumber: blockNumber,
		BlockHash:   common.BytesToHash(blockHash),
	}, nil
}

func (r *QuickSwapCheckpointRepository) Delete(ctx context.Context, poolAddress common.Address) error {
	_, err := r.db.pool.Exec(ctx, `DELETE FROM quickswap_checkpoints WHERE pool_address = $1`, codec.AddressToBytes(poolAddress))
	if err != nil {
		return fmt.Errorf("delete quick checkpoint: %w", err)
	}
	return nil
}
