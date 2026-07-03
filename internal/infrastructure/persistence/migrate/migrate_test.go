package migrate_test

import (
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/migrate"
)

func TestSplitSQLStatements(t *testing.T) {
	sql := `-- header
CREATE TABLE IF NOT EXISTS foo (
    id INTEGER PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS bar (
    id INTEGER PRIMARY KEY
);
`
	statements := migrate.SplitSQLStatements(sql)
	if len(statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(statements))
	}
}
