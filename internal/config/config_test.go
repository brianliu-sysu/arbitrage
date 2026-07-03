package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
)

func TestLoadPoolsConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
database:
  url: postgres://localhost/univ3
pools:
  static:
    - address: "0x88e6A0c2dDD26FEEb64F039a2c41296Fb728693B"
      fee: 500
  subgraph:
    enabled: true
    endpoint: "https://example.com/subgraph"
    first: 50
    fee_tiers: [500, 3000]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.StaticPoolAddresses()) != 1 {
		t.Fatalf("expected 1 static pool")
	}
	if !cfg.SubgraphPoolSource().IsEnabled() {
		t.Fatal("expected subgraph source enabled")
	}
	if cfg.SubgraphPoolSource().First != 50 {
		t.Fatalf("expected subgraph first=50, got %d", cfg.SubgraphPoolSource().First)
	}
}
