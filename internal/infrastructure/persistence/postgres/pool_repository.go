package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolRepository persists pool aggregate state in PostgreSQL.
type PoolRepository struct {
	db *DB
}

func NewPoolRepository(db *DB) *PoolRepository {
	return &PoolRepository{db: db}
}

func (r *PoolRepository) Save(ctx context.Context, pool *market.Pool) error {
	tx, err := r.db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := upsertPool(ctx, tx, pool); err != nil {
		return err
	}
	if err := replacePoolTicks(ctx, tx, pool); err != nil {
		return err
	}
	if err := replacePoolBitmap(ctx, tx, pool); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PoolRepository) Get(ctx context.Context, address common.Address) (*market.Pool, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT token0, token1, fee, tick_spacing, status, last_block_number,
		       sqrt_price_x96::text, tick, liquidity::text,
		       fee_growth_global0_x128::text, fee_growth_global1_x128::text
		FROM pools
		WHERE address = $1
	`, codec.AddressToBytes(address))

	var (
		token0, token1         []byte
		fee                    uint32
		tickSpacing            int32
		status                 string
		lastBlockNumber        uint64
		sqrtPrice, liquidity   string
		tick                   int32
		feeGrowth0, feeGrowth1 string
	)
	if err := row.Scan(
		&token0, &token1, &fee, &tickSpacing, &status, &lastBlockNumber,
		&sqrtPrice, &tick, &liquidity, &feeGrowth0, &feeGrowth1,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan pool: %w", err)
	}

	ticks, err := loadPoolTicks(ctx, r.db.pool, address)
	if err != nil {
		return nil, err
	}
	bitmap, err := loadPoolBitmap(ctx, r.db.pool, address)
	if err != nil {
		return nil, err
	}

	pool := market.NewPool(address, codec.BytesToAddress(token0), codec.BytesToAddress(token1), fee, tickSpacing)
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

func (r *PoolRepository) Delete(ctx context.Context, address common.Address) error {
	_, err := r.db.pool.Exec(ctx, `DELETE FROM pools WHERE address = $1`, codec.AddressToBytes(address))
	if err != nil {
		return fmt.Errorf("delete pool: %w", err)
	}
	return nil
}

func (r *PoolRepository) AdvanceSyncProgress(ctx context.Context, address common.Address, blockNumber uint64) error {
	return r.AdvanceSyncProgressMany(ctx, []common.Address{address}, blockNumber)
}

func (r *PoolRepository) AdvanceSyncProgressMany(ctx context.Context, addresses []common.Address, blockNumber uint64) error {
	if len(addresses) == 0 {
		return nil
	}
	if len(addresses) == 1 {
		return r.advanceSyncProgressOne(ctx, addresses[0], blockNumber)
	}

	poolAddresses := make([][]byte, len(addresses))
	for i, address := range addresses {
		poolAddresses[i] = codec.AddressToBytes(address)
	}

	tag, err := r.db.pool.Exec(ctx, `
		UPDATE pools SET
			last_block_number = GREATEST(last_block_number, $2),
			status = CASE WHEN status = $3 THEN $4 ELSE status END,
			updated_at = NOW()
		WHERE address = ANY($1)
	`,
		poolAddresses,
		blockNumber,
		string(market.PoolStatusCatchingUp),
		string(market.PoolStatusSyncing),
	)
	if err != nil {
		return fmt.Errorf("advance sync progress: %w", err)
	}
	if tag.RowsAffected() != int64(len(addresses)) {
		return fmt.Errorf("expected to update %d pools, updated %d", len(addresses), tag.RowsAffected())
	}
	return nil
}

func (r *PoolRepository) advanceSyncProgressOne(ctx context.Context, address common.Address, blockNumber uint64) error {
	tag, err := r.db.pool.Exec(ctx, `
		UPDATE pools SET
			last_block_number = GREATEST(last_block_number, $2),
			status = CASE WHEN status = $3 THEN $4 ELSE status END,
			updated_at = NOW()
		WHERE address = $1
	`,
		codec.AddressToBytes(address),
		blockNumber,
		string(market.PoolStatusCatchingUp),
		string(market.PoolStatusSyncing),
	)
	if err != nil {
		return fmt.Errorf("advance sync progress: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("pool %s not found", address.Hex())
	}
	return nil
}

func upsertPool(ctx context.Context, tx pgx.Tx, pool *market.Pool) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO pools (
			address, token0, token1, fee, tick_spacing, status, last_block_number,
			sqrt_price_x96, tick, liquidity, fee_growth_global0_x128, fee_growth_global1_x128, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, NOW()
		)
		ON CONFLICT (address) DO UPDATE SET
			token0 = EXCLUDED.token0,
			token1 = EXCLUDED.token1,
			fee = EXCLUDED.fee,
			tick_spacing = EXCLUDED.tick_spacing,
			status = EXCLUDED.status,
			last_block_number = EXCLUDED.last_block_number,
			sqrt_price_x96 = EXCLUDED.sqrt_price_x96,
			tick = EXCLUDED.tick,
			liquidity = EXCLUDED.liquidity,
			fee_growth_global0_x128 = EXCLUDED.fee_growth_global0_x128,
			fee_growth_global1_x128 = EXCLUDED.fee_growth_global1_x128,
			updated_at = NOW()
	`,
		codec.AddressToBytes(pool.Address),
		codec.AddressToBytes(pool.Token0),
		codec.AddressToBytes(pool.Token1),
		pool.Fee,
		pool.TickSpacing,
		string(pool.Status),
		pool.LastBlockNumber,
		pool.State.SqrtPriceX96.String(),
		pool.State.Tick,
		pool.State.Liquidity.String(),
		pool.State.FeeGrowthGlobal0X128.String(),
		pool.State.FeeGrowthGlobal1X128.String(),
	)
	if err != nil {
		return fmt.Errorf("upsert pool: %w", err)
	}
	return nil
}

func replacePoolTicks(ctx context.Context, tx pgx.Tx, pool *market.Pool) error {
	if _, err := tx.Exec(ctx, `DELETE FROM pool_ticks WHERE pool_address = $1`, codec.AddressToBytes(pool.Address)); err != nil {
		return fmt.Errorf("delete pool ticks: %w", err)
	}
	for _, tick := range pool.Ticks.ExportTicks() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO pool_ticks (pool_address, tick_index, liquidity_gross, liquidity_net)
			VALUES ($1, $2, $3, $4)
		`,
			codec.AddressToBytes(pool.Address),
			tick.Index,
			tick.LiquidityGross.String(),
			tick.LiquidityNet.String(),
		); err != nil {
			return fmt.Errorf("insert pool tick: %w", err)
		}
	}
	return nil
}

func replacePoolBitmap(ctx context.Context, tx pgx.Tx, pool *market.Pool) error {
	if _, err := tx.Exec(ctx, `DELETE FROM pool_tick_bitmap WHERE pool_address = $1`, codec.AddressToBytes(pool.Address)); err != nil {
		return fmt.Errorf("delete pool bitmap: %w", err)
	}
	for _, word := range pool.Bitmap.ExportBitmap() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO pool_tick_bitmap (pool_address, word_pos, word_value)
			VALUES ($1, $2, $3)
		`,
			codec.AddressToBytes(pool.Address),
			word.WordPos,
			word.Word.String(),
		); err != nil {
			return fmt.Errorf("insert pool bitmap word: %w", err)
		}
	}
	return nil
}

func loadPoolTicks(ctx context.Context, pool *pgxpool.Pool, address common.Address) (market.TickTable, error) {
	rows, err := pool.Query(ctx, `
		SELECT tick_index, liquidity_gross::text, liquidity_net::text
		FROM pool_ticks
		WHERE pool_address = $1
	`, codec.AddressToBytes(address))
	if err != nil {
		return market.TickTable{}, fmt.Errorf("query pool ticks: %w", err)
	}
	defer rows.Close()

	records := make([]market.PersistedTick, 0)
	for rows.Next() {
		var index int32
		var gross, net string
		if err := rows.Scan(&index, &gross, &net); err != nil {
			return market.TickTable{}, fmt.Errorf("scan pool tick: %w", err)
		}
		records = append(records, market.PersistedTick{
			Index:          index,
			LiquidityGross: parseNumericString(gross),
			LiquidityNet:   parseNumericString(net),
		})
	}
	return market.ImportTickTable(records), rows.Err()
}

func loadPoolBitmap(ctx context.Context, pool *pgxpool.Pool, address common.Address) (market.TickBitmap, error) {
	rows, err := pool.Query(ctx, `
		SELECT word_pos, word_value::text
		FROM pool_tick_bitmap
		WHERE pool_address = $1
	`, codec.AddressToBytes(address))
	if err != nil {
		return market.TickBitmap{}, fmt.Errorf("query pool bitmap: %w", err)
	}
	defer rows.Close()

	records := make([]market.PersistedBitmapWord, 0)
	for rows.Next() {
		var wordPos int16
		var wordValue string
		if err := rows.Scan(&wordPos, &wordValue); err != nil {
			return market.TickBitmap{}, fmt.Errorf("scan pool bitmap: %w", err)
		}
		records = append(records, market.PersistedBitmapWord{
			WordPos: wordPos,
			Word:    parseNumericString(wordValue),
		})
	}
	return market.ImportTickBitmap(records), rows.Err()
}

func parseNumericString(value string) *big.Int {
	out, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return big.NewInt(0)
	}
	return out
}
