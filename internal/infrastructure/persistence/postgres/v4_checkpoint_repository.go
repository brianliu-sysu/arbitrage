package postgres

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

// V4CheckpointRepository persists Uniswap V4 sync checkpoints in PostgreSQL.
type V4CheckpointRepository struct {
	db *DB
}

func NewV4CheckpointRepository(db *DB) *V4CheckpointRepository {
	return &V4CheckpointRepository{db: db}
}

func (r *V4CheckpointRepository) Save(ctx context.Context, checkpoint *blockchain.V4Checkpoint) error {
	return r.SaveMany(ctx, []*blockchain.V4Checkpoint{checkpoint})
}

func (r *V4CheckpointRepository) SaveMany(ctx context.Context, checkpoints []*blockchain.V4Checkpoint) error {
	if len(checkpoints) == 0 {
		return nil
	}
	if len(checkpoints) == 1 {
		return r.saveOne(ctx, checkpoints[0])
	}

	poolIDs := make([][]byte, len(checkpoints))
	blockNumbers := make([]int64, len(checkpoints))
	blockHashes := make([][]byte, len(checkpoints))
	for i, checkpoint := range checkpoints {
		if checkpoint == nil {
			return fmt.Errorf("checkpoint at index %d is nil", i)
		}
		poolIDs[i] = codec.PoolIDToBytes(checkpoint.PoolID)
		blockNumbers[i] = int64(checkpoint.BlockNumber)
		blockHashes[i] = checkpoint.BlockHash.Bytes()
	}

	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO v4_checkpoints (pool_id, block_number, block_hash, updated_at)
		SELECT pool_id, block_number, block_hash, NOW()
		FROM UNNEST($1::bytea[], $2::bigint[], $3::bytea[]) AS t(pool_id, block_number, block_hash)
		ON CONFLICT (pool_id) DO UPDATE SET
			block_number = EXCLUDED.block_number,
			block_hash = EXCLUDED.block_hash,
			updated_at = NOW()
	`, poolIDs, blockNumbers, blockHashes)
	if err != nil {
		return fmt.Errorf("save v4 checkpoints: %w", err)
	}
	return nil
}

func (r *V4CheckpointRepository) saveOne(ctx context.Context, checkpoint *blockchain.V4Checkpoint) error {
	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO v4_checkpoints (pool_id, block_number, block_hash, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (pool_id) DO UPDATE SET
			block_number = EXCLUDED.block_number,
			block_hash = EXCLUDED.block_hash,
			updated_at = NOW()
	`,
		codec.PoolIDToBytes(checkpoint.PoolID),
		checkpoint.BlockNumber,
		checkpoint.BlockHash.Bytes(),
	)
	if err != nil {
		return fmt.Errorf("save v4 checkpoint: %w", err)
	}
	return nil
}

func (r *V4CheckpointRepository) Get(ctx context.Context, id marketv4.PoolID) (*blockchain.V4Checkpoint, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT block_number, block_hash
		FROM v4_checkpoints
		WHERE pool_id = $1
	`, codec.PoolIDToBytes(id))

	var blockNumber uint64
	var blockHash []byte
	if err := row.Scan(&blockNumber, &blockHash); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan v4 checkpoint: %w", err)
	}
	return &blockchain.V4Checkpoint{
		PoolID:      id,
		BlockNumber: blockNumber,
		BlockHash:   common.BytesToHash(blockHash),
	}, nil
}

func (r *V4CheckpointRepository) Delete(ctx context.Context, id marketv4.PoolID) error {
	_, err := r.db.pool.Exec(ctx, `DELETE FROM v4_checkpoints WHERE pool_id = $1`, codec.PoolIDToBytes(id))
	if err != nil {
		return fmt.Errorf("delete v4 checkpoint: %w", err)
	}
	return nil
}
