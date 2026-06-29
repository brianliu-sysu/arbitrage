// Package store 提供池子状态持久化接口和 PostgreSQL 实现。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PoolSnapshot 池子状态快照，用于持久化和恢复。
type PoolSnapshot struct {
	ChainName    string
	PoolAddress  string
	BlockNumber  uint64
	Tick         int32
	SqrtPriceX96 *big.Int
	Liquidity    *big.Int
	Price0In1    float64
	Token0Symbol string
	Token1Symbol string
	Fee          uint32
	TickData     map[int32]TickLiquiditySnapshot
}

// TickLiquiditySnapshot 持久化的单个 tick 流动性信息。
type TickLiquiditySnapshot struct {
	LiquidityNet   *big.Int `json:"liquidityNet"`
	LiquidityGross *big.Int `json:"liquidityGross"`
}

// TokenMetadata 代币元信息缓存（按链 + 代币地址唯一）。
type TokenMetadata struct {
	ChainName    string
	TokenAddress string
	Symbol       string
	Decimals     int
}

// Storer 持久化接口。
type Storer interface {
	Save(ctx context.Context, s *PoolSnapshot) error
	// SaveHistory 将池子状态追加写入历史记录表（仅追加，不更新）。
	SaveHistory(ctx context.Context, s *PoolSnapshot) error
	// Load 加载上次保存的池子状态快照（chain + poolAddress 联合主键）。
	Load(ctx context.Context, chainName, poolAddress string) (*PoolSnapshot, error)
	// LoadAll 加载指定链下所有已保存的池子状态（按 pool_address 索引）。
	LoadAll(ctx context.Context, chainName string) (map[string]*PoolSnapshot, error)
	// LoadTokenMetadata 加载指定链上某个代币的元信息缓存。
	LoadTokenMetadata(ctx context.Context, chainName, tokenAddress string) (*TokenMetadata, error)
	// SaveTokenMetadata 保存代币元信息缓存（upsert）。
	SaveTokenMetadata(ctx context.Context, meta *TokenMetadata) error
	Close()
}

// MaxIncrementalGap 增量同步的最大区块间隔。
// 超过此值则采用全量重建（Tick Bitmap），而非事件回放。
const MaxIncrementalGap = 100

// RunMigrations 在 main 函数启动时自动执行 migrations 目录下的 SQL 迁移文件。
// 这是 cmd/migrate 工具的补充：正式部署时应使用 migrate 命令，此函数仅作为安全网确保表存在。
func RunMigrations(connString string) error {
	db, err := sql.Open("pgx", connString)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	// 确保版本表存在
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS goose_db_version (
		id SERIAL PRIMARY KEY,
		version_id BIGINT NOT NULL,
		is_applied BOOLEAN NOT NULL DEFAULT true,
		tstamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		return fmt.Errorf("create version table: %w", err)
	}

	// 查找 migrations 目录
	migrationsDir := findMigrationsDir()

	files, _ := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	sort.Strings(files)

	for _, f := range files {
		name := filepath.Base(f)
		var version int64
		fmt.Sscanf(name, "%d_", &version)

		var count int
		err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM goose_db_version WHERE version_id = $1 AND is_applied = TRUE`,
			version,
		).Scan(&count)
		if err == nil && count > 0 {
			continue // 已应用
		}

		data, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}

		upSQL := extractSection(string(data), "Up")
		if upSQL == "" {
			continue
		}

		if _, err := db.ExecContext(ctx, upSQL); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}

		if _, err := db.ExecContext(ctx,
			`INSERT INTO goose_db_version (version_id, is_applied) VALUES ($1, TRUE)`,
			version,
		); err != nil {
			return fmt.Errorf("record %s: %w", name, err)
		}
	}

	return nil
}

func findMigrationsDir() string {
	// 依次尝试常见位置
	candidates := []string{"migrations", "../migrations", "../../migrations"}
	if cwd, err := os.Getwd(); err == nil {
		for range 3 {
			cwd = filepath.Dir(cwd)
			candidates = append(candidates, filepath.Join(cwd, "migrations"))
		}
	}
	for _, dir := range candidates {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir
		}
	}
	return "migrations"
}

func extractSection(content, section string) string {
	marker := "-- +goose " + section
	idx := strings.Index(content, marker)
	if idx < 0 {
		return ""
	}
	start := strings.Index(content[idx:], "\n") + idx + 1
	remaining := content[start:]
	nextMarker := strings.Index(remaining, "\n-- +goose ")
	if nextMarker >= 0 {
		remaining = remaining[:nextMarker]
	}
	return strings.TrimSpace(remaining)
}
