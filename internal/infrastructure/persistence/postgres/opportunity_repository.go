package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/codec"
	"github.com/jackc/pgx/v5"
)

// OpportunityRepository persists arbitrage opportunities in PostgreSQL.
type OpportunityRepository struct {
	db *DB
}

func NewOpportunityRepository(db *DB) *OpportunityRepository {
	return &OpportunityRepository{db: db}
}

func (r *OpportunityRepository) Save(ctx context.Context, opportunity *arbitrage.Opportunity) error {
	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO opportunities (id, pool_address, block_number, payload, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			pool_address = EXCLUDED.pool_address,
			block_number = EXCLUDED.block_number,
			payload = EXCLUDED.payload,
			created_at = EXCLUDED.created_at
	`,
		opportunity.ID,
		codec.AddressToBytes(opportunity.PoolAddress),
		opportunity.BlockNumber,
		opportunity.Payload,
		opportunity.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("save opportunity: %w", err)
	}
	return nil
}

func (r *OpportunityRepository) Get(ctx context.Context, id string) (*arbitrage.Opportunity, error) {
	var (
		item        arbitrage.Opportunity
		poolAddress []byte
		payload     []byte
	)
	err := r.db.pool.QueryRow(ctx, `
		SELECT id, pool_address, block_number, payload, created_at
		FROM opportunities
		WHERE id = $1
	`, id).Scan(&item.ID, &poolAddress, &item.BlockNumber, &payload, &item.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, arbitrage.ErrOpportunityNotFound
		}
		return nil, fmt.Errorf("get opportunity: %w", err)
	}
	item.PoolAddress = codec.BytesToAddress(poolAddress)
	item.Payload = append([]byte(nil), payload...)
	if err := item.ApplyPayload(); err != nil {
		return nil, fmt.Errorf("apply opportunity payload for %s: %w", item.ID, err)
	}
	return &item, nil
}

func (r *OpportunityRepository) List(ctx context.Context, limit int) ([]*arbitrage.Opportunity, error) {
	query := `
		SELECT id, pool_address, block_number, payload, created_at
		FROM opportunities
		ORDER BY created_at DESC
	`
	args := []any{}
	if limit > 0 {
		query += ` LIMIT $1`
		args = append(args, limit)
	}

	rows, err := r.db.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list opportunities: %w", err)
	}
	defer rows.Close()

	items := make([]*arbitrage.Opportunity, 0)
	for rows.Next() {
		var (
			item        arbitrage.Opportunity
			poolAddress []byte
			payload     []byte
		)
		if err := rows.Scan(&item.ID, &poolAddress, &item.BlockNumber, &payload, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan opportunity: %w", err)
		}
		item.PoolAddress = codec.BytesToAddress(poolAddress)
		item.Payload = append([]byte(nil), payload...)
		if err := item.ApplyPayload(); err != nil {
			return nil, fmt.Errorf("apply opportunity payload for %s: %w", item.ID, err)
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (r *OpportunityRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.db.pool.Exec(ctx, `DELETE FROM opportunities WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete opportunity: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
