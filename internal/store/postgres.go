package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore PostgreSQL 实现。
type PostgresStore struct {
	pool  *pgxpool.Pool
}

// NewPostgresStore 创建 PostgreSQL 持久化存储。
// 数据库迁移请使用独立的 migrate 工具：go run ./cmd/migrate/ -db "$DB_URL" up
func NewPostgresStore(ctx context.Context, connString string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Save(ctx context.Context, snap *PoolSnapshot) error {
	tickJSON, err := json.Marshal(snap.TickData)
	if err != nil {
		return fmt.Errorf("marshal tick data: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO pool_states
			(pool_address, block_number, tick, sqrt_price_x96, liquidity, price0_in_1, tick_data, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (pool_address) DO UPDATE SET
			block_number   = EXCLUDED.block_number,
			tick           = EXCLUDED.tick,
			sqrt_price_x96 = EXCLUDED.sqrt_price_x96,
			liquidity      = EXCLUDED.liquidity,
			price0_in_1    = EXCLUDED.price0_in_1,
			tick_data      = EXCLUDED.tick_data,
			updated_at     = EXCLUDED.updated_at`,
		snap.PoolAddress,
		snap.BlockNumber,
		snap.Tick,
		snap.SqrtPriceX96.String(),
		snap.Liquidity.String(),
		snap.Price0In1,
		string(tickJSON),
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("insert pool state: %w", err)
	}
	return nil
}

func (s *PostgresStore) Load(ctx context.Context, poolAddress string) (*PoolSnapshot, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT block_number, tick, sqrt_price_x96, liquidity, price0_in_1, tick_data
		FROM pool_states WHERE pool_address = $1`,
		poolAddress,
	)

	var (
		blockNum  uint64
		tick      int32
		sqrtPrice string
		liquidity string
		price0    float64
		tickJSON  string
	)

	err := row.Scan(&blockNum, &tick, &sqrtPrice, &liquidity, &price0, &tickJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // 池子从未被持久化，不是错误
		}
		return nil, fmt.Errorf("scan pool state: %w", err)
	}

	sqrtPriceX96, ok := new(big.Int).SetString(sqrtPrice, 10)
	if !ok {
		return nil, fmt.Errorf("invalid sqrt_price_x96: %s", sqrtPrice)
	}
	liq, ok := new(big.Int).SetString(liquidity, 10)
	if !ok {
		return nil, fmt.Errorf("invalid liquidity: %s", liquidity)
	}

	var tickData map[string]string
	if tickJSON != "" && tickJSON != "{}" {
		if err := json.Unmarshal([]byte(tickJSON), &tickData); err != nil {
			return nil, fmt.Errorf("unmarshal tick data: %w", err)
		}
	}
	if tickData == nil {
		tickData = make(map[string]string)
	}

	return &PoolSnapshot{
		PoolAddress:  poolAddress,
		BlockNumber:  blockNum,
		Tick:         tick,
		SqrtPriceX96: sqrtPriceX96,
		Liquidity:    liq,
		Price0In1:    price0,
		TickData:     tickData,
	}, nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}
