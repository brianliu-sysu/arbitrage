package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SyncRepo 实现链级同步进度持久化。
type SyncRepo struct {
	store *Store
}

// NewSyncRepo 创建 SyncRepo。
func NewSyncRepo(store *Store) *SyncRepo {
	return &SyncRepo{store: store}
}

func (r *SyncRepo) GetLastProcessedBlock(ctx context.Context, chainName string) (uint64, error) {
	if r.store == nil {
		return 0, nil
	}
	var block uint64
	err := r.store.pool.QueryRow(ctx, `
		SELECT last_processed_block FROM chain_sync_state WHERE chain_name = $1`,
		chainName,
	).Scan(&block)
	if err != nil {
		if isNoRows(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("get last processed block: %w", err)
	}
	return block, nil
}

func (r *SyncRepo) SetLastProcessedBlock(ctx context.Context, chainName string, block uint64) error {
	if r.store == nil {
		return nil
	}
	_, err := r.store.pool.Exec(ctx, `
		INSERT INTO chain_sync_state (chain_name, last_processed_block, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (chain_name) DO UPDATE SET
			last_processed_block = EXCLUDED.last_processed_block,
			updated_at = EXCLUDED.updated_at`,
		chainName, block,
	)
	if err != nil {
		return fmt.Errorf("set last processed block: %w", err)
	}
	return nil
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
