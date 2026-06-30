package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/brianliu-sysu/arbitrage/internal/storage"
)

// TickRepo 将 tick 数据写入 pool_states.tick_data（JSONB）。
type TickRepo struct {
	store *Store
}

// NewTickRepo 创建 TickRepo。
func NewTickRepo(store *Store) *TickRepo {
	return &TickRepo{store: store}
}

func (r *TickRepo) SaveTickData(ctx context.Context, chainName, poolAddress string, tickData map[int32]storage.TickLiquiditySnapshot) error {
	if r.store == nil {
		return nil
	}
	tickJSON, err := json.Marshal(tickData)
	if err != nil {
		return fmt.Errorf("marshal tick data: %w", err)
	}
	_, err = r.store.pool.Exec(ctx, `
		UPDATE pool_states SET tick_data = $1, updated_at = NOW()
		WHERE pool_address = $2 AND chain_name = $3`,
		string(tickJSON), poolAddress, chainName,
	)
	return err
}
