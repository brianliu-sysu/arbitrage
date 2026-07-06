package postgres

import (
	"context"
	"database/sql"
	"fmt"

	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/codec"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// V4PoolRepository persists Uniswap V4 pool aggregate state in PostgreSQL.
type V4PoolRepository struct {
	db *DB
}

func NewV4PoolRepository(db *DB) *V4PoolRepository {
	return &V4PoolRepository{db: db}
}

func (r *V4PoolRepository) Save(ctx context.Context, pool *marketv4.Pool) error {
	tx, err := r.db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := upsertV4Pool(ctx, tx, pool); err != nil {
		return err
	}
	if err := replaceV4PoolTicks(ctx, tx, pool); err != nil {
		return err
	}
	if err := replaceV4PoolBitmap(ctx, tx, pool); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *V4PoolRepository) Get(ctx context.Context, id marketv4.PoolID) (*marketv4.Pool, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT currency0, currency1, fee, tick_spacing, hooks, status, last_block_number,
		       sqrt_price_x96::text, tick, liquidity::text,
		       fee_growth_global0_x128::text, fee_growth_global1_x128::text
		FROM v4_pools
		WHERE pool_id = $1
	`, codec.PoolIDToBytes(id))

	var (
		currency0, currency1, hooks []byte
		fee                         uint32
		tickSpacing                 int32
		status                      string
		lastBlockNumber             uint64
		sqrtPrice, liquidity        string
		tick                        int32
		feeGrowth0, feeGrowth1      string
	)
	if err := row.Scan(
		&currency0, &currency1, &fee, &tickSpacing, &hooks, &status, &lastBlockNumber,
		&sqrtPrice, &tick, &liquidity, &feeGrowth0, &feeGrowth1,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan v4 pool: %w", err)
	}

	ticks, err := loadV4PoolTicks(ctx, r.db.pool, id)
	if err != nil {
		return nil, err
	}
	bitmap, err := loadV4PoolBitmap(ctx, r.db.pool, id)
	if err != nil {
		return nil, err
	}

	pool := marketv4.NewPool(id, marketv4.PoolKey{
		Currency0:   codec.BytesToAddress(currency0),
		Currency1:   codec.BytesToAddress(currency1),
		Fee:         fee,
		TickSpacing: tickSpacing,
		Hooks:       codec.BytesToAddress(hooks),
	})
	pool.Status = market.PoolStatus(status)
	pool.LastBlockNumber = lastBlockNumber
	pool.State = codec.PoolStateFromRow(
		sql.NullString{String: sqrtPrice, Valid: true},
		tick,
		sql.NullString{String: liquidity, Valid: true},
		sql.NullString{String: feeGrowth0, Valid: true},
		sql.NullString{String: feeGrowth1, Valid: true},
	)
	pool.Ticks = ticks
	pool.Bitmap = bitmap
	return pool, nil
}

func (r *V4PoolRepository) Delete(ctx context.Context, id marketv4.PoolID) error {
	_, err := r.db.pool.Exec(ctx, `DELETE FROM v4_pools WHERE pool_id = $1`, codec.PoolIDToBytes(id))
	if err != nil {
		return fmt.Errorf("delete v4 pool: %w", err)
	}
	return nil
}

func (r *V4PoolRepository) AdvanceSyncProgress(ctx context.Context, id marketv4.PoolID, blockNumber uint64) error {
	return r.AdvanceSyncProgressMany(ctx, []marketv4.PoolID{id}, blockNumber)
}

func (r *V4PoolRepository) AdvanceSyncProgressMany(ctx context.Context, ids []marketv4.PoolID, blockNumber uint64) error {
	if len(ids) == 0 {
		return nil
	}
	if len(ids) == 1 {
		return r.advanceV4SyncProgressOne(ctx, ids[0], blockNumber)
	}

	poolIDs := make([][]byte, len(ids))
	for i, id := range ids {
		poolIDs[i] = codec.PoolIDToBytes(id)
	}

	tag, err := r.db.pool.Exec(ctx, `
		UPDATE v4_pools SET
			last_block_number = GREATEST(last_block_number, $2),
			status = CASE WHEN status = $3 THEN $4 ELSE status END,
			updated_at = NOW()
		WHERE pool_id = ANY($1)
	`,
		poolIDs,
		blockNumber,
		string(market.PoolStatusCatchingUp),
		string(market.PoolStatusSyncing),
	)
	if err != nil {
		return fmt.Errorf("advance v4 sync progress: %w", err)
	}
	if tag.RowsAffected() != int64(len(ids)) {
		return fmt.Errorf("expected to update %d v4 pools, updated %d", len(ids), tag.RowsAffected())
	}
	return nil
}

func (r *V4PoolRepository) advanceV4SyncProgressOne(ctx context.Context, id marketv4.PoolID, blockNumber uint64) error {
	tag, err := r.db.pool.Exec(ctx, `
		UPDATE v4_pools SET
			last_block_number = GREATEST(last_block_number, $2),
			status = CASE WHEN status = $3 THEN $4 ELSE status END,
			updated_at = NOW()
		WHERE pool_id = $1
	`,
		codec.PoolIDToBytes(id),
		blockNumber,
		string(market.PoolStatusCatchingUp),
		string(market.PoolStatusSyncing),
	)
	if err != nil {
		return fmt.Errorf("advance v4 sync progress: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("v4 pool %s not found", id)
	}
	return nil
}

func upsertV4Pool(ctx context.Context, tx pgx.Tx, pool *marketv4.Pool) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO v4_pools (
			pool_id, currency0, currency1, fee, tick_spacing, hooks, status, last_block_number,
			sqrt_price_x96, tick, liquidity, fee_growth_global0_x128, fee_growth_global1_x128, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13, NOW()
		)
		ON CONFLICT (pool_id) DO UPDATE SET
			currency0 = EXCLUDED.currency0,
			currency1 = EXCLUDED.currency1,
			fee = EXCLUDED.fee,
			tick_spacing = EXCLUDED.tick_spacing,
			hooks = EXCLUDED.hooks,
			status = EXCLUDED.status,
			last_block_number = EXCLUDED.last_block_number,
			sqrt_price_x96 = EXCLUDED.sqrt_price_x96,
			tick = EXCLUDED.tick,
			liquidity = EXCLUDED.liquidity,
			fee_growth_global0_x128 = EXCLUDED.fee_growth_global0_x128,
			fee_growth_global1_x128 = EXCLUDED.fee_growth_global1_x128,
			updated_at = NOW()
	`,
		codec.PoolIDToBytes(pool.ID),
		codec.AddressToBytes(pool.Key.Currency0),
		codec.AddressToBytes(pool.Key.Currency1),
		pool.Key.Fee,
		pool.Key.TickSpacing,
		codec.AddressToBytes(pool.Key.Hooks),
		string(pool.Status),
		pool.LastBlockNumber,
		pool.State.SqrtPriceX96.String(),
		pool.State.Tick,
		pool.State.Liquidity.String(),
		pool.State.FeeGrowthGlobal0X128.String(),
		pool.State.FeeGrowthGlobal1X128.String(),
	)
	if err != nil {
		return fmt.Errorf("upsert v4 pool: %w", err)
	}
	return nil
}

func replaceV4PoolTicks(ctx context.Context, tx pgx.Tx, pool *marketv4.Pool) error {
	if _, err := tx.Exec(ctx, `DELETE FROM v4_pool_ticks WHERE pool_id = $1`, codec.PoolIDToBytes(pool.ID)); err != nil {
		return fmt.Errorf("delete v4 pool ticks: %w", err)
	}
	for _, tick := range pool.Ticks.ExportTicks() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO v4_pool_ticks (pool_id, tick_index, liquidity_gross, liquidity_net)
			VALUES ($1, $2, $3, $4)
		`,
			codec.PoolIDToBytes(pool.ID),
			tick.Index,
			tick.LiquidityGross.String(),
			tick.LiquidityNet.String(),
		); err != nil {
			return fmt.Errorf("insert v4 pool tick: %w", err)
		}
	}
	return nil
}

func replaceV4PoolBitmap(ctx context.Context, tx pgx.Tx, pool *marketv4.Pool) error {
	if _, err := tx.Exec(ctx, `DELETE FROM v4_pool_tick_bitmap WHERE pool_id = $1`, codec.PoolIDToBytes(pool.ID)); err != nil {
		return fmt.Errorf("delete v4 pool bitmap: %w", err)
	}
	for _, word := range pool.Bitmap.ExportBitmap() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO v4_pool_tick_bitmap (pool_id, word_pos, word_value)
			VALUES ($1, $2, $3)
		`,
			codec.PoolIDToBytes(pool.ID),
			word.WordPos,
			word.Word.String(),
		); err != nil {
			return fmt.Errorf("insert v4 pool bitmap word: %w", err)
		}
	}
	return nil
}

func loadV4PoolTicks(ctx context.Context, pool *pgxpool.Pool, id marketv4.PoolID) (market.TickTable, error) {
	rows, err := pool.Query(ctx, `
		SELECT tick_index, liquidity_gross::text, liquidity_net::text
		FROM v4_pool_ticks
		WHERE pool_id = $1
	`, codec.PoolIDToBytes(id))
	if err != nil {
		return market.TickTable{}, fmt.Errorf("query v4 pool ticks: %w", err)
	}
	defer rows.Close()

	records := make([]market.PersistedTick, 0)
	for rows.Next() {
		var index int32
		var gross, net string
		if err := rows.Scan(&index, &gross, &net); err != nil {
			return market.TickTable{}, fmt.Errorf("scan v4 pool tick: %w", err)
		}
		records = append(records, market.PersistedTick{
			Index:          index,
			LiquidityGross: parseNumericString(gross),
			LiquidityNet:   parseNumericString(net),
		})
	}
	return market.ImportTickTable(records), rows.Err()
}

func loadV4PoolBitmap(ctx context.Context, pool *pgxpool.Pool, id marketv4.PoolID) (market.TickBitmap, error) {
	rows, err := pool.Query(ctx, `
		SELECT word_pos, word_value::text
		FROM v4_pool_tick_bitmap
		WHERE pool_id = $1
	`, codec.PoolIDToBytes(id))
	if err != nil {
		return market.TickBitmap{}, fmt.Errorf("query v4 pool bitmap: %w", err)
	}
	defer rows.Close()

	records := make([]market.PersistedBitmapWord, 0)
	for rows.Next() {
		var wordPos int16
		var wordValue string
		if err := rows.Scan(&wordPos, &wordValue); err != nil {
			return market.TickBitmap{}, fmt.Errorf("scan v4 pool bitmap: %w", err)
		}
		records = append(records, market.PersistedBitmapWord{
			WordPos: wordPos,
			Word:    parseNumericString(wordValue),
		})
	}
	return market.ImportTickBitmap(records), rows.Err()
}
