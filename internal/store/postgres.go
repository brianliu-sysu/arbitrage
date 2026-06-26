package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
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
			(pool_address, chain_name, block_number, tick, sqrt_price_x96, liquidity, price0_in_1, tick_data, token0_symbol, token1_symbol, fee, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (pool_address, chain_name) DO UPDATE SET
			block_number   = EXCLUDED.block_number,
			tick           = EXCLUDED.tick,
			sqrt_price_x96 = EXCLUDED.sqrt_price_x96,
			liquidity      = EXCLUDED.liquidity,
			price0_in_1    = EXCLUDED.price0_in_1,
			tick_data      = EXCLUDED.tick_data,
			token0_symbol  = EXCLUDED.token0_symbol,
			token1_symbol  = EXCLUDED.token1_symbol,
			fee            = EXCLUDED.fee,
			updated_at     = EXCLUDED.updated_at`,
		snap.PoolAddress,
		snap.ChainName,
		snap.BlockNumber,
		snap.Tick,
		snap.SqrtPriceX96.String(),
		snap.Liquidity.String(),
		snap.Price0In1,
		string(tickJSON),
		snap.Token0Symbol,
		snap.Token1Symbol,
		snap.Fee,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("insert pool state: %w", err)
	}
	return nil
}

// SaveHistory 将池子状态追加到历史记录表，用于回溯分析价格和流动性变化。
// 每次 Swap 事件或全量/轻量同步时调用，纯追加写入，不更新已有记录。
func (s *PostgresStore) SaveHistory(ctx context.Context, snap *PoolSnapshot) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO pool_states_history
			(pool_address, block_number, tick, sqrt_price_x96, liquidity, price0_in_1)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		snap.PoolAddress,
		snap.BlockNumber,
		snap.Tick,
		snap.SqrtPriceX96.String(),
		snap.Liquidity.String(),
		snap.Price0In1,
	)
	if err != nil {
		return fmt.Errorf("insert pool state history: %w", err)
	}
	return nil
}

func (s *PostgresStore) Load(ctx context.Context, chainName, poolAddress string) (*PoolSnapshot, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT block_number, tick, sqrt_price_x96, liquidity, price0_in_1, tick_data, token0_symbol, token1_symbol, fee
		FROM pool_states WHERE pool_address = $1 AND chain_name = $2`,
		poolAddress, chainName,
	)

	var (
		blockNum  uint64
		tick      int32
		sqrtPrice string
		liquidity string
		price0    float64
		tickJSON  string
		token0Sym string
		token1Sym string
		fee       uint32
	)

	err := row.Scan(&blockNum, &tick, &sqrtPrice, &liquidity, &price0, &tickJSON, &token0Sym, &token1Sym, &fee)
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
		ChainName:    chainName,
		PoolAddress:  poolAddress,
		BlockNumber:  blockNum,
		Tick:         tick,
		SqrtPriceX96: sqrtPriceX96,
		Liquidity:    liq,
		Price0In1:    price0,
		Token0Symbol: token0Sym,
		Token1Symbol: token1Sym,
		Fee:          fee,
		TickData:     tickData,
	}, nil
}

// LoadAll 加载指定链下所有已保存的池子状态，返回 map[pool_address]*PoolSnapshot。
func (s *PostgresStore) LoadAll(ctx context.Context, chainName string) (map[string]*PoolSnapshot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pool_address, block_number, tick, sqrt_price_x96, liquidity,
		       price0_in_1, tick_data, token0_symbol, token1_symbol, fee
		FROM pool_states WHERE chain_name = $1`,
		chainName,
	)
	if err != nil {
		return nil, fmt.Errorf("query pool states for chain %s: %w", chainName, err)
	}
	defer rows.Close()

	result := make(map[string]*PoolSnapshot)
	for rows.Next() {
		var (
			poolAddr  string
			blockNum  uint64
			tick      int32
			sqrtPrice string
			liquidity string
			price0    float64
			tickJSON  string
			token0Sym string
			token1Sym string
			fee       uint32
		)
		if err := rows.Scan(&poolAddr, &blockNum, &tick, &sqrtPrice, &liquidity,
			&price0, &tickJSON, &token0Sym, &token1Sym, &fee); err != nil {
			return nil, fmt.Errorf("scan pool state: %w", err)
		}

		sqrtPriceX96, ok := new(big.Int).SetString(sqrtPrice, 10)
		if !ok {
			return nil, fmt.Errorf("invalid sqrt_price_x96: %s for pool %s", sqrtPrice, poolAddr)
		}
		liq, ok := new(big.Int).SetString(liquidity, 10)
		if !ok {
			return nil, fmt.Errorf("invalid liquidity: %s for pool %s", liquidity, poolAddr)
		}

		var tickData map[string]string
		if tickJSON != "" && tickJSON != "{}" {
			_ = json.Unmarshal([]byte(tickJSON), &tickData)
		}
		if tickData == nil {
			tickData = make(map[string]string)
		}

		result[poolAddr] = &PoolSnapshot{
			ChainName:    chainName,
			PoolAddress:  poolAddr,
			BlockNumber:  blockNum,
			Tick:         tick,
			SqrtPriceX96: sqrtPriceX96,
			Liquidity:    liq,
			Price0In1:    price0,
			Token0Symbol: token0Sym,
			Token1Symbol: token1Sym,
			Fee:          fee,
			TickData:     tickData,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pool states: %w", err)
	}

	return result, nil
}

// LoadTokenMetadata 加载代币元信息缓存。
func (s *PostgresStore) LoadTokenMetadata(ctx context.Context, chainName, tokenAddress string) (*TokenMetadata, error) {
	tokenAddress = strings.ToLower(tokenAddress)
	row := s.pool.QueryRow(ctx, `
		SELECT symbol, decimals
		FROM token_metadata
		WHERE chain_name = $1 AND token_address = $2`,
		chainName, tokenAddress,
	)

	var (
		symbol   string
		decimals int
	)
	if err := row.Scan(&symbol, &decimals); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan token metadata: %w", err)
	}

	return &TokenMetadata{
		ChainName:    chainName,
		TokenAddress: tokenAddress,
		Symbol:       symbol,
		Decimals:     decimals,
	}, nil
}

// SaveTokenMetadata 保存代币元信息缓存（upsert）。
func (s *PostgresStore) SaveTokenMetadata(ctx context.Context, meta *TokenMetadata) error {
	if meta == nil {
		return fmt.Errorf("token metadata is nil")
	}
	tokenAddress := strings.ToLower(meta.TokenAddress)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO token_metadata
			(chain_name, token_address, symbol, decimals, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (chain_name, token_address) DO UPDATE SET
			symbol     = EXCLUDED.symbol,
			decimals   = EXCLUDED.decimals,
			updated_at = EXCLUDED.updated_at`,
		meta.ChainName, tokenAddress, meta.Symbol, meta.Decimals, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("upsert token metadata: %w", err)
	}
	return nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}
