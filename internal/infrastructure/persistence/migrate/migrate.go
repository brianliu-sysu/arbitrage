package migrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const schemaTable = "schema_migrations"

// Run applies pending SQL migrations from migrationsDir in lexical order.
func Run(ctx context.Context, databaseURL, migrationsDir string) error {
	if strings.TrimSpace(databaseURL) == "" {
		return fmt.Errorf("database url is required")
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)
	if len(files) == 0 {
		return fmt.Errorf("no migration files found in %s", migrationsDir)
	}

	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer conn.Close(ctx)

	if err := ensureSchemaTable(ctx, conn); err != nil {
		return err
	}

	applied, err := loadApplied(ctx, conn)
	if err != nil {
		return err
	}

	for _, file := range files {
		if applied[file] {
			continue
		}
		path := filepath.Join(migrationsDir, file)
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file, err)
		}
		if err := applyMigration(ctx, conn, file, string(content)); err != nil {
			return fmt.Errorf("apply migration %s: %w", file, err)
		}
		fmt.Printf("applied migration %s\n", file)
	}

	return nil
}

func ensureSchemaTable(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`, schemaTable))
	if err != nil {
		return fmt.Errorf("ensure schema migrations table: %w", err)
	}
	return nil
}

func loadApplied(ctx context.Context, conn *pgx.Conn) (map[string]bool, error) {
	rows, err := conn.Query(ctx, fmt.Sprintf(`SELECT version FROM %s`, schemaTable))
	if err != nil {
		return nil, fmt.Errorf("load applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = true
	}
	return applied, rows.Err()
}

func applyMigration(ctx context.Context, conn *pgx.Conn, version, sqlContent string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, statement := range SplitSQLStatements(sqlContent) {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("exec statement: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (version, applied_at) VALUES ($1, $2)
	`, schemaTable), version, time.Now().UTC()); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}

	return tx.Commit(ctx)
}

// SplitSQLStatements splits a SQL file into executable statements.
func SplitSQLStatements(content string) []string {
	var statements []string
	var builder strings.Builder

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		builder.WriteString(line)
		builder.WriteByte('\n')
		if strings.HasSuffix(trimmed, ";") {
			statement := strings.TrimSpace(builder.String())
			statement = strings.TrimSuffix(statement, ";")
			if statement != "" {
				statements = append(statements, statement)
			}
			builder.Reset()
		}
	}

	remaining := strings.TrimSpace(builder.String())
	if remaining != "" {
		statements = append(statements, strings.TrimSuffix(remaining, ";"))
	}
	return statements
}
