package postgres

import (
	"context"
	"fmt"
	"time"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/jackc/pgx/v5"
)

// BalancerSnapshotRepository persists Balancer pool snapshots in PostgreSQL.
type BalancerSnapshotRepository struct {
	db *DB
}

func NewBalancerSnapshotRepository(db *DB) *BalancerSnapshotRepository {
	return &BalancerSnapshotRepository{db: db}
}

func (r *BalancerSnapshotRepository) Save(ctx context.Context, snapshot *marketbalancer.Snapshot) error {
	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO balancer_snapshots (
			pool_id, block_number, tokens, balances, weights,
			amplification, swap_fee_percentage, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (pool_id, block_number) DO UPDATE SET
			tokens = EXCLUDED.tokens,
			balances = EXCLUDED.balances,
			weights = EXCLUDED.weights,
			amplification = EXCLUDED.amplification,
			swap_fee_percentage = EXCLUDED.swap_fee_percentage,
			created_at = EXCLUDED.created_at
	`,
		snapshot.PoolID.Hash().Bytes(),
		snapshot.BlockNumber,
		mustJSON(balancerTokensToStrings(snapshot.Tokens)),
		mustJSON(balancerIntMapToStrings(snapshot.Balances)),
		mustJSON(balancerIntMapToStrings(snapshot.Weights)),
		snapshot.Amplification.String(),
		snapshot.SwapFeePercentage.String(),
		snapshot.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert balancer snapshot: %w", err)
	}
	return nil
}

func (r *BalancerSnapshotRepository) GetLatest(ctx context.Context, id marketbalancer.PoolID) (*marketbalancer.Snapshot, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT block_number, tokens, balances, weights, amplification, swap_fee_percentage, created_at
		FROM balancer_snapshots
		WHERE pool_id = $1
		ORDER BY block_number DESC
		LIMIT 1
	`, id.Hash().Bytes())
	return scanBalancerSnapshotRow(id, row)
}

func (r *BalancerSnapshotRepository) GetAtBlock(ctx context.Context, id marketbalancer.PoolID, blockNumber uint64) (*marketbalancer.Snapshot, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT block_number, tokens, balances, weights, amplification, swap_fee_percentage, created_at
		FROM balancer_snapshots
		WHERE pool_id = $1 AND block_number = $2
	`, id.Hash().Bytes(), blockNumber)
	return scanBalancerSnapshotRow(id, row)
}

func (r *BalancerSnapshotRepository) DeleteAfterBlock(ctx context.Context, id marketbalancer.PoolID, blockNumber uint64) error {
	_, err := r.db.pool.Exec(ctx, `
		DELETE FROM balancer_snapshots
		WHERE pool_id = $1 AND block_number > $2
	`, id.Hash().Bytes(), blockNumber)
	if err != nil {
		return fmt.Errorf("delete balancer snapshots after block: %w", err)
	}
	return nil
}

func scanBalancerSnapshotRow(poolID marketbalancer.PoolID, row pgx.Row) (*marketbalancer.Snapshot, error) {
	var (
		blockNumber uint64
		tokensRaw   []byte
		balancesRaw []byte
		weightsRaw  []byte
		ampRaw      string
		swapFeeRaw  string
		createdAt   time.Time
	)
	if err := row.Scan(&blockNumber, &tokensRaw, &balancesRaw, &weightsRaw, &ampRaw, &swapFeeRaw, &createdAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan balancer snapshot: %w", err)
	}
	tokens, err := balancerTokensFromJSON(tokensRaw)
	if err != nil {
		return nil, err
	}
	balances, err := balancerIntMapFromJSON(balancesRaw)
	if err != nil {
		return nil, err
	}
	weights, err := balancerIntMapFromJSON(weightsRaw)
	if err != nil {
		return nil, err
	}
	return &marketbalancer.Snapshot{
		PoolID:            poolID,
		BlockNumber:       blockNumber,
		Tokens:            tokens,
		Balances:          balances,
		Weights:           weights,
		Amplification:     parseNumericString(ampRaw),
		SwapFeePercentage: parseNumericString(swapFeeRaw),
		CreatedAt:         createdAt,
	}, nil
}
