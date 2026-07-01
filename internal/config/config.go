// Package config 提供 YAML 配置文件加载。
package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// PoolConfig 单个 Uniswap V3 池子的配置。
type PoolConfig struct {
	PoolAddress   string `yaml:"pool_address"`    // Uniswap V3 Pool 地址
	SyncFromBlock uint64 `yaml:"sync_from_block"` // 从哪个区块开始同步历史事件，0 表示跳过
}

// ChainConfig 单条链的完整配置。
type ChainConfig struct {
	Name            string            `yaml:"name"`             // 链名称标识
	RPCFailover     []string          `yaml:"rpc_failover"`     // RPC 故障转移列表
	WSEndpoint      string            `yaml:"ws_endpoint"`      // WebSocket 事件订阅地址
	RPCEndpoint     string            `yaml:"rpc_endpoint"`     // HTTP RPC 地址，空则从 ws_endpoint 推导
	FactoryAddress  string            `yaml:"factory_address"`  // Uniswap V3 Factory 合约地址
	BaseTokens      []string          `yaml:"base_tokens"`      // 基础代币白名单（跨池报价中间代币 + 自动发现基础代币）
	MaxHops         int               `yaml:"max_hops"`         // 跨池报价最大跳数，0 使用全局默认值
	Pools           []PoolConfig      `yaml:"pools"`            // 该链的池子列表（手动指定）
	AutoDiscover    AutoDiscoverConfig `yaml:"auto_discover"`    // Subgraph 自动发现配置
	MulticallAddress string           `yaml:"multicall_address"` // Multicall3 合约地址，空使用标准部署地址
	QuoterAddress    string           `yaml:"quoter_address"`    // Uniswap V3 QuoterV2 合约地址，空使用默认地址
}

// DefaultMulticall3Address Multicall3 在所有主流 EVM 链上的标准部署地址。
const DefaultMulticall3Address = "0xcA11bde05977b3631167028862bE2a173976CA11"
const DefaultQuoterV2Address = "0x61fFE014bA17989E743c5F6cB21bF9697530B21e"

// GetMulticallAddress 返回 Multicall3 合约地址，未配置时返回标准部署地址。
func (c *ChainConfig) GetMulticallAddress() string {
	if c.MulticallAddress != "" {
		return c.MulticallAddress
	}
	return DefaultMulticall3Address
}

// GetQuoterAddress 返回 QuoterV2 合约地址，未配置时返回默认地址。
func (c *ChainConfig) GetQuoterAddress() string {
	if c.QuoterAddress != "" {
		return c.QuoterAddress
	}
	return DefaultQuoterV2Address
}

// AutoDiscoverConfig Uniswap V3 Subgraph 自动池子发现配置。
type AutoDiscoverConfig struct {
	Enabled      bool   `yaml:"enabled"`       // 是否启用自动发现
	SubgraphURL  string `yaml:"subgraph_url"`   // 子图 API 端点，默认使用 Uniswap 官方
	MinTVLUSD    int    `yaml:"min_tvl_usd"`    // 最低 TVL（美元），默认 500,000
	MinVolumeUSD int    `yaml:"min_volume_usd"` // 最低 24h 交易量（美元），默认 10,000,000
	MaxPools     int    `yaml:"max_pools"`      // 最多添加池子数，默认 20
	OrderBy      string `yaml:"order_by"`       // 排序字段: volumeUSD / totalValueLockedUSD / txCount，默认 volumeUSD
}

// GetAutoDiscover 返回自动发现配置（带默认值）。
func (c *ChainConfig) GetAutoDiscover() AutoDiscoverConfig {
	cfg := c.AutoDiscover
	if cfg.SubgraphURL == "" {
		cfg.SubgraphURL = "https://api.thegraph.com/subgraphs/name/uniswap/uniswap-v3"
	}
	if cfg.MinTVLUSD == 0 {
		cfg.MinTVLUSD = 500_000
	}
	if cfg.MinVolumeUSD == 0 {
		cfg.MinVolumeUSD = 10_000_000
	}
	if cfg.MaxPools == 0 {
		cfg.MaxPools = 20
	}
	if cfg.OrderBy == "" {
		cfg.OrderBy = "volumeUSD"
	}
	return cfg
}

// AppConfig 应用顶层配置。
type AppConfig struct {
	HTTPPort               int           `yaml:"http_port"`                 // HTTP 端口，0 禁用，默认 8080
	HealthCheckIntervalSec int           `yaml:"health_check_interval_sec"` // 健康检查间隔，0 禁用
	PoolStatusPollIntervalSec int        `yaml:"pool_status_poll_interval_sec"` // READY 状态轮询间隔，0 默认 30 秒
	LogFile                string        `yaml:"log_file"`                  // 日志文件路径
	LogLevel               string        `yaml:"log_level"`                 // 日志级别: debug/info/warn/error，默认 info
	TracingEndpoint        string        `yaml:"tracing_endpoint"`          // OTLP 端点，空禁用
	DBURL                  string        `yaml:"db_url"`                    // PostgreSQL 连接串
	RedisURL               string        `yaml:"redis_url"`                 // Redis 连接串（token 元信息缓存），空则禁用
	MaxBlockGapForFullSync uint64        `yaml:"max_block_gap_for_full_sync"` // 全量同步最大区块间隔
	MaxHops                int           `yaml:"max_hops"`                  // 跨池报价最大跳数
	HTTPRateLimit          int           `yaml:"http_rate_limit"`           // HTTP API 每秒最大请求数，0 不限，默认 100
	APIKey                 string        `yaml:"api_key"`                    // API 鉴权 key（X-API-Key），空表示不鉴权
	Chains                 []ChainConfig `yaml:"chains"`                    // 多链配置
}

// GetChains 返回多链配置列表。
func (c *AppConfig) GetChains() []ChainConfig {
	return c.Chains
}

// Validate 校验配置合法性。
func (c *AppConfig) Validate() error {
	if c.HTTPPort != 0 && (c.HTTPPort < 1 || c.HTTPPort > 65535) {
		return fmt.Errorf("http_port must be 1-65535 or 0, got %d", c.HTTPPort)
	}
	if c.MaxHops < 1 || c.MaxHops > 10 {
		return fmt.Errorf("max_hops must be 1-10, got %d", c.MaxHops)
	}
	if c.LogLevel != "" && c.LogLevel != "debug" && c.LogLevel != "info" && c.LogLevel != "warn" && c.LogLevel != "error" {
		return fmt.Errorf("log_level must be debug/info/warn/error, got %q", c.LogLevel)
	}

	chains := c.GetChains()
	if len(chains) == 0 {
		return fmt.Errorf("no chains configured")
	}

	for i, ch := range chains {
		if ch.Name == "" {
			return fmt.Errorf("chains[%d].name is required", i)
		}
		if ch.WSEndpoint == "" {
			return fmt.Errorf("chains[%d] (%q): ws_endpoint is required", i, ch.Name)
		}
		if !isValidAddress(ch.FactoryAddress) {
			return fmt.Errorf("chains[%d] (%q): factory_address is not a valid 0x address", i, ch.Name)
		}
		if ch.QuoterAddress != "" && !isValidAddress(ch.QuoterAddress) {
			return fmt.Errorf("chains[%d] (%q): quoter_address is not a valid 0x address", i, ch.Name)
		}
		if len(ch.BaseTokens) == 0 {
			return fmt.Errorf("chains[%d] (%q): at least one base_token is required", i, ch.Name)
		}
		for j, bt := range ch.BaseTokens {
			if !isValidAddress(bt) {
				return fmt.Errorf("chains[%d] (%q).base_tokens[%d]: not a valid 0x address", i, ch.Name, j)
			}
		}
		if ch.MaxHops < 1 || ch.MaxHops > 10 {
			if ch.MaxHops != 0 {
				return fmt.Errorf("chains[%d] (%q): max_hops must be 1-10 or 0, got %d", i, ch.Name, ch.MaxHops)
			}
		}
		if len(ch.Pools) == 0 {
			return fmt.Errorf("chains[%d] (%q): at least one pool is required", i, ch.Name)
		}
		for j, pc := range ch.Pools {
			if !isValidAddress(pc.PoolAddress) {
				return fmt.Errorf("chains[%d] (%q).pools[%d]: pool_address is not a valid 0x address", i, ch.Name, j)
			}
		}
	}
	return nil
}

var addrRegex = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

func isValidAddress(addr string) bool {
	return addrRegex.MatchString(addr)
}

// Load 从 YAML 文件加载配置。
//
// 支持 {{ENV_VAR}} 模板语法：文件中所有 {{变量名}} 会被替换为对应环境变量的值。
func Load(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}

	resolved, err := resolveTemplateVars(data)
	if err != nil {
		return nil, fmt.Errorf("resolve template vars in %s: %w", path, err)
	}

	cfg := &AppConfig{
		HTTPPort:               8080,
		HealthCheckIntervalSec: 30,
		MaxHops:                2,
		MaxBlockGapForFullSync: 100,
	}

	if err := yaml.Unmarshal(resolved, cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	chains := cfg.GetChains()
	if len(chains) == 0 {
		return nil, fmt.Errorf("no chains configured: set chains in config.yaml")
	}

	// 补全每链的默认值和 RPC 推导
	for i := range cfg.Chains {
		if cfg.Chains[i].RPCEndpoint == "" && cfg.Chains[i].WSEndpoint != "" {
			cfg.Chains[i].RPCEndpoint = deriveRPCEndpoint(cfg.Chains[i].WSEndpoint)
		}
		if cfg.Chains[i].MaxHops == 0 {
			cfg.Chains[i].MaxHops = cfg.MaxHops
		}
	}

	return cfg, nil
}

// deriveRPCEndpoint 从 WebSocket URL 推导 HTTP RPC URL。
//
//	wss://eth-mainnet.g.alchemy.com/v2/KEY → https://eth-mainnet.g.alchemy.com/v2/KEY
//	ws://localhost:8546 → http://localhost:8546
func deriveRPCEndpoint(wsURL string) string {
	rpcURL := wsURL
	if strings.HasPrefix(rpcURL, "wss://") {
		rpcURL = "https://" + rpcURL[6:]
	} else if strings.HasPrefix(rpcURL, "ws://") {
		rpcURL = "http://" + rpcURL[5:]
	}
	return rpcURL
}

// templateVarPattern 匹配 {{VAR_NAME}} 模板变量。
var templateVarPattern = regexp.MustCompile(`\{\{(\w[\w\-]*)\}\}`)

// resolveTemplateVars 将 data 中的 {{VAR}} 替换为环境变量值。
// 注释中的 {{VAR}} 也会被处理，但缺失的变量不会报错——保留原文本并输出警告。
func resolveTemplateVars(data []byte) ([]byte, error) {
	stderr := func(msg string) { fmt.Fprintf(os.Stderr, "[config] WARNING: %s\n", msg) }

	result := templateVarPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		sub := templateVarPattern.FindSubmatch(match)
		if len(sub) < 2 {
			stderr(fmt.Sprintf("invalid template syntax: %s", match))
			return match
		}

		varName := string(sub[1])
		val, ok := os.LookupEnv(varName)
		if !ok {
			stderr(fmt.Sprintf("environment variable %q is not set (template %s left as-is)",
				varName, match))
			return match // 保留原文本，不报错
		}
		return []byte(val)
	})

	return result, nil
}
