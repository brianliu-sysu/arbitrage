// migrate 执行数据库迁移。
//
// 用法:
//
//	go run ./cmd/migrate/ -db "postgres://user:pass@localhost:5432/arbitrage?sslmode=disable" up
//	go run ./cmd/migrate/ -db "..." down
//
// 依赖 goose 库执行 SQL 迁移文件（./migrations/ 目录）。
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brianliu-sysu/arbitrage/internal/config"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML config file")
	dbURL := flag.String("db", "", "PostgreSQL connection string (overrides config file)")
	flag.Parse()

	connStr := *dbURL

	// 如果没指定 -db，尝试从 config.yaml 读取
	if connStr == "" {
		connStr = os.Getenv("DB_URL")
	}
	if connStr == "" {
		cfg, err := config.Load(*configPath)
		if err == nil && cfg.DBURL != "" {
			connStr = cfg.DBURL
		}
	}
	if connStr == "" {
		fmt.Fprintf(os.Stderr, "Usage: migrate -db <postgres_url> [up|down]\n")
		fmt.Fprintf(os.Stderr, "  -config <path>        load db_url from config.yaml\n")
		fmt.Fprintf(os.Stderr, "  DB_URL env var also accepted\n")
		os.Exit(1)
	}

	action := "up"
	if flag.NArg() > 0 {
		action = flag.Arg(0)
	}

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("ping database: %v", err)
	}

	// 确保迁移记录表存在
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS goose_db_version (
		id SERIAL PRIMARY KEY,
		version_id BIGINT NOT NULL,
		is_applied BOOLEAN NOT NULL DEFAULT true,
		tstamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		log.Fatalf("create version table: %v", err)
	}

	migrationsDir := "migrations"
	// 尝试相对路径、绝对路径等
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		// 尝试从工作目录向上找
		if cwd, err := os.Getwd(); err == nil {
			for range 3 {
				cwd = filepath.Dir(cwd)
				alt := filepath.Join(cwd, "migrations")
				if _, err := os.Stat(alt); err == nil {
					migrationsDir = alt
					break
				}
			}
		}
	}

	fmt.Printf("database: connected\n")
	fmt.Printf("migrations dir: %s\n", migrationsDir)

	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	if err != nil {
		log.Fatalf("read migrations dir: %v", err)
	}
	sort.Strings(files)

	if len(files) == 0 {
		log.Fatalf("no migration files found in %s", migrationsDir)
	}

	switch action {
	case "up":
		runUp(db, files)
	case "down":
		runDown(db, files)
	default:
		log.Fatalf("unknown action: %s (use up or down)", action)
	}
}

func runUp(db *sql.DB, files []string) {
	for _, f := range files {
		name := filepath.Base(f)
		data, err := os.ReadFile(f)
		if err != nil {
			log.Fatalf("read %s: %v", name, err)
		}

		content := string(data)
		// 只执行 +goose Up 部分
		upSQL := extractSection(content, "Up")
		if upSQL == "" {
			log.Printf("skip %s (no Up section)", name)
			continue
		}

		version := extractVersion(name)
		if alreadyApplied(db, version) {
			log.Printf("skip %s (already applied)", name)
			continue
		}

		fmt.Printf("applying %s...\n", name)
		if _, err := db.Exec(upSQL); err != nil {
			log.Fatalf("apply %s: %v", name, err)
		}

		if _, err := db.Exec(
			`INSERT INTO goose_db_version (version_id, is_applied) VALUES ($1, true)`,
			version,
		); err != nil {
			log.Fatalf("record %s: %v", name, err)
		}

		fmt.Printf("  applied %s\n", name)
	}
	fmt.Println("migration up complete")
}

func runDown(db *sql.DB, files []string) {
	// 反向执行
	for i := len(files) - 1; i >= 0; i-- {
		f := files[i]
		name := filepath.Base(f)
		data, err := os.ReadFile(f)
		if err != nil {
			log.Fatalf("read %s: %v", name, err)
		}

		content := string(data)
		downSQL := extractSection(content, "Down")
		if downSQL == "" {
			log.Printf("skip %s (no Down section)", name)
			continue
		}

		version := extractVersion(name)
		if !alreadyApplied(db, version) {
			log.Printf("skip %s (not applied)", name)
			continue
		}

		fmt.Printf("reverting %s...\n", name)
		if _, err := db.Exec(downSQL); err != nil {
			log.Fatalf("revert %s: %v", name, err)
		}

		if _, err := db.Exec(
			`DELETE FROM goose_db_version WHERE version_id = $1`,
			version,
		); err != nil {
			log.Fatalf("delete record %s: %v", name, err)
		}

		fmt.Printf("  reverted %s\n", name)
	}
}

// extractSection 从 goose 格式的 SQL 中提取指定 section 的内容。
// 格式: -- +goose Up\n...\n-- +goose Down\n...
func extractSection(content, section string) string {
	marker := "-- +goose " + section
	idx := strings.Index(content, marker)
	if idx < 0 {
		return ""
	}

	start := strings.Index(content[idx:], "\n") + idx + 1
	remaining := content[start:]

	// 截断到下一个 -- +goose 标记或文件末尾
	nextMarker := strings.Index(remaining, "\n-- +goose ")
	if nextMarker >= 0 {
		remaining = remaining[:nextMarker]
	}

	return strings.TrimSpace(remaining)
}

// extractVersion 从文件名提取版本号（如 001_create_xxx.sql → 1）。
func extractVersion(filename string) int64 {
	var v int64
	fmt.Sscanf(filename, "%d_", &v)
	return v
}

func alreadyApplied(db *sql.DB, version int64) bool {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM goose_db_version WHERE version_id = $1 AND is_applied = true`,
		version,
	).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}
