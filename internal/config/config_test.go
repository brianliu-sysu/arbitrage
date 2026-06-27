package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	yaml := `
http_port: 9090
health_check_interval_sec: 60
log_file: "test.log"
max_hops: 3
chains:
  - name: "ethereum"
    ws_endpoint: "wss://example.com/ws"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    max_hops: 3
    base_tokens:
      - "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
      - "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
      - "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
        sync_from_block: 100
      - pool_address: "0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640"
`

	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if len(cfg.GetChains()) != 1 {
		t.Fatalf("expected 1 chain, got %d", len(cfg.GetChains()))
	}
	if cfg.GetChains()[0].WSEndpoint != "wss://example.com/ws" {
		t.Errorf("WSEndpoint = %q", cfg.GetChains()[0].WSEndpoint)
	}
	if cfg.HTTPPort != 9090 {
		t.Errorf("HTTPPort = %d, want 9090", cfg.HTTPPort)
	}
	if cfg.HealthCheckIntervalSec != 60 {
		t.Errorf("HealthCheckIntervalSec = %d", cfg.HealthCheckIntervalSec)
	}
	if cfg.LogFile != "test.log" {
		t.Errorf("LogFile = %q", cfg.LogFile)
	}
	if cfg.MaxHops != 3 {
		t.Errorf("MaxHops = %d, want 3", cfg.MaxHops)
	}
	if len(cfg.GetChains()[0].BaseTokens) != 3 {
		t.Errorf("BaseTokens len = %d, want 3", len(cfg.GetChains()[0].BaseTokens))
	}
	if len(cfg.GetChains()[0].Pools) != 2 {
		t.Errorf("Pools len = %d, want 2", len(cfg.GetChains()[0].Pools))
	}
	if cfg.GetChains()[0].Pools[0].PoolAddress != "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8" {
		t.Errorf("Pools[0].PoolAddress mismatch")
	}
	if cfg.GetChains()[0].Pools[0].SyncFromBlock != 100 {
		t.Errorf("Pools[0].SyncFromBlock = %d, want 100", cfg.GetChains()[0].Pools[0].SyncFromBlock)
	}
}

func TestLoadDefaults(t *testing.T) {
	yaml := `
chains:
  - name: "ethereum"
    ws_endpoint: "wss://x.com"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`

	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.HTTPPort != 8080 {
		t.Errorf("HTTPPort default = %d, want 8080", cfg.HTTPPort)
	}
	if cfg.HealthCheckIntervalSec != 30 {
		t.Errorf("HealthCheckIntervalSec default = %d, want 30", cfg.HealthCheckIntervalSec)
	}
	if cfg.MaxHops != 2 {
		t.Errorf("MaxHops default = %d, want 2", cfg.MaxHops)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadNoChains(t *testing.T) {
	yaml := `
	http_port: 8080
	`
	path := writeTempConfig(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for no chains configured")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	path := writeTempConfig(t, "not: [valid: yaml!!!")
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadWithTemplateVars(t *testing.T) {
	t.Setenv("ALCHEMY_KEY", "abc123")
	t.Setenv("CUSTOM_PORT", "9090")

	yaml := `
http_port: {{CUSTOM_PORT}}
chains:
  - name: "ethereum"
    ws_endpoint: "wss://eth-mainnet.g.alchemy.com/v2/{{ALCHEMY_KEY}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.GetChains()[0].WSEndpoint != "wss://eth-mainnet.g.alchemy.com/v2/abc123" {
		t.Errorf("WSEndpoint = %q, want with env var substituted", cfg.GetChains()[0].WSEndpoint)
	}
	if cfg.HTTPPort != 9090 {
		t.Errorf("HTTPPort = %d, want 9090", cfg.HTTPPort)
	}
}

func TestLoadMissingEnvVar(t *testing.T) {
	yaml := `
chains:
  - name: "ethereum"
    ws_endpoint: "{{MISSING_VAR_THAT_DOES_NOT_EXIST}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	// 缺失的变量保留原文本，不报错
	if cfg.GetChains()[0].WSEndpoint != "{{MISSING_VAR_THAT_DOES_NOT_EXIST}}" {
		t.Errorf("WSEndpoint = %q, want template left as-is", cfg.GetChains()[0].WSEndpoint)
	}
}

func TestLoadMultiVarsInOneLine(t *testing.T) {
	t.Setenv("HOST", "mainnet.infura.io")
	t.Setenv("KEY", "secret123")

	yaml := `
chains:
  - name: "ethereum"
    ws_endpoint: "wss://{{HOST}}/ws/v3/{{KEY}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.GetChains()[0].WSEndpoint != "wss://mainnet.infura.io/ws/v3/secret123" {
		t.Errorf("WSEndpoint = %q", cfg.GetChains()[0].WSEndpoint)
	}
}

func TestRPCEndpointAutoDerive(t *testing.T) {
	yaml := `
chains:
  - name: "ethereum"
    ws_endpoint: "wss://eth-mainnet.g.alchemy.com/v2/key123"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.GetChains()[0].RPCEndpoint != "https://eth-mainnet.g.alchemy.com/v2/key123" {
		t.Errorf("RPCEndpoint = %q, want https://...", cfg.GetChains()[0].RPCEndpoint)
	}
}

func TestRPCEndpointExplicit(t *testing.T) {
	yaml := `
chains:
  - name: "ethereum"
    ws_endpoint: "wss://example.com/ws"
    rpc_endpoint: "https://custom-rpc.example.com"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.GetChains()[0].RPCEndpoint != "https://custom-rpc.example.com" {
		t.Errorf("RPCEndpoint = %q, want explicit value", cfg.GetChains()[0].RPCEndpoint)
	}
}

func TestRPCEndpointDeriveWS(t *testing.T) {
	yaml := `
chains:
  - name: "ethereum"
    ws_endpoint: "ws://localhost:8546"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.GetChains()[0].RPCEndpoint != "http://localhost:8546" {
		t.Errorf("RPCEndpoint = %q, want http://localhost:8546", cfg.GetChains()[0].RPCEndpoint)
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestValidateValidConfig(t *testing.T) {
	yaml := `
http_port: 8080
max_hops: 3
chains:
  - name: "ethereum"
    ws_endpoint: "wss://eth.example.com"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    base_tokens:
      - "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
      - "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	cfg, err := Load(writeTempConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate should succeed: %v", err)
	}
}

func TestValidateInvalidHTTPPort(t *testing.T) {
	cfg := &AppConfig{
		HTTPPort: 70000,
		MaxHops:  2,
		Chains: []ChainConfig{{
			Name:     "ethereum",
			WSEndpoint: "wss://x.com",
			FactoryAddress: "0x1F98431c8aD98523631AE4a59f267346ea31F984",
			BaseTokens: []string{"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"},
			Pools:     []PoolConfig{{PoolAddress: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"}},
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid HTTP port")
	}
}

func TestValidateHTTPPortZero(t *testing.T) {
	cfg := &AppConfig{
		HTTPPort: 0,
		MaxHops:  2,
		Chains: []ChainConfig{{
			Name:     "ethereum",
			WSEndpoint: "wss://x.com",
			FactoryAddress: "0x1F98431c8aD98523631AE4a59f267346ea31F984",
			BaseTokens: []string{"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"},
			Pools:     []PoolConfig{{PoolAddress: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"}},
		}},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate should succeed with port 0: %v", err)
	}
}

func TestValidateInvalidMaxHops(t *testing.T) {
	tests := []struct {
		maxHops int
		invalid bool
	}{
		{0, true},  // less than 1
		{1, false}, // valid
		{10, false}, // valid
		{11, true}, // greater than 10
	}
	for _, tt := range tests {
		cfg := &AppConfig{
			MaxHops: tt.maxHops,
			Chains: []ChainConfig{{
				Name:     "ethereum",
				WSEndpoint: "wss://x.com",
				FactoryAddress: "0x1F98431c8aD98523631AE4a59f267346ea31F984",
				BaseTokens: []string{"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"},
				Pools:     []PoolConfig{{PoolAddress: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"}},
			}},
		}
		err := cfg.Validate()
		if tt.invalid && err == nil {
			t.Errorf("expected error for max_hops=%d", tt.maxHops)
		}
		if !tt.invalid && err != nil {
			t.Errorf("unexpected error for max_hops=%d: %v", tt.maxHops, err)
		}
	}
}

func TestValidateInvalidLogLevel(t *testing.T) {
	cfg := &AppConfig{
		MaxHops:  2,
		LogLevel: "invalid_level",
		Chains: []ChainConfig{{
			Name:     "ethereum",
			WSEndpoint: "wss://x.com",
			FactoryAddress: "0x1F98431c8aD98523631AE4a59f267346ea31F984",
			BaseTokens: []string{"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"},
			Pools:     []PoolConfig{{PoolAddress: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"}},
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid log_level")
	}
}

func TestValidateValidLogLevels(t *testing.T) {
	for _, level := range []string{"", "debug", "info", "warn", "error"} {
		cfg := &AppConfig{
			MaxHops:  2,
			LogLevel: level,
			Chains: []ChainConfig{{
				Name:     "ethereum",
				WSEndpoint: "wss://x.com",
				FactoryAddress: "0x1F98431c8aD98523631AE4a59f267346ea31F984",
				BaseTokens: []string{"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"},
				Pools:     []PoolConfig{{PoolAddress: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"}},
			}},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate should succeed for log_level=%q: %v", level, err)
		}
	}
}

func TestValidateNoChains(t *testing.T) {
	cfg := &AppConfig{MaxHops: 2}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for no chains")
	}
}

func TestValidateChainNoName(t *testing.T) {
	cfg := &AppConfig{
		MaxHops: 2,
		Chains:  []ChainConfig{{}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for chain without name")
	}
}

func TestValidateChainNoWSEndpoint(t *testing.T) {
	cfg := &AppConfig{
		MaxHops: 2,
		Chains: []ChainConfig{{
			Name:     "ethereum",
			FactoryAddress: "0x1F98431c8aD98523631AE4a59f267346ea31F984",
			BaseTokens: []string{"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"},
			Pools:     []PoolConfig{{PoolAddress: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"}},
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for chain without ws_endpoint")
	}
}

func TestValidateChainInvalidFactoryAddress(t *testing.T) {
	cfg := &AppConfig{
		MaxHops: 2,
		Chains: []ChainConfig{{
			Name:     "ethereum",
			WSEndpoint: "wss://x.com",
			FactoryAddress: "not-an-address",
			BaseTokens: []string{"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"},
			Pools:     []PoolConfig{{PoolAddress: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"}},
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid factory_address")
	}
}

func TestValidateChainInvalidQuoterAddress(t *testing.T) {
	cfg := &AppConfig{
		MaxHops: 2,
		Chains: []ChainConfig{{
			Name:            "ethereum",
			WSEndpoint:      "wss://x.com",
			FactoryAddress:  "0x1F98431c8aD98523631AE4a59f267346ea31F984",
			QuoterAddress:   "not-an-address",
			BaseTokens:      []string{"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"},
			Pools:           []PoolConfig{{PoolAddress: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"}},
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid quoter_address")
	}
}

func TestValidateChainEmptyBaseTokens(t *testing.T) {
	cfg := &AppConfig{
		MaxHops: 2,
		Chains: []ChainConfig{{
			Name:     "ethereum",
			WSEndpoint: "wss://x.com",
			FactoryAddress: "0x1F98431c8aD98523631AE4a59f267346ea31F984",
			BaseTokens: []string{},
			Pools:     []PoolConfig{{PoolAddress: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"}},
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty base_tokens")
	}
}

func TestValidateChainInvalidBaseToken(t *testing.T) {
	cfg := &AppConfig{
		MaxHops: 2,
		Chains: []ChainConfig{{
			Name:     "ethereum",
			WSEndpoint: "wss://x.com",
			FactoryAddress: "0x1F98431c8aD98523631AE4a59f267346ea31F984",
			BaseTokens: []string{"invalid_address"},
			Pools:     []PoolConfig{{PoolAddress: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"}},
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid base_token address")
	}
}

func TestValidateChainMaxHopsInvalid(t *testing.T) {
	cfg := &AppConfig{
		MaxHops: 2,
		Chains: []ChainConfig{{
			Name:     "ethereum",
			WSEndpoint: "wss://x.com",
			FactoryAddress: "0x1F98431c8aD98523631AE4a59f267346ea31F984",
			MaxHops:  11,
			BaseTokens: []string{"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"},
			Pools:     []PoolConfig{{PoolAddress: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"}},
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for chain max_hops=11")
	}
}

func TestValidateChainMaxHopsZeroIsOK(t *testing.T) {
	cfg := &AppConfig{
		MaxHops: 2,
		Chains: []ChainConfig{{
			Name:     "ethereum",
			WSEndpoint: "wss://x.com",
			FactoryAddress: "0x1F98431c8aD98523631AE4a59f267346ea31F984",
			MaxHops:  0, // uses global default
			BaseTokens: []string{"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"},
			Pools:     []PoolConfig{{PoolAddress: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"}},
		}},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate should succeed for chain max_hops=0: %v", err)
	}
}

func TestValidateChainNoPools(t *testing.T) {
	cfg := &AppConfig{
		MaxHops: 2,
		Chains: []ChainConfig{{
			Name:     "ethereum",
			WSEndpoint: "wss://x.com",
			FactoryAddress: "0x1F98431c8aD98523631AE4a59f267346ea31F984",
			BaseTokens: []string{"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"},
			Pools:     []PoolConfig{},
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for chain with no pools")
	}
}

func TestValidateChainInvalidPoolAddress(t *testing.T) {
	cfg := &AppConfig{
		MaxHops: 2,
		Chains: []ChainConfig{{
			Name:     "ethereum",
			WSEndpoint: "wss://x.com",
			FactoryAddress: "0x1F98431c8aD98523631AE4a59f267346ea31F984",
			BaseTokens: []string{"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"},
			Pools:     []PoolConfig{{PoolAddress: "not_an_address"}},
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid pool_address")
	}
}

func TestGetAutoDiscover(t *testing.T) {
	tests := []struct {
		name     string
		input    AutoDiscoverConfig
		expected AutoDiscoverConfig
	}{
		{
			name: "all defaults",
			input: AutoDiscoverConfig{},
			expected: AutoDiscoverConfig{
				SubgraphURL:  "https://api.thegraph.com/subgraphs/name/uniswap/uniswap-v3",
				MinTVLUSD:    500_000,
				MinVolumeUSD: 10_000_000,
				MaxPools:     20,
				OrderBy:      "volumeUSD",
			},
		},
		{
			name: "custom values",
			input: AutoDiscoverConfig{
				SubgraphURL:  "https://custom.subgraph.com",
				MinTVLUSD:    100_000,
				MinVolumeUSD: 5_000_000,
				MaxPools:     10,
				OrderBy:      "totalValueLockedUSD",
			},
			expected: AutoDiscoverConfig{
				SubgraphURL:  "https://custom.subgraph.com",
				MinTVLUSD:    100_000,
				MinVolumeUSD: 5_000_000,
				MaxPools:     10,
				OrderBy:      "totalValueLockedUSD",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cc := &ChainConfig{AutoDiscover: tt.input}
			got := cc.GetAutoDiscover()
			if got.SubgraphURL != tt.expected.SubgraphURL {
				t.Errorf("SubgraphURL = %q, want %q", got.SubgraphURL, tt.expected.SubgraphURL)
			}
			if got.MinTVLUSD != tt.expected.MinTVLUSD {
				t.Errorf("MinTVLUSD = %d, want %d", got.MinTVLUSD, tt.expected.MinTVLUSD)
			}
			if got.MinVolumeUSD != tt.expected.MinVolumeUSD {
				t.Errorf("MinVolumeUSD = %d, want %d", got.MinVolumeUSD, tt.expected.MinVolumeUSD)
			}
			if got.MaxPools != tt.expected.MaxPools {
				t.Errorf("MaxPools = %d, want %d", got.MaxPools, tt.expected.MaxPools)
			}
			if got.OrderBy != tt.expected.OrderBy {
				t.Errorf("OrderBy = %q, want %q", got.OrderBy, tt.expected.OrderBy)
			}
		})
	}
}

func TestGetMulticallAddress(t *testing.T) {
	// Default
	cc := &ChainConfig{}
	if got := cc.GetMulticallAddress(); got != DefaultMulticall3Address {
		t.Errorf("default = %q, want %q", got, DefaultMulticall3Address)
	}
	// Custom
	cc.MulticallAddress = "0xcA11bde05977b3631167028862bE2a173976CA11"
	if got := cc.GetMulticallAddress(); got != "0xcA11bde05977b3631167028862bE2a173976CA11" {
		t.Errorf("custom = %q", got)
	}
}

func TestGetQuoterAddress(t *testing.T) {
	// Default
	cc := &ChainConfig{}
	if got := cc.GetQuoterAddress(); got != DefaultQuoterV2Address {
		t.Errorf("default = %q, want %q", got, DefaultQuoterV2Address)
	}
	// Custom
	cc.QuoterAddress = "0x61fFE014bA17989E743c5F6cB21bF9697530B21e"
	if got := cc.GetQuoterAddress(); got != "0x61fFE014bA17989E743c5F6cB21bF9697530B21e" {
		t.Errorf("custom = %q", got)
	}
}

func TestDeriveRPCEndpoint(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"wss://eth-mainnet.g.alchemy.com/v2/key123", "https://eth-mainnet.g.alchemy.com/v2/key123"},
		{"ws://localhost:8546", "http://localhost:8546"},
		{"https://rpc.example.com", "https://rpc.example.com"},
		{"http://localhost:8545", "http://localhost:8545"},
	}
	for _, tt := range tests {
		got := deriveRPCEndpoint(tt.input)
		if got != tt.expected {
			t.Errorf("deriveRPCEndpoint(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestIsValidAddress(t *testing.T) {
	tests := []struct {
		addr     string
		expected bool
	}{
		{"0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8", true},
		{"0x0000000000000000000000000000000000000000", true},
		{"0x1F98431c8aD98523631AE4a59f267346ea31F984", true},
		{"not_an_address", false},
		{"", false},
		{"0xSHORT", false},
		{"0xGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGG", false}, // invalid hex chars
	}
	for _, tt := range tests {
		got := isValidAddress(tt.addr)
		if got != tt.expected {
			t.Errorf("isValidAddress(%q) = %v, want %v", tt.addr, got, tt.expected)
		}
	}
}

func TestGetChains(t *testing.T) {
	cfg := &AppConfig{
		Chains: []ChainConfig{
			{Name: "ethereum"},
			{Name: "polygon"},
		},
	}
	chains := cfg.GetChains()
	if len(chains) != 2 {
		t.Errorf("len = %d, want 2", len(chains))
	}
	if chains[0].Name != "ethereum" {
		t.Errorf("chains[0].Name = %q", chains[0].Name)
	}
}

func TestLoadWithMaxHopsDefaultOnChain(t *testing.T) {
	yaml := `
max_hops: 5
chains:
  - name: "ethereum"
    ws_endpoint: "wss://x.com"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    base_tokens:
      - "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	cfg, err := Load(writeTempConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Chain MaxHops=0 should be overridden by global MaxHops=5
	if cfg.Chains[0].MaxHops != 5 {
		t.Errorf("chain MaxHops = %d, want 5", cfg.Chains[0].MaxHops)
	}
}

func TestLoadWithExplicitMaxHopsOnChain(t *testing.T) {
	yaml := `
max_hops: 5
chains:
  - name: "ethereum"
    ws_endpoint: "wss://x.com"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    max_hops: 3
    base_tokens:
      - "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	cfg, err := Load(writeTempConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Explicit chain MaxHops=3 should NOT be overridden
	if cfg.Chains[0].MaxHops != 3 {
		t.Errorf("chain MaxHops = %d, want 3", cfg.Chains[0].MaxHops)
	}
}

func TestLoadWithAutoDiscoverDefaults(t *testing.T) {
	yaml := `
chains:
  - name: "ethereum"
    ws_endpoint: "wss://x.com"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    base_tokens:
      - "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
    auto_discover:
      enabled: true
`
	cfg, err := Load(writeTempConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ad := cfg.Chains[0].GetAutoDiscover()
	if !ad.Enabled {
		t.Error("auto_discover should be enabled")
	}
	if ad.SubgraphURL == "" {
		t.Error("SubgraphURL should have default")
	}
}

func TestLoadWithMulticallAndQuoter(t *testing.T) {
	yaml := `
chains:
  - name: "ethereum"
    ws_endpoint: "wss://x.com"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    multicall_address: "0xcA11bde05977b3631167028862bE2a173976CA11"
    quoter_address: "0x61fFE014bA17989E743c5F6cB21bF9697530B21e"
    base_tokens:
      - "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	cfg, err := Load(writeTempConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Chains[0].MulticallAddress != "0xcA11bde05977b3631167028862bE2a173976CA11" {
		t.Errorf("MulticallAddress mismatch")
	}
	if cfg.Chains[0].QuoterAddress != "0x61fFE014bA17989E743c5F6cB21bF9697530B21e" {
		t.Errorf("QuoterAddress mismatch")
	}
}

func TestLoadWithRateLimit(t *testing.T) {
	yaml := `
http_rate_limit: 50
api_key: "test-key"
chains:
  - name: "ethereum"
    ws_endpoint: "wss://x.com"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    base_tokens:
      - "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	cfg, err := Load(writeTempConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPRateLimit != 50 {
		t.Errorf("HTTPRateLimit = %d, want 50", cfg.HTTPRateLimit)
	}
	if cfg.APIKey != "test-key" {
		t.Errorf("APIKey = %s, want test-key", cfg.APIKey)
	}
}

func TestLoadMultipleChains(t *testing.T) {
	yaml := `
chains:
  - name: "ethereum"
    ws_endpoint: "wss://eth.com"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    base_tokens:
      - "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
  - name: "polygon"
    ws_endpoint: "wss://poly.com"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    rpc_endpoint: "https://poly-rpc.com"
    base_tokens:
      - "0x0d500B1d8E8eF31E21C99d1Db9A6444d3ADf1270"
    pools:
      - pool_address: "0x45dDa9cb7c25131DF268515131f647d726f50608"
`
	cfg, err := Load(writeTempConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Chains) != 2 {
		t.Fatalf("expected 2 chains, got %d", len(cfg.Chains))
	}
	if cfg.Chains[0].Name != "ethereum" {
		t.Errorf("chain 0 name = %q", cfg.Chains[0].Name)
	}
	if cfg.Chains[1].Name != "polygon" {
		t.Errorf("chain 1 name = %q", cfg.Chains[1].Name)
	}
}

func TestPoolConfig(t *testing.T) {
	pc := PoolConfig{
		PoolAddress:   "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8",
		SyncFromBlock: 12345,
	}
	if pc.PoolAddress != "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8" {
		t.Error("PoolAddress mismatch")
	}
	if pc.SyncFromBlock != 12345 {
		t.Errorf("SyncFromBlock = %d, want 12345", pc.SyncFromBlock)
	}
}
