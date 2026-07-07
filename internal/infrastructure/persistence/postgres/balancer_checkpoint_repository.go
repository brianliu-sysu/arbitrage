package postgres

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

// BalancerCheckpointRepository persists Balancer sync checkpoints in PostgreSQL.
type BalancerCheckpointRepository struct {
	db *DB
}

func NewBalancerCheckpointRepository(db *DB) *BalancerCheckpointRepository {
	return &BalancerCheckpointRepository{db: db}
}

func (r *BalancerCheckpointRepository) Save(ctx context.Context, checkpoint *blockchain.BalancerCheckpoint) error {
	return r.SaveMany(ctx, []*blockchain.BalancerCheckpoint{checkpoint})
}

func (r *BalancerCheckpointRepository) SaveMany(ctx context.Context, checkpoints []*blockchain.BalancerCheckpoint) error {
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
		poolIDs[i] = checkpoint.PoolID.Hash().Bytes()
		blockNumbers[i] = int64(checkpoint.BlockNumber)
		blockHashes[i] = checkpoint.BlockHash.Bytes()
	}

	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO balancer_checkpoints (pool_id, block_number, block_hash, updated_at)
		SELECT pool_id, block_number, block_hash, NOW()
		FROM UNNEST($1::bytea[], $2::bigint[], $3::bytea[]) AS t(pool_id, block_number, block_hash)
		ON CONFLICT (pool_id) DO UPDATE SET
			block_number = EXCLUDED.block_number,
			block_hash = EXCLUDED.block_hash,
			updated_at = NOW()
	`, poolIDs, blockNumbers, blockHashes)
	if err != nil {
		return fmt.Errorf("save balancer checkpoints: %w", err)
	}
	return nil
}

func (r *BalancerCheckpointRepository) saveOne(ctx context.Context, checkpoint *blockchain.BalancerCheckpoint) error {
	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO balancer_checkpoints (pool_id, block_number, block_hash, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (pool_id) DO UPDATE SET
			block_number = EXCLUDED.block_number,
			block_hash = EXCLUDED.block_hash,
			updated_at = NOW()
	`,
		checkpoint.PoolID.Hash().Bytes(),
		checkpoint.BlockNumber,
		checkpoint.BlockHash.Bytes(),
	)
	if err != nil {
		return fmt.Errorf("save balancer checkpoint: %w", err)
	}
	return nil
}

func (r *BalancerCheckpointRepository) Get(ctx context.Context, id marketbalancer.PoolID) (*blockchain.BalancerCheckpoint, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT block_number, block_hash
		FROM balancer_checkpoints
		WHERE pool_id = $1
	`, id.Hash().Bytes())

	var blockNumber uint64
	var blockHash []byte
	if err := row.Scan(&blockNumber, &blockHash); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan balancer checkpoint: %w", err)
	}
	return &blockchain.BalancerCheckpoint{
		PoolID:      id,
		BlockNumber: blockNumber,
		BlockHash:   common.BytesToHash(blockHash),
	}, nil
}

func (r *BalancerCheckpointRepository) Delete(ctx context.Context, id marketbalancer.PoolID) error {
	_, err := r.db.pool.Exec(ctx, `DELETE FROM balancer_checkpoints WHERE pool_id = $1`, id.Hash().Bytes())
	if err != nil {
		return fmt.Errorf("delete balancer checkpoint: %w", err)
	}
	return nil
}
