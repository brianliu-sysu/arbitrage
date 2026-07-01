// Package config 鎻愪緵 YAML 閰嶇疆鏂囦欢鍔犺浇銆?
package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// PoolConfig 鍗曚釜 Uniswap V3 姹犲瓙鐨勯厤缃€?
type PoolConfig struct {
	PoolAddress   string `yaml:"pool_address"`    // Uniswap V3 Pool 鍦板潃
	SyncFromBlock uint64 `yaml:"sync_from_block"` // 浠庡摢涓尯鍧楀紑濮嬪悓姝ュ巻鍙蹭簨浠讹紝0 琛ㄧず璺宠繃
}

// ChainConfig 鍗曟潯閾剧殑瀹屾暣閰嶇疆銆?
type ChainConfig struct {
	Name             string             `yaml:"name"`              // 閾惧悕绉版爣璇?
	RPCFailover      []string           `yaml:"rpc_failover"`      // RPC 鏁呴殰杞Щ鍒楄〃
	WSEndpoint       string             `yaml:"ws_endpoint"`       // WebSocket 浜嬩欢璁㈤槄鍦板潃
	RPCEndpoint      string             `yaml:"rpc_endpoint"`      // HTTP RPC 鍦板潃锛岀┖鍒欎粠 ws_endpoint 鎺ㄥ
	FactoryAddress   string             `yaml:"factory_address"`   // Uniswap V3 Factory 鍚堢害鍦板潃
	BaseTokens       []string           `yaml:"base_tokens"`       // 鍩虹浠ｅ竵鐧藉悕鍗曪紙璺ㄦ睜鎶ヤ环涓棿浠ｅ竵 + 鑷姩鍙戠幇鍩虹浠ｅ竵锛?
	MaxHops          int                `yaml:"max_hops"`          // 璺ㄦ睜鎶ヤ环鏈€澶ц烦鏁帮紝0 浣跨敤鍏ㄥ眬榛樿鍊?
	Pools            []PoolConfig       `yaml:"pools"`             // 璇ラ摼鐨勬睜瀛愬垪琛紙鎵嬪姩鎸囧畾锛?
	AutoDiscover     AutoDiscoverConfig `yaml:"auto_discover"`     // Subgraph 鑷姩鍙戠幇閰嶇疆
	MulticallAddress string             `yaml:"multicall_address"` // Multicall3 鍚堢害鍦板潃锛岀┖浣跨敤鏍囧噯閮ㄧ讲鍦板潃
	QuoterAddress    string             `yaml:"quoter_address"`    // Uniswap V3 QuoterV2 鍚堢害鍦板潃锛岀┖浣跨敤榛樿鍦板潃
}

// DefaultMulticall3Address Multicall3 鍦ㄦ墍鏈変富娴?EVM 閾句笂鐨勬爣鍑嗛儴缃插湴鍧€銆?
const DefaultMulticall3Address = "0xcA11bde05977b3631167028862bE2a173976CA11"
const DefaultQuoterV2Address = "0x61fFE014bA17989E743c5F6cB21bF9697530B21e"

// GetMulticallAddress 杩斿洖 Multicall3 鍚堢害鍦板潃锛屾湭閰嶇疆鏃惰繑鍥炴爣鍑嗛儴缃插湴鍧€銆?
func (c *ChainConfig) GetMulticallAddress() string {
	if c.MulticallAddress != "" {
		return c.MulticallAddress
	}
	return DefaultMulticall3Address
}

// GetQuoterAddress 杩斿洖 QuoterV2 鍚堢害鍦板潃锛屾湭閰嶇疆鏃惰繑鍥為粯璁ゅ湴鍧€銆?
func (c *ChainConfig) GetQuoterAddress() string {
	if c.QuoterAddress != "" {
		return c.QuoterAddress
	}
	return DefaultQuoterV2Address
}

// AutoDiscoverConfig Uniswap V3 Subgraph 鑷姩姹犲瓙鍙戠幇閰嶇疆銆?
type AutoDiscoverConfig struct {
	Enabled      bool   `yaml:"enabled"`        // 鏄惁鍚敤鑷姩鍙戠幇
	SubgraphURL  string `yaml:"subgraph_url"`   // 瀛愬浘 API 绔偣锛岄粯璁や娇鐢?Uniswap 瀹樻柟
	MinTVLUSD    int    `yaml:"min_tvl_usd"`    // 鏈€浣?TVL锛堢編鍏冿級锛岄粯璁?500,000
	MinVolumeUSD int    `yaml:"min_volume_usd"` // 鏈€浣?24h 浜ゆ槗閲忥紙缇庡厓锛夛紝榛樿 10,000,000
	MaxPools     int    `yaml:"max_pools"`      // 鏈€澶氭坊鍔犳睜瀛愭暟锛岄粯璁?20
	OrderBy      string `yaml:"order_by"`       // 鎺掑簭瀛楁: volumeUSD / totalValueLockedUSD / txCount锛岄粯璁?volumeUSD
}

// GetAutoDiscover 杩斿洖鑷姩鍙戠幇閰嶇疆锛堝甫榛樿鍊硷級銆?
// TriangleArbitrageConfig controls in-memory triangular arbitrage detection.
type TriangleArbitrageConfig struct {
	Enabled                 bool     `yaml:"enabled"`
	AmountCandidates        []string `yaml:"amount_candidates"`
	MinProfitBps            float64  `yaml:"min_profit_bps"`
	MaxOpportunities        int      `yaml:"max_opportunities"`
	ScanThrottleIntervalSec int      `yaml:"scan_throttle_interval_sec"`
}

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

// AppConfig 搴旂敤椤跺眰閰嶇疆銆?
type AppConfig struct {
	HTTPPort                  int                     `yaml:"http_port"`                     // HTTP 绔彛锛? 绂佺敤锛岄粯璁?8080
	HealthCheckIntervalSec    int                     `yaml:"health_check_interval_sec"`     // 鍋ュ悍妫€鏌ラ棿闅旓紝0 绂佺敤
	PoolStatusPollIntervalSec int                     `yaml:"pool_status_poll_interval_sec"` // READY 鐘舵€佽疆璇㈤棿闅旓紝0 榛樿 30 绉?
	LogFile                   string                  `yaml:"log_file"`                      // 鏃ュ織鏂囦欢璺緞
	LogLevel                  string                  `yaml:"log_level"`                     // 鏃ュ織绾у埆: debug/info/warn/error锛岄粯璁?info
	TracingEndpoint           string                  `yaml:"tracing_endpoint"`              // OTLP 绔偣锛岀┖绂佺敤
	DBURL                     string                  `yaml:"db_url"`                        // PostgreSQL 杩炴帴涓?
	RedisURL                  string                  `yaml:"redis_url"`                     // Redis 杩炴帴涓诧紙token 鍏冧俊鎭紦瀛橈級锛岀┖鍒欑鐢?
	MaxBlockGapForFullSync    uint64                  `yaml:"max_block_gap_for_full_sync"`   // 鍏ㄩ噺鍚屾鏈€澶у尯鍧楅棿闅?
	MaxHops                   int                     `yaml:"max_hops"`                      // 璺ㄦ睜鎶ヤ环鏈€澶ц烦鏁?
	HTTPRateLimit             int                     `yaml:"http_rate_limit"`               // HTTP API 姣忕鏈€澶ц姹傛暟锛? 涓嶉檺锛岄粯璁?100
	APIKey                    string                  `yaml:"api_key"`                       // API 閴存潈 key锛圶-API-Key锛夛紝绌鸿〃绀轰笉閴存潈
	TriangleArbitrage         TriangleArbitrageConfig `yaml:"triangle_arbitrage"`
	Chains                    []ChainConfig           `yaml:"chains"` // 澶氶摼閰嶇疆
}

// GetChains 杩斿洖澶氶摼閰嶇疆鍒楄〃銆?
func (c *AppConfig) GetChains() []ChainConfig {
	return c.Chains
}

// Validate 鏍￠獙閰嶇疆鍚堟硶鎬с€?
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

// Load 浠?YAML 鏂囦欢鍔犺浇閰嶇疆銆?
//
// 鏀寔 {{ENV_VAR}} 妯℃澘璇硶锛氭枃浠朵腑鎵€鏈?{{鍙橀噺鍚峿} 浼氳鏇挎崲涓哄搴旂幆澧冨彉閲忕殑鍊笺€?
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

	// 琛ュ叏姣忛摼鐨勯粯璁ゅ€煎拰 RPC 鎺ㄥ
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

// deriveRPCEndpoint 浠?WebSocket URL 鎺ㄥ HTTP RPC URL銆?
//
//	wss://eth-mainnet.g.alchemy.com/v2/KEY 鈫?https://eth-mainnet.g.alchemy.com/v2/KEY
//	ws://localhost:8546 鈫?http://localhost:8546
func deriveRPCEndpoint(wsURL string) string {
	rpcURL := wsURL
	if strings.HasPrefix(rpcURL, "wss://") {
		rpcURL = "https://" + rpcURL[6:]
	} else if strings.HasPrefix(rpcURL, "ws://") {
		rpcURL = "http://" + rpcURL[5:]
	}
	return rpcURL
}

// templateVarPattern 鍖归厤 {{VAR_NAME}} 妯℃澘鍙橀噺銆?
var templateVarPattern = regexp.MustCompile(`\{\{(\w[\w\-]*)\}\}`)

// resolveTemplateVars 灏?data 涓殑 {{VAR}} 鏇挎崲涓虹幆澧冨彉閲忓€笺€?
// 娉ㄩ噴涓殑 {{VAR}} 涔熶細琚鐞嗭紝浣嗙己澶辩殑鍙橀噺涓嶄細鎶ラ敊鈥斺€斾繚鐣欏師鏂囨湰骞惰緭鍑鸿鍛娿€?
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
			return match // 淇濈暀鍘熸枃鏈紝涓嶆姤閿?
		}
		return []byte(val)
	})

	return result, nil
}
