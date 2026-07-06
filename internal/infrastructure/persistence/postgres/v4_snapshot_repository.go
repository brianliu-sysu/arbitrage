package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/codec"
	"github.com/jackc/pgx/v5"
)

// V4SnapshotRepository persists Uniswap V4 pool snapshots in PostgreSQL.
type V4SnapshotRepository struct {
	db *DB
}

func NewV4SnapshotRepository(db *DB) *V4SnapshotRepository {
	return &V4SnapshotRepository{db: db}
}

func (r *V4SnapshotRepository) Save(ctx context.Context, snapshot *marketv4.Snapshot) error {
	tx, err := r.db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := upsertV4Snapshot(ctx, tx, snapshot); err != nil {
		return err
	}
	if err := replaceV4SnapshotTicks(ctx, tx, snapshot); err != nil {
		return err
	}
	if err := replaceV4SnapshotBitmap(ctx, tx, snapshot); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *V4SnapshotRepository) GetLatest(ctx context.Context, id marketv4.PoolID) (*marketv4.Snapshot, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT block_number, sqrt_price_x96::text, tick, liquidity::text,
		       fee_growth_global0_x128::text, fee_growth_global1_x128::text, created_at
		FROM v4_snapshots
		WHERE pool_id = $1
		ORDER BY block_number DESC
		LIMIT 1
	`, codec.PoolIDToBytes(id))
	return scanV4SnapshotRow(ctx, r.db.pool, id, row)
}

func (r *V4SnapshotRepository) GetAtBlock(ctx context.Context, id marketv4.PoolID, blockNumber uint64) (*marketv4.Snapshot, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT block_number, sqrt_price_x96::text, tick, liquidity::text,
		       fee_growth_global0_x128::text, fee_growth_global1_x128::text, created_at
		FROM v4_snapshots
		WHERE pool_id = $1 AND block_number = $2
	`, codec.PoolIDToBytes(id), blockNumber)
	return scanV4SnapshotRow(ctx, r.db.pool, id, row)
}

func (r *V4SnapshotRepository) DeleteAfterBlock(ctx context.Context, id marketv4.PoolID, blockNumber uint64) error {
	_, err := r.db.pool.Exec(ctx, `
		DELETE FROM v4_snapshots
		WHERE pool_id = $1 AND block_number > $2
	`, codec.PoolIDToBytes(id), blockNumber)
	if err != nil {
		return fmt.Errorf("delete v4 snapshots after block: %w", err)
	}
	return nil
}

func upsertV4Snapshot(ctx context.Context, tx pgx.Tx, snapshot *marketv4.Snapshot) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO v4_snapshots (
			pool_id, block_number, sqrt_price_x96, tick, liquidity,
			fee_growth_global0_x128, fee_growth_global1_x128, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (pool_id, block_number) DO UPDATE SET
			sqrt_price_x96 = EXCLUDED.sqrt_price_x96,
			tick = EXCLUDED.tick,
			liquidity = EXCLUDED.liquidity,
			fee_growth_global0_x128 = EXCLUDED.fee_growth_global0_x128,
			fee_growth_global1_x128 = EXCLUDED.fee_growth_global1_x128,
			created_at = EXCLUDED.created_at
	`,
		codec.PoolIDToBytes(snapshot.PoolID),
		snapshot.BlockNumber,
		snapshot.State.SqrtPriceX96.String(),
		snapshot.State.Tick,
		snapshot.State.Liquidity.String(),
		snapshot.State.FeeGrowthGlobal0X128.String(),
		snapshot.State.FeeGrowthGlobal1X128.String(),
		snapshot.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert v4 snapshot: %w", err)
	}
	return nil
}

func replaceV4SnapshotTicks(ctx context.Context, tx pgx.Tx, snapshot *marketv4.Snapshot) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM v4_snapshot_ticks
		WHERE pool_id = $1 AND block_number = $2
	`, codec.PoolIDToBytes(snapshot.PoolID), snapshot.BlockNumber); err != nil {
		return fmt.Errorf("delete v4 snapshot ticks: %w", err)
	}
	for _, tick := range snapshot.Ticks.ExportTicks() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO v4_snapshot_ticks (pool_id, block_number, tick_index, liquidity_gross, liquidity_net)
			VALUES ($1, $2, $3, $4, $5)
		`,
			codec.PoolIDToBytes(snapshot.PoolID),
			snapshot.BlockNumber,
			tick.Index,
			tick.LiquidityGross.String(),
			tick.LiquidityNet.String(),
		); err != nil {
			return fmt.Errorf("insert v4 snapshot tick: %w", err)
		}
	}
	return nil
}

func replaceV4SnapshotBitmap(ctx context.Context, tx pgx.Tx, snapshot *marketv4.Snapshot) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM v4_snapshot_tick_bitmap
		WHERE pool_id = $1 AND block_number = $2
	`, codec.PoolIDToBytes(snapshot.PoolID), snapshot.BlockNumber); err != nil {
		return fmt.Errorf("delete v4 snapshot bitmap: %w", err)
	}
	for _, word := range snapshot.Bitmap.ExportBitmap() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO v4_snapshot_tick_bitmap (pool_id, block_number, word_pos, word_value)
			VALUES ($1, $2, $3, $4)
		`,
			codec.PoolIDToBytes(snapshot.PoolID),
			snapshot.BlockNumber,
			word.WordPos,
			word.Word.String(),
		); err != nil {
			return fmt.Errorf("insert v4 snapshot bitmap word: %w", err)
		}
	}
	return nil
}

func scanV4SnapshotRow(ctx context.Context, pool pgxQueryPool, poolID marketv4.PoolID, row snapshotRowScanner) (*marketv4.Snapshot, error) {
	var (
		blockNumber            uint64
		sqrtPrice, liquidity   string
		tick                   int32
		feeGrowth0, feeGrowth1 string
		createdAt              time.Time
	)
	if err := row.Scan(&blockNumber, &sqrtPrice, &tick, &liquidity, &feeGrowth0, &feeGrowth1, &createdAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan v4 snapshot: %w", err)
	}

	ticks, err := loadV4SnapshotTicks(ctx, pool, poolID, blockNumber)
	if err != nil {
		return nil, err
	}
	bitmap, err := loadV4SnapshotBitmap(ctx, pool, poolID, blockNumber)
	if err != nil {
		return nil, err
	}

	return &marketv4.Snapshot{
		PoolID:      poolID,
		BlockNumber: blockNumber,
		State: codec.PoolStateFromRow(
			sql.NullString{String: sqrtPrice, Valid: true},
			tick,
			sql.NullString{String: liquidity, Valid: true},
			sql.NullString{String: feeGrowth0, Valid: true},
			sql.NullString{String: feeGrowth1, Valid: true},
		),
		Ticks:     ticks,
		Bitmap:    bitmap,
		CreatedAt: createdAt,
	}, nil
}

func loadV4SnapshotTicks(ctx context.Context, pool pgxQueryPool, poolID marketv4.PoolID, blockNumber uint64) (market.TickTable, error) {
	rows, err := pool.Query(ctx, `
		SELECT tick_index, liquidity_gross::text, liquidity_net::text
		FROM v4_snapshot_ticks
		WHERE pool_id = $1 AND block_number = $2
	`, codec.PoolIDToBytes(poolID), blockNumber)
	if err != nil {
		return market.TickTable{}, fmt.Errorf("query v4 snapshot ticks: %w", err)
	}
	defer rows.Close()

	records := make([]market.PersistedTick, 0)
	for rows.Next() {
		var index int32
		var gross, net string
		if err := rows.Scan(&index, &gross, &net); err != nil {
			return market.TickTable{}, fmt.Errorf("scan v4 snapshot tick: %w", err)
		}
		records = append(records, market.PersistedTick{
			Index:          index,
			LiquidityGross: parseNumericString(gross),
			LiquidityNet:   parseNumericString(net),
		})
	}
	return market.ImportTickTable(records), rows.Err()
}

func loadV4SnapshotBitmap(ctx context.Context, pool pgxQueryPool, poolID marketv4.PoolID, blockNumber uint64) (market.TickBitmap, error) {
	rows, err := pool.Query(ctx, `
		SELECT word_pos, word_value::text
		FROM v4_snapshot_tick_bitmap
		WHERE pool_id = $1 AND block_number = $2
	`, codec.PoolIDToBytes(poolID), blockNumber)
	if err != nil {
		return market.TickBitmap{}, fmt.Errorf("query v4 snapshot bitmap: %w", err)
	}
	defer rows.Close()

	records := make([]market.PersistedBitmapWord, 0)
	for rows.Next() {
		var wordPos int16
		var wordValue string
		if err := rows.Scan(&wordPos, &wordValue); err != nil {
			return market.TickBitmap{}, fmt.Errorf("scan v4 snapshot bitmap: %w", err)
		}
		records = append(records, market.PersistedBitmapWord{
			WordPos: wordPos,
			Word:    parseNumericString(wordValue),
		})
	}
	return market.ImportTickBitmap(records), rows.Err()
}
