package postgres

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

// CheckpointRepository persists sync checkpoints in PostgreSQL.
type CheckpointRepository struct {
	db *DB
}

func NewCheckpointRepository(db *DB) *CheckpointRepository {
	return &CheckpointRepository{db: db}
}

func (r *CheckpointRepository) Save(ctx context.Context, checkpoint *blockchain.Checkpoint) error {
	return r.SaveMany(ctx, []*blockchain.Checkpoint{checkpoint})
}

func (r *CheckpointRepository) SaveMany(ctx context.Context, checkpoints []*blockchain.Checkpoint) error {
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
		INSERT INTO checkpoints (pool_address, block_number, block_hash, updated_at)
		SELECT pool_address, block_number, block_hash, NOW()
		FROM UNNEST($1::bytea[], $2::bigint[], $3::bytea[]) AS t(pool_address, block_number, block_hash)
		ON CONFLICT (pool_address) DO UPDATE SET
			block_number = EXCLUDED.block_number,
			block_hash = EXCLUDED.block_hash,
			updated_at = NOW()
	`, poolAddresses, blockNumbers, blockHashes)
	if err != nil {
		return fmt.Errorf("save checkpoints: %w", err)
	}
	return nil
}

func (r *CheckpointRepository) saveOne(ctx context.Context, checkpoint *blockchain.Checkpoint) error {
	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO checkpoints (pool_address, block_number, block_hash, updated_at)
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
		return fmt.Errorf("save checkpoint: %w", err)
	}
	return nil
}

func (r *CheckpointRepository) Get(ctx context.Context, poolAddress common.Address) (*blockchain.Checkpoint, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT block_number, block_hash
		FROM checkpoints
		WHERE pool_address = $1
	`, codec.AddressToBytes(poolAddress))

	var blockNumber uint64
	var blockHash []byte
	if err := row.Scan(&blockNumber, &blockHash); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan checkpoint: %w", err)
	}
	return &blockchain.Checkpoint{
		PoolAddress: poolAddress,
		BlockNumber: blockNumber,
		BlockHash:   common.BytesToHash(blockHash),
	}, nil
}

func (r *CheckpointRepository) Delete(ctx context.Context, poolAddress common.Address) error {
	_, err := r.db.pool.Exec(ctx, `DELETE FROM checkpoints WHERE pool_address = $1`, codec.AddressToBytes(poolAddress))
	if err != nil {
		return fmt.Errorf("delete checkpoint: %w", err)
	}
	return nil
}
