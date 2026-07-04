package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v3"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

// SnapshotRepository persists pool snapshots in PostgreSQL.
type SnapshotRepository struct {
	db *DB
}

func NewSnapshotRepository(db *DB) *SnapshotRepository {
	return &SnapshotRepository{db: db}
}

func (r *SnapshotRepository) Save(ctx context.Context, snapshot *marketv3.Snapshot) error {
	tx, err := r.db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := upsertSnapshot(ctx, tx, snapshot); err != nil {
		return err
	}
	if err := replaceSnapshotTicks(ctx, tx, snapshot); err != nil {
		return err
	}
	if err := replaceSnapshotBitmap(ctx, tx, snapshot); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *SnapshotRepository) GetLatest(ctx context.Context, poolAddress common.Address) (*marketv3.Snapshot, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT block_number, sqrt_price_x96::text, tick, liquidity::text,
		       fee_growth_global0_x128::text, fee_growth_global1_x128::text, created_at
		FROM snapshots
		WHERE pool_address = $1
		ORDER BY block_number DESC
		LIMIT 1
	`, codec.AddressToBytes(poolAddress))
	return scanSnapshotRow(ctx, r.db.pool, poolAddress, row)
}

func (r *SnapshotRepository) GetAtBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*marketv3.Snapshot, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT block_number, sqrt_price_x96::text, tick, liquidity::text,
		       fee_growth_global0_x128::text, fee_growth_global1_x128::text, created_at
		FROM snapshots
		WHERE pool_address = $1 AND block_number = $2
	`, codec.AddressToBytes(poolAddress), blockNumber)
	return scanSnapshotRow(ctx, r.db.pool, poolAddress, row)
}

func (r *SnapshotRepository) DeleteAfterBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) error {
	_, err := r.db.pool.Exec(ctx, `
		DELETE FROM snapshots
		WHERE pool_address = $1 AND block_number > $2
	`, codec.AddressToBytes(poolAddress), blockNumber)
	if err != nil {
		return fmt.Errorf("delete snapshots after block: %w", err)
	}
	return nil
}

func upsertSnapshot(ctx context.Context, tx pgx.Tx, snapshot *marketv3.Snapshot) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO snapshots (
			pool_address, block_number, sqrt_price_x96, tick, liquidity,
			fee_growth_global0_x128, fee_growth_global1_x128, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (pool_address, block_number) DO UPDATE SET
			sqrt_price_x96 = EXCLUDED.sqrt_price_x96,
			tick = EXCLUDED.tick,
			liquidity = EXCLUDED.liquidity,
			fee_growth_global0_x128 = EXCLUDED.fee_growth_global0_x128,
			fee_growth_global1_x128 = EXCLUDED.fee_growth_global1_x128,
			created_at = EXCLUDED.created_at
	`,
		codec.AddressToBytes(snapshot.PoolAddress),
		snapshot.BlockNumber,
		snapshot.State.SqrtPriceX96.String(),
		snapshot.State.Tick,
		snapshot.State.Liquidity.String(),
		snapshot.State.FeeGrowthGlobal0X128.String(),
		snapshot.State.FeeGrowthGlobal1X128.String(),
		snapshot.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert snapshot: %w", err)
	}
	return nil
}

func replaceSnapshotTicks(ctx context.Context, tx pgx.Tx, snapshot *marketv3.Snapshot) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM snapshot_ticks
		WHERE pool_address = $1 AND block_number = $2
	`, codec.AddressToBytes(snapshot.PoolAddress), snapshot.BlockNumber); err != nil {
		return fmt.Errorf("delete snapshot ticks: %w", err)
	}
	for _, tick := range snapshot.Ticks.ExportTicks() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO snapshot_ticks (pool_address, block_number, tick_index, liquidity_gross, liquidity_net)
			VALUES ($1, $2, $3, $4, $5)
		`,
			codec.AddressToBytes(snapshot.PoolAddress),
			snapshot.BlockNumber,
			tick.Index,
			tick.LiquidityGross.String(),
			tick.LiquidityNet.String(),
		); err != nil {
			return fmt.Errorf("insert snapshot tick: %w", err)
		}
	}
	return nil
}

func replaceSnapshotBitmap(ctx context.Context, tx pgx.Tx, snapshot *marketv3.Snapshot) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM snapshot_tick_bitmap
		WHERE pool_address = $1 AND block_number = $2
	`, codec.AddressToBytes(snapshot.PoolAddress), snapshot.BlockNumber); err != nil {
		return fmt.Errorf("delete snapshot bitmap: %w", err)
	}
	for _, word := range snapshot.Bitmap.ExportBitmap() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO snapshot_tick_bitmap (pool_address, block_number, word_pos, word_value)
			VALUES ($1, $2, $3, $4)
		`,
			codec.AddressToBytes(snapshot.PoolAddress),
			snapshot.BlockNumber,
			word.WordPos,
			word.Word.String(),
		); err != nil {
			return fmt.Errorf("insert snapshot bitmap word: %w", err)
		}
	}
	return nil
}

type snapshotRowScanner interface {
	Scan(dest ...any) error
}

func scanSnapshotRow(ctx context.Context, pool pgxQueryPool, poolAddress common.Address, row snapshotRowScanner) (*marketv3.Snapshot, error) {
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
		return nil, fmt.Errorf("scan snapshot: %w", err)
	}

	ticks, err := loadSnapshotTicks(ctx, pool, poolAddress, blockNumber)
	if err != nil {
		return nil, err
	}
	bitmap, err := loadSnapshotBitmap(ctx, pool, poolAddress, blockNumber)
	if err != nil {
		return nil, err
	}

	return &marketv3.Snapshot{
		PoolAddress: poolAddress,
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

func loadSnapshotTicks(ctx context.Context, pool pgxQueryPool, poolAddress common.Address, blockNumber uint64) (market.TickTable, error) {
	rows, err := pool.Query(ctx, `
		SELECT tick_index, liquidity_gross::text, liquidity_net::text
		FROM snapshot_ticks
		WHERE pool_address = $1 AND block_number = $2
	`, codec.AddressToBytes(poolAddress), blockNumber)
	if err != nil {
		return market.TickTable{}, fmt.Errorf("query snapshot ticks: %w", err)
	}
	defer rows.Close()

	records := make([]market.PersistedTick, 0)
	for rows.Next() {
		var index int32
		var gross, net string
		if err := rows.Scan(&index, &gross, &net); err != nil {
			return market.TickTable{}, fmt.Errorf("scan snapshot tick: %w", err)
		}
		records = append(records, market.PersistedTick{
			Index:          index,
			LiquidityGross: parseNumericString(gross),
			LiquidityNet:   parseNumericString(net),
		})
	}
	return market.ImportTickTable(records), rows.Err()
}

func loadSnapshotBitmap(ctx context.Context, pool pgxQueryPool, poolAddress common.Address, blockNumber uint64) (market.TickBitmap, error) {
	rows, err := pool.Query(ctx, `
		SELECT word_pos, word_value::text
		FROM snapshot_tick_bitmap
		WHERE pool_address = $1 AND block_number = $2
	`, codec.AddressToBytes(poolAddress), blockNumber)
	if err != nil {
		return market.TickBitmap{}, fmt.Errorf("query snapshot bitmap: %w", err)
	}
	defer rows.Close()

	records := make([]market.PersistedBitmapWord, 0)
	for rows.Next() {
		var wordPos int16
		var wordValue string
		if err := rows.Scan(&wordPos, &wordValue); err != nil {
			return market.TickBitmap{}, fmt.Errorf("scan snapshot bitmap: %w", err)
		}
		records = append(records, market.PersistedBitmapWord{
			WordPos: wordPos,
			Word:    parseNumericString(wordValue),
		})
	}
	return market.ImportTickBitmap(records), rows.Err()
}

type pgxQueryPool interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}
