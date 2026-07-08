package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

// QuickSwapSnapshotRepository persists QuickSwap V3 pool snapshots in PostgreSQL.
type QuickSwapSnapshotRepository struct {
	db *DB
}

func NewQuickSwapSnapshotRepository(db *DB) *QuickSwapSnapshotRepository {
	return &QuickSwapSnapshotRepository{db: db}
}

func (r *QuickSwapSnapshotRepository) Save(ctx context.Context, snapshot *marketquick.Snapshot) error {
	tx, err := r.db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := upsertQuickSwapSnapshot(ctx, tx, snapshot); err != nil {
		return err
	}
	if err := replaceQuickSwapSnapshotTicks(ctx, tx, snapshot); err != nil {
		return err
	}
	if err := replaceQuickSwapSnapshotBitmap(ctx, tx, snapshot); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *QuickSwapSnapshotRepository) GetLatest(ctx context.Context, poolAddress common.Address) (*marketquick.Snapshot, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT block_number, sqrt_price_x96::text, tick, liquidity::text,
		       fee_growth_global0_x128::text, fee_growth_global1_x128::text, created_at
		FROM quickswap_snapshots
		WHERE pool_address = $1
		ORDER BY block_number DESC
		LIMIT 1
	`, codec.AddressToBytes(poolAddress))
	return scanQuickSwapSnapshotRow(ctx, r.db.pool, poolAddress, row)
}

func (r *QuickSwapSnapshotRepository) GetAtBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*marketquick.Snapshot, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT block_number, sqrt_price_x96::text, tick, liquidity::text,
		       fee_growth_global0_x128::text, fee_growth_global1_x128::text, created_at
		FROM quickswap_snapshots
		WHERE pool_address = $1 AND block_number = $2
	`, codec.AddressToBytes(poolAddress), blockNumber)
	return scanQuickSwapSnapshotRow(ctx, r.db.pool, poolAddress, row)
}

func (r *QuickSwapSnapshotRepository) DeleteAfterBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) error {
	_, err := r.db.pool.Exec(ctx, `
		DELETE FROM quickswap_snapshots
		WHERE pool_address = $1 AND block_number > $2
	`, codec.AddressToBytes(poolAddress), blockNumber)
	if err != nil {
		return fmt.Errorf("delete quick snapshots after block: %w", err)
	}
	return nil
}

func upsertQuickSwapSnapshot(ctx context.Context, tx pgx.Tx, snapshot *marketquick.Snapshot) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO quickswap_snapshots (
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
		return fmt.Errorf("upsert quick snapshot: %w", err)
	}
	return nil
}

func replaceQuickSwapSnapshotTicks(ctx context.Context, tx pgx.Tx, snapshot *marketquick.Snapshot) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM quickswap_snapshot_ticks
		WHERE pool_address = $1 AND block_number = $2
	`, codec.AddressToBytes(snapshot.PoolAddress), snapshot.BlockNumber); err != nil {
		return fmt.Errorf("delete quick snapshot ticks: %w", err)
	}
	for _, tick := range snapshot.Ticks.ExportTicks() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO quickswap_snapshot_ticks (pool_address, block_number, tick_index, liquidity_gross, liquidity_net)
			VALUES ($1, $2, $3, $4, $5)
		`,
			codec.AddressToBytes(snapshot.PoolAddress),
			snapshot.BlockNumber,
			tick.Index,
			tick.LiquidityGross.String(),
			tick.LiquidityNet.String(),
		); err != nil {
			return fmt.Errorf("insert quick snapshot tick: %w", err)
		}
	}
	return nil
}

func replaceQuickSwapSnapshotBitmap(ctx context.Context, tx pgx.Tx, snapshot *marketquick.Snapshot) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM quickswap_snapshot_tick_bitmap
		WHERE pool_address = $1 AND block_number = $2
	`, codec.AddressToBytes(snapshot.PoolAddress), snapshot.BlockNumber); err != nil {
		return fmt.Errorf("delete quick snapshot bitmap: %w", err)
	}
	for _, word := range snapshot.Bitmap.ExportBitmap() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO quickswap_snapshot_tick_bitmap (pool_address, block_number, word_pos, word_value)
			VALUES ($1, $2, $3, $4)
		`,
			codec.AddressToBytes(snapshot.PoolAddress),
			snapshot.BlockNumber,
			word.WordPos,
			word.Word.String(),
		); err != nil {
			return fmt.Errorf("insert quick snapshot bitmap word: %w", err)
		}
	}
	return nil
}

func scanQuickSwapSnapshotRow(ctx context.Context, pool pgxQueryPool, poolAddress common.Address, row snapshotRowScanner) (*marketquick.Snapshot, error) {
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
		return nil, fmt.Errorf("scan quick snapshot: %w", err)
	}

	ticks, err := loadQuickSwapSnapshotTicks(ctx, pool, poolAddress, blockNumber)
	if err != nil {
		return nil, err
	}
	bitmap, err := loadQuickSwapSnapshotBitmap(ctx, pool, poolAddress, blockNumber)
	if err != nil {
		return nil, err
	}

	return &marketquick.Snapshot{
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

func loadQuickSwapSnapshotTicks(ctx context.Context, pool pgxQueryPool, poolAddress common.Address, blockNumber uint64) (market.TickTable, error) {
	rows, err := pool.Query(ctx, `
		SELECT tick_index, liquidity_gross::text, liquidity_net::text
		FROM quickswap_snapshot_ticks
		WHERE pool_address = $1 AND block_number = $2
	`, codec.AddressToBytes(poolAddress), blockNumber)
	if err != nil {
		return market.TickTable{}, fmt.Errorf("query quick snapshot ticks: %w", err)
	}
	defer rows.Close()

	records := make([]market.PersistedTick, 0)
	for rows.Next() {
		var index int32
		var gross, net string
		if err := rows.Scan(&index, &gross, &net); err != nil {
			return market.TickTable{}, fmt.Errorf("scan quick snapshot tick: %w", err)
		}
		records = append(records, market.PersistedTick{
			Index:          index,
			LiquidityGross: parseNumericString(gross),
			LiquidityNet:   parseNumericString(net),
		})
	}
	return market.ImportTickTable(records), rows.Err()
}

func loadQuickSwapSnapshotBitmap(ctx context.Context, pool pgxQueryPool, poolAddress common.Address, blockNumber uint64) (market.TickBitmap, error) {
	rows, err := pool.Query(ctx, `
		SELECT word_pos, word_value::text
		FROM quickswap_snapshot_tick_bitmap
		WHERE pool_address = $1 AND block_number = $2
	`, codec.AddressToBytes(poolAddress), blockNumber)
	if err != nil {
		return market.TickBitmap{}, fmt.Errorf("query quick snapshot bitmap: %w", err)
	}
	defer rows.Close()

	records := make([]market.PersistedBitmapWord, 0)
	for rows.Next() {
		var wordPos int16
		var wordValue string
		if err := rows.Scan(&wordPos, &wordValue); err != nil {
			return market.TickBitmap{}, fmt.Errorf("scan quick snapshot bitmap: %w", err)
		}
		records = append(records, market.PersistedBitmapWord{
			WordPos: wordPos,
			Word:    parseNumericString(wordValue),
		})
	}
	return market.ImportTickBitmap(records), rows.Err()
}
