package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

// PancakeSnapshotRepository persists PancakeSwap V3 pool snapshots in PostgreSQL.
type PancakeSnapshotRepository struct {
	db *DB
}

func NewPancakeSnapshotRepository(db *DB) *PancakeSnapshotRepository {
	return &PancakeSnapshotRepository{db: db}
}

func (r *PancakeSnapshotRepository) Save(ctx context.Context, snapshot *marketpancake.Snapshot) error {
	tx, err := r.db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := upsertPancakeSnapshot(ctx, tx, snapshot); err != nil {
		return err
	}
	if err := replacePancakeSnapshotTicks(ctx, tx, snapshot); err != nil {
		return err
	}
	if err := replacePancakeSnapshotBitmap(ctx, tx, snapshot); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PancakeSnapshotRepository) GetLatest(ctx context.Context, poolAddress common.Address) (*marketpancake.Snapshot, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT block_number, sqrt_price_x96::text, tick, liquidity::text,
		       fee_growth_global0_x128::text, fee_growth_global1_x128::text, created_at
		FROM pancake_snapshots
		WHERE pool_address = $1
		ORDER BY block_number DESC
		LIMIT 1
	`, codec.AddressToBytes(poolAddress))
	return scanPancakeSnapshotRow(ctx, r.db.pool, poolAddress, row)
}

func (r *PancakeSnapshotRepository) GetAtBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*marketpancake.Snapshot, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT block_number, sqrt_price_x96::text, tick, liquidity::text,
		       fee_growth_global0_x128::text, fee_growth_global1_x128::text, created_at
		FROM pancake_snapshots
		WHERE pool_address = $1 AND block_number = $2
	`, codec.AddressToBytes(poolAddress), blockNumber)
	return scanPancakeSnapshotRow(ctx, r.db.pool, poolAddress, row)
}

func (r *PancakeSnapshotRepository) DeleteAfterBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) error {
	_, err := r.db.pool.Exec(ctx, `
		DELETE FROM pancake_snapshots
		WHERE pool_address = $1 AND block_number > $2
	`, codec.AddressToBytes(poolAddress), blockNumber)
	if err != nil {
		return fmt.Errorf("delete pancake snapshots after block: %w", err)
	}
	return nil
}

func upsertPancakeSnapshot(ctx context.Context, tx pgx.Tx, snapshot *marketpancake.Snapshot) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO pancake_snapshots (
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
		return fmt.Errorf("upsert pancake snapshot: %w", err)
	}
	return nil
}

func replacePancakeSnapshotTicks(ctx context.Context, tx pgx.Tx, snapshot *marketpancake.Snapshot) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM pancake_snapshot_ticks
		WHERE pool_address = $1 AND block_number = $2
	`, codec.AddressToBytes(snapshot.PoolAddress), snapshot.BlockNumber); err != nil {
		return fmt.Errorf("delete pancake snapshot ticks: %w", err)
	}
	for _, tick := range snapshot.Ticks.ExportTicks() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO pancake_snapshot_ticks (pool_address, block_number, tick_index, liquidity_gross, liquidity_net)
			VALUES ($1, $2, $3, $4, $5)
		`,
			codec.AddressToBytes(snapshot.PoolAddress),
			snapshot.BlockNumber,
			tick.Index,
			tick.LiquidityGross.String(),
			tick.LiquidityNet.String(),
		); err != nil {
			return fmt.Errorf("insert pancake snapshot tick: %w", err)
		}
	}
	return nil
}

func replacePancakeSnapshotBitmap(ctx context.Context, tx pgx.Tx, snapshot *marketpancake.Snapshot) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM pancake_snapshot_tick_bitmap
		WHERE pool_address = $1 AND block_number = $2
	`, codec.AddressToBytes(snapshot.PoolAddress), snapshot.BlockNumber); err != nil {
		return fmt.Errorf("delete pancake snapshot bitmap: %w", err)
	}
	for _, word := range snapshot.Bitmap.ExportBitmap() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO pancake_snapshot_tick_bitmap (pool_address, block_number, word_pos, word_value)
			VALUES ($1, $2, $3, $4)
		`,
			codec.AddressToBytes(snapshot.PoolAddress),
			snapshot.BlockNumber,
			word.WordPos,
			word.Word.String(),
		); err != nil {
			return fmt.Errorf("insert pancake snapshot bitmap word: %w", err)
		}
	}
	return nil
}

func scanPancakeSnapshotRow(ctx context.Context, pool pgxQueryPool, poolAddress common.Address, row snapshotRowScanner) (*marketpancake.Snapshot, error) {
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
		return nil, fmt.Errorf("scan pancake snapshot: %w", err)
	}

	ticks, err := loadPancakeSnapshotTicks(ctx, pool, poolAddress, blockNumber)
	if err != nil {
		return nil, err
	}
	bitmap, err := loadPancakeSnapshotBitmap(ctx, pool, poolAddress, blockNumber)
	if err != nil {
		return nil, err
	}

	return &marketpancake.Snapshot{
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

func loadPancakeSnapshotTicks(ctx context.Context, pool pgxQueryPool, poolAddress common.Address, blockNumber uint64) (market.TickTable, error) {
	rows, err := pool.Query(ctx, `
		SELECT tick_index, liquidity_gross::text, liquidity_net::text
		FROM pancake_snapshot_ticks
		WHERE pool_address = $1 AND block_number = $2
	`, codec.AddressToBytes(poolAddress), blockNumber)
	if err != nil {
		return market.TickTable{}, fmt.Errorf("query pancake snapshot ticks: %w", err)
	}
	defer rows.Close()

	records := make([]market.PersistedTick, 0)
	for rows.Next() {
		var index int32
		var gross, net string
		if err := rows.Scan(&index, &gross, &net); err != nil {
			return market.TickTable{}, fmt.Errorf("scan pancake snapshot tick: %w", err)
		}
		records = append(records, market.PersistedTick{
			Index:          index,
			LiquidityGross: parseNumericString(gross),
			LiquidityNet:   parseNumericString(net),
		})
	}
	return market.ImportTickTable(records), rows.Err()
}

func loadPancakeSnapshotBitmap(ctx context.Context, pool pgxQueryPool, poolAddress common.Address, blockNumber uint64) (market.TickBitmap, error) {
	rows, err := pool.Query(ctx, `
		SELECT word_pos, word_value::text
		FROM pancake_snapshot_tick_bitmap
		WHERE pool_address = $1 AND block_number = $2
	`, codec.AddressToBytes(poolAddress), blockNumber)
	if err != nil {
		return market.TickBitmap{}, fmt.Errorf("query pancake snapshot bitmap: %w", err)
	}
	defer rows.Close()

	records := make([]market.PersistedBitmapWord, 0)
	for rows.Next() {
		var wordPos int16
		var wordValue string
		if err := rows.Scan(&wordPos, &wordValue); err != nil {
			return market.TickBitmap{}, fmt.Errorf("scan pancake snapshot bitmap: %w", err)
		}
		records = append(records, market.PersistedBitmapWord{
			WordPos: wordPos,
			Word:    parseNumericString(wordValue),
		})
	}
	return market.ImportTickBitmap(records), rows.Err()
}
