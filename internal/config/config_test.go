package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
)

func TestLoadSyncV3Config(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: false
  database:
    url: postgres://localhost/univ3
chains:
  - name: ethereum
    chain_id: 1
    rpc:
      url: "https://eth.example"
      ws_url: "wss://eth.example"
    blockchain:
      factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
      multicall_address: "0xcA11bde05977b3631167028862bE2a173976CA11"
    sync:
      catchup_batch_size: 2000
      catchup_pool_group_size: 100
      catchup_block_span: 100
      catchup_header_concurrency: 16
      bootstrap_stale_block_threshold: 1000
      snapshot_interval: 5000
      snapshot_fallback: 10m
      reorg_max_depth: 128
      univ3:
        enabled: true
        pools:
          - address: "0x88e6A0c2dDD26FEEb64F039a2c41296Fb728693B"
            fee: 500
        subgraph:
          enabled: true
          endpoint: "https://example.com/subgraph"
          refresh_interval: 10m
          first: 50
          order_by: volume24h
          order_direction: desc
          fee_tiers: [500, 3000]
      univ4:
        enabled: false
        poolmanager:
          pools: []
        subgraph:
          enabled: false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	runtime := cfg.NormalizedChains()[0]
	if len(runtime.StaticPoolAddresses()) != 1 {
		t.Fatalf("expected 1 static pool")
	}
	if !runtime.Sync.Univ3.IsActive() {
		t.Fatal("expected univ3 sync active")
	}
	if !runtime.SubgraphPoolSource().IsEnabled() {
		t.Fatal("expected subgraph source enabled")
	}
	if runtime.SubgraphPoolSource().First != 50 {
		t.Fatalf("expected subgraph first=50, got %d", runtime.SubgraphPoolSource().First)
	}
}

func TestLoadMemoryModeConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: true
chains:
  - name: ethereum
    chain_id: 1
    rpc:
      url: "https://eth.example"
      ws_url: "wss://eth.example"
    blockchain:
      factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
      multicall_address: "0xcA11bde05977b3631167028862bE2a173976CA11"
    sync:
      catchup_batch_size: 2000
      catchup_pool_group_size: 100
      catchup_block_span: 100
      catchup_header_concurrency: 16
      bootstrap_stale_block_threshold: 1000
      snapshot_interval: 5000
      snapshot_fallback: 10m
      reorg_max_depth: 128
      univ3:
        enabled: true
        pools:
          - address: "0x88e6A0c2dDD26FEEb64F039a2c41296Fb728693B"
            fee: 500
      univ4:
        enabled: false
        poolmanager:
          pools: []
        subgraph:
          enabled: false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.MemoryMode() {
		t.Fatal("expected memory mode enabled")
	}

	pCfg := cfg.PersistenceConfig()
	if !pCfg.UseMemory {
		t.Fatal("expected persistence config to use memory")
	}
	if pCfg.DatabaseURL != "" {
		t.Fatalf("expected empty database url in memory mode, got %q", pCfg.DatabaseURL)
	}
}

func TestValidateRejectsMissingMulticallAddress(t *testing.T) {
	cfg := config.Config{
		Persistence: config.PersistenceConfig{Memory: true},
		Chains: []config.ChainConfig{{
			Name:    "ethereum",
			ChainID: 1,
			RPC: config.RPCConfig{
				URL:   "https://eth.example",
				WSURL: "wss://eth.example",
			},
		}},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "blockchain.multicall_address") {
		t.Fatalf("expected missing multicall address error, got %v", err)
	}
}

func TestValidateRejectsMissingUniv3FactoryAddress(t *testing.T) {
	cfg := config.Config{
		Persistence: config.PersistenceConfig{Memory: true},
		Chains: []config.ChainConfig{{
			Name:    "ethereum",
			ChainID: 1,
			RPC: config.RPCConfig{
				URL:   "https://eth.example",
				WSURL: "wss://eth.example",
			},
			Blockchain: config.BlockchainConfig{
				MulticallAddress: "0xcA11bde05977b3631167028862bE2a173976CA11",
			},
			Sync: config.SyncConfig{
				Univ3: config.Univ3SyncConfig{
					Enabled: true,
					Pools: []config.StaticPoolConfig{{
						Address: "0x88e6A0c2dDD26FEEb64F039a2c41296Fb728693B",
					}},
				},
			},
		}},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "blockchain.factory_address") {
		t.Fatalf("expected missing factory address error, got %v", err)
	}
}

func TestLoadExpandsEnvironmentPlaceholders(t *testing.T) {
	t.Setenv("ARBITRAGE_TEST_RPC_HOST", "rpc.example")
	t.Setenv("ARBITRAGE_TEST_RPC_TOKEN", "secret-token")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: true
chains:
  - name: ethereum
    chain_id: 1
    rpc:
      url: "https://{{ ARBITRAGE_TEST_RPC_HOST }}/{{ARBITRAGE_TEST_RPC_TOKEN}}"
      ws_url: "wss://rpc.example"
    blockchain:
      multicall_address: "0xcA11bde05977b3631167028862bE2a173976CA11"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := cfg.NormalizedChains()[0].RPC.URL; got != "https://rpc.example/secret-token" {
		t.Fatalf("unexpected expanded rpc url %q", got)
	}
}

func TestLoadRejectsMissingEnvironmentPlaceholder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: true
chains:
  - name: ethereum
    chain_id: 1
    rpc:
      url: "{{ARBITRAGE_TEST_MISSING_ENV}}"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "ARBITRAGE_TEST_MISSING_ENV") {
		t.Fatalf("expected missing environment variable error, got %v", err)
	}
}

func TestLoadFlashLoanFeeConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: true
chains:
  - name: ethereum
    chain_id: 1
    rpc:
      url: "https://eth.example"
      ws_url: "wss://eth.example"
    blockchain:
      multicall_address: "0xcA11bde05977b3631167028862bE2a173976CA11"
    arbitrage:
      flash_loan:
        balancer_fee_ppm: "1"
        univ4_fee_ppm: "2"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	flashLoan := cfg.NormalizedChains()[0].Arbitrage.FlashLoan
	if flashLoan.BalancerFee().String() != "1" {
		t.Fatalf("expected balancer fee 1, got %s", flashLoan.BalancerFee())
	}
	if flashLoan.Univ4Fee().String() != "2" {
		t.Fatalf("expected univ4 fee 2, got %s", flashLoan.Univ4Fee())
	}
}

func TestExecutionResolvedRPCURL(t *testing.T) {
	cfg := config.ExecutionConfig{RPCURL: " https://execution.example "}
	if got := cfg.ResolvedRPCURL(); got != "https://execution.example" {
		t.Fatalf("expected configured execution rpc, got %q", got)
	}
}

func TestLoadExecutionRPCURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: true
chains:
  - name: ethereum
    enabled: true
    chain_id: 1
    rpc:
      url: "https://chain.example"
      ws_url: "wss://chain.example"
    blockchain:
      multicall_address: "0xcA11bde05977b3631167028862bE2a173976CA11"
    arbitrage:
      execution:
        enabled: false
        rpc_url: "https://execution.example"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	chains := cfg.NormalizedChains()
	if len(chains) == 0 {
		t.Fatal("expected at least one chain")
	}
	execution := chains[0].Arbitrage.Execution
	if execution.RPCURL != "https://execution.example" {
		t.Fatalf("expected execution rpc_url, got %q", execution.RPCURL)
	}
	if got := execution.ResolvedRPCURL(); got != "https://execution.example" {
		t.Fatalf("expected resolved execution rpc, got %q", got)
	}
}

func TestLoadMultiChainConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: true
chains:
  - name: ethereum
    chain_id: 1
    rpc:
      url: "https://eth.example"
      ws_url: "wss://eth.example"
    blockchain:
      factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
      multicall_address: "0xcA11bde05977b3631167028862bE2a173976CA11"
    sync:
      catchup_batch_size: 2000
      catchup_pool_group_size: 100
      catchup_block_span: 100
      catchup_header_concurrency: 16
      bootstrap_stale_block_threshold: 1000
      snapshot_interval: 5000
      snapshot_fallback: 10m
      reorg_max_depth: 128
      univ3:
        enabled: true
        pools:
          - address: "0x88e6A0c2dDD26FEEb64F039a2c41296Fb728693B"
            fee: 500
    arbitrage:
      triangle:
        enabled: true
        start_tokens:
          - "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
        min_net_profit_wei: "100"
  - name: base
    chain_id: 8453
    rpc:
      url: "https://base.example"
      ws_url: "wss://base.example"
    blockchain:
      multicall_address: "0xcA11bde05977b3631167028862bE2a173976CA11"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	chains := cfg.NormalizedChains()
	if len(chains) != 2 {
		t.Fatalf("expected 2 chains, got %d", len(chains))
	}
	if chains[0].Name != "ethereum" || chains[0].ChainID != 1 || chains[0].RPC.URL != "https://eth.example" {
		t.Fatalf("unexpected first chain: %+v", chains[0])
	}
	if !chains[0].Sync.Univ3.IsActive() || !chains[0].Arbitrage.Triangle.Enabled {
		t.Fatalf("expected chain-local sync and arbitrage config, got sync=%+v arbitrage=%+v", chains[0].Sync.Univ3, chains[0].Arbitrage.Triangle)
	}
	if chains[1].Name != "base" || chains[1].ChainID != 8453 || chains[1].RPC.WSURL != "wss://base.example" {
		t.Fatalf("unexpected second chain: %+v", chains[1])
	}
	if chains[1].Blockchain.MulticallAddress != "0xcA11bde05977b3631167028862bE2a173976CA11" {
		t.Fatalf("expected chain-local multicall address, got %s", chains[1].Blockchain.MulticallAddress)
	}
}

func TestNormalizedChainsSkipsDisabledChains(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: true
chains:
  - name: ethereum
    enabled: true
    chain_id: 1
    rpc:
      url: "https://eth.example"
      ws_url: "wss://eth.example"
    blockchain:
      multicall_address: "0xcA11bde05977b3631167028862bE2a173976CA11"
  - name: polygon
    enabled: false
    chain_id: 137
    rpc:
      url: "https://polygon.example"
      ws_url: "wss://polygon.example"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	chains := cfg.NormalizedChains()
	if len(chains) != 1 {
		t.Fatalf("expected 1 enabled chain, got %d", len(chains))
	}
	if chains[0].Name != "ethereum" || chains[0].ChainID != 1 {
		t.Fatalf("unexpected enabled chain: %+v", chains[0])
	}
}

func TestLoadFailsWhenAllChainsDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: true
chains:
  - name: ethereum
    enabled: false
    chain_id: 1
    rpc:
      url: "https://eth.example"
      ws_url: "wss://eth.example"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := config.Load(path); err == nil {
		t.Fatal("expected error when all chains are disabled")
	}
}

func TestLoadRejectsLegacyTopLevelChainConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: true
chain_id: 1
rpc:
  url: "https://eth.example"
sync:
  univ3:
    enabled: true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "at least one enabled chain is required") {
		t.Fatalf("expected legacy top-level config rejection, got %v", err)
	}
}

func TestExampleConfigFilesLoad(t *testing.T) {
	t.Setenv("PRIVATE_KEY", "test-private-key")
	for _, path := range []string{
		filepath.Join("..", "..", "config.yaml"),
		filepath.Join("..", "..", "configs", "config.yaml"),
	} {
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		if len(cfg.Chains) == 0 {
			t.Fatalf("expected configured chains in %s", path)
		}
		if len(cfg.NormalizedChains()) == 0 {
			t.Fatalf("expected at least one enabled chain in %s", path)
		}
		for i, chain := range cfg.Chains {
			if chain.Name == "" || chain.ChainID == 0 {
				t.Fatalf("chain[%d] missing identity in %s: %+v", i, path, chain)
			}
			if !chain.Arbitrage.Execution.Enabled {
				continue
			}
			if got := chain.Arbitrage.Execution.ResolvedRPCURL(); got == "" {
				t.Fatalf("chain %s: execution rpc_url is required when execution is enabled", chain.Name)
			}
		}
	}
}

func TestLogConfigResolvedPaths(t *testing.T) {
	cfg := config.LogConfig{
		File:      "logs/arbitrage.log",
		ErrorFile: "logs/arbitrage.error.log",
	}

	outputPaths := cfg.ResolvedOutputPaths()
	if len(outputPaths) != 2 || outputPaths[0] != "stdout" || outputPaths[1] != "logs/arbitrage.log" {
		t.Fatalf("unexpected output paths: %#v", outputPaths)
	}

	errorPaths := cfg.ResolvedErrorOutputPaths()
	if len(errorPaths) != 2 || errorPaths[0] != "stderr" || errorPaths[1] != "logs/arbitrage.error.log" {
		t.Fatalf("unexpected error output paths: %#v", errorPaths)
	}
}

func TestValidateRejectsShortV4PoolManagerAddress(t *testing.T) {
	cfg := config.Default()
	cfg.Persistence.Memory = true
	cfg.Chains = []config.ChainConfig{{
		Name:    "ethereum",
		ChainID: 1,
		Blockchain: config.BlockchainConfig{
			PoolManagerAddress: "0x000000000004444c5dc75cb35838093bd135961",
			StateViewAddress:   "0x7ffe42c4a5deea5b0fec41c94c136cf115597227",
		},
		Sync: config.SyncConfig{
			Univ4: config.Univ4SyncConfig{
				Enabled: true,
				Subgraph: config.V4SubgraphPoolConfig{
					SubgraphPoolConfig: config.SubgraphPoolConfig{
						Enabled:  true,
						Endpoint: "https://example.com/subgraph",
					},
				},
			},
		},
	}}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected invalid pool manager address error")
	}
}
