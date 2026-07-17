package config

import (
	"fmt"
	"math/big"
	"os"
	"regexp"
	"strings"
	"time"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	chaininfra "github.com/brianliu-sysu/uniswapv3/internal/infrastructure/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence"
	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"
)

var environmentPlaceholderPattern = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// Config is the root application configuration loaded from YAML.
type Config struct {
	App         AppConfig         `yaml:"app"`
	ChainID     uint64            `yaml:"chain_id"`
	RPC         RPCConfig         `yaml:"rpc"`
	Persistence PersistenceConfig `yaml:"persistence"`
	Blockchain  BlockchainConfig  `yaml:"blockchain"`
	Sync        SyncConfig        `yaml:"sync"`
	HTTP        HTTPConfig        `yaml:"http"`
	Quote       QuoteConfig       `yaml:"quote"`
	Arbitrage   ArbitrageConfig   `yaml:"arbitrage"`
	Chains      []ChainConfig     `yaml:"chains"`
	Log         LogConfig         `yaml:"log"`
}

type AppConfig struct {
	Name string `yaml:"name"`
}

// ChainConfig contains per-chain runtime settings. Each configured chain runs
// independent sync, quote, and arbitrage services.
type ChainConfig struct {
	Name       string           `yaml:"name"`
	Enabled    *bool            `yaml:"enabled"`
	ChainID    uint64           `yaml:"chain_id"`
	RPC        RPCConfig        `yaml:"rpc"`
	Blockchain BlockchainConfig `yaml:"blockchain"`
	Sync       SyncConfig       `yaml:"sync"`
	Quote      QuoteConfig      `yaml:"quote"`
	Arbitrage  ArbitrageConfig  `yaml:"arbitrage"`
}

// IsEnabled reports whether this chain runtime should be started.
// Default is enabled when the flag is omitted.
func (c ChainConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

type HTTPConfig struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr"`
}

type QuoteConfig struct {
	MaxHops int `yaml:"max_hops"`
}

type ArbitrageConfig struct {
	Triangle  TriangleArbitrageConfig `yaml:"triangle"`
	Spread    SpreadArbitrageConfig   `yaml:"spread"`
	FlashLoan FlashLoanConfig         `yaml:"flash_loan"`
	Execution ExecutionConfig         `yaml:"execution"`
}

type FlashLoanConfig struct {
	BalancerFeePPM string `yaml:"balancer_fee_ppm"`
	Univ4FeePPM    string `yaml:"univ4_fee_ppm"`
}

type ExecutionConfig struct {
	Enabled               bool     `yaml:"enabled"`
	RPCURL                string   `yaml:"rpc_url"`
	ExecutorAddress       string   `yaml:"executor_address"`
	PrivateKey            string   `yaml:"private_key"`
	BroadcastToken        string   `yaml:"broadcast_token"`
	FlashbotsRPCURL       string   `yaml:"flashbots_rpc_url"`
	FlashbotsPaymentBPS   uint64   `yaml:"flashbots_payment_bps"`
	SettlementSlippageBPS uint64   `yaml:"settlement_slippage_bps"`
	GasLimit              uint64   `yaml:"gas_limit"`
	GasPriceWei           string   `yaml:"gas_price_wei"`
	SkipEstimate          bool     `yaml:"skip_estimate"`
	MaxOpportunityAge     uint64   `yaml:"max_opportunity_age_blocks"`
	WETHAddress           string   `yaml:"weth_address"`
	SwapRouterV3          string   `yaml:"swap_router_v3"`
	SwapRouterPancakeV3   string   `yaml:"swap_router_pancake_v3"`
	UniversalRouter       string   `yaml:"universal_router"`
	BalancerRouterV3      string   `yaml:"balancer_router_v3"`
	AllowedRouters        []string `yaml:"allowed_routers"`
	AllowedSpenders       []string `yaml:"allowed_approval_spenders"`
}

type SpreadArbitrageConfig struct {
	Enabled             bool     `yaml:"enabled"`
	StartTokens         []string `yaml:"start_tokens"`
	MinNetProfitWei     string   `yaml:"min_net_profit_wei"`
	MinAmount           string   `yaml:"min_amount"`
	MaxAmount           string   `yaml:"max_amount"`
	OptimizerIterations int      `yaml:"optimizer_iterations"`
}

type TriangleArbitrageConfig struct {
	Enabled             bool     `yaml:"enabled"`
	StartTokens         []string `yaml:"start_tokens"`
	MinNetProfitWei     string   `yaml:"min_net_profit_wei"`
	MinAmount           string   `yaml:"min_amount"`
	MaxAmount           string   `yaml:"max_amount"`
	OptimizerIterations int      `yaml:"optimizer_iterations"`
}

type RPCConfig struct {
	URL   string `yaml:"url"`
	WSURL string `yaml:"ws_url"`
}

type DatabaseConfig struct {
	URL string `yaml:"url"`
}

type RedisConfig struct {
	URL string `yaml:"url"`
}

// PersistenceConfig selects the storage backend for pools, snapshots, and opportunities.
type PersistenceConfig struct {
	Memory   bool           `yaml:"memory"`
	Database DatabaseConfig `yaml:"database"`
	Redis    RedisConfig    `yaml:"redis"`
}

func (c PersistenceConfig) MemoryMode() bool {
	return c.Memory
}

type BlockchainConfig struct {
	FactoryAddress         string `yaml:"factory_address"`
	MulticallAddress       string `yaml:"multicall_address"`
	PoolManagerAddress     string `yaml:"pool_manager_address"`
	StateViewAddress       string `yaml:"state_view_address"`
	BalancerVaultAddress   string `yaml:"balancer_vault_address"`
	BalancerVaultV3Address string `yaml:"balancer_vault_v3_address"`
}

type SyncConfig struct {
	CatchupBatchSize             uint64                `yaml:"catchup_batch_size"`
	CatchupPoolGroupSize         uint64                `yaml:"catchup_pool_group_size"`
	CatchupBlockSpan             uint64                `yaml:"catchup_block_span"`
	CatchupHeaderConcurrency     int                   `yaml:"catchup_header_concurrency"`
	BootstrapStaleBlockThreshold uint64                `yaml:"bootstrap_stale_block_threshold"`
	SnapshotInterval             uint64                `yaml:"snapshot_interval"`
	SnapshotFallback             time.Duration         `yaml:"snapshot_fallback"`
	ReorgMaxDepth                uint64                `yaml:"reorg_max_depth"`
	Univ3                        Univ3SyncConfig       `yaml:"univ3"`
	PancakeV3                    PancakeV3SyncConfig   `yaml:"pancakev3"`
	QuickSwapV3                  QuickSwapV3SyncConfig `yaml:"quickswapv3"`
	Univ4                        Univ4SyncConfig       `yaml:"univ4"`
	Balancer                     BalancerSyncConfig    `yaml:"balancer"`
}

// Univ3SyncConfig configures Uniswap V3 pool sync sources.
type Univ3SyncConfig struct {
	Enabled  bool               `yaml:"enabled"`
	Pools    []StaticPoolConfig `yaml:"pools"`
	Subgraph SubgraphPoolConfig `yaml:"subgraph"`
}

// PancakeV3SyncConfig configures PancakeSwap V3 pool sync sources.
type PancakeV3SyncConfig struct {
	Enabled  bool               `yaml:"enabled"`
	Pools    []StaticPoolConfig `yaml:"pools"`
	Subgraph SubgraphPoolConfig `yaml:"subgraph"`
}

// QuickSwapV3SyncConfig configures QuickSwap V3 pool sync sources.
type QuickSwapV3SyncConfig struct {
	Enabled  bool               `yaml:"enabled"`
	Pools    []StaticPoolConfig `yaml:"pools"`
	Subgraph SubgraphPoolConfig `yaml:"subgraph"`
}

// Univ4SyncConfig configures Uniswap V4 pool sync sources.
type Univ4SyncConfig struct {
	Enabled     bool                 `yaml:"enabled"`
	PoolManager V4PoolManagerConfig  `yaml:"poolmanager"`
	Subgraph    V4SubgraphPoolConfig `yaml:"subgraph"`
}

// BalancerSyncConfig configures Balancer weighted/stable pool sync sources.
type BalancerSyncConfig struct {
	Enabled  bool                       `yaml:"enabled"`
	Pools    []StaticBalancerPoolConfig `yaml:"pools"`
	Subgraph BalancerSubgraphPoolConfig `yaml:"subgraph"`
}

// StaticBalancerPoolConfig defines a Balancer pool tracked by Vault PoolID.
type StaticBalancerPoolConfig struct {
	ID      string `yaml:"id"`
	Address string `yaml:"address"`
	Vault   string `yaml:"vault"`
	Type    string `yaml:"type"`
}

// V4PoolManagerConfig lists V4 pools tracked via static PoolKey definitions.
type V4PoolManagerConfig struct {
	Pools []StaticV4PoolConfig `yaml:"pools"`
}

// StaticV4PoolConfig defines a V4 pool tracked by PoolKey fields.
type StaticV4PoolConfig struct {
	Currency0   string `yaml:"currency0"`
	Currency1   string `yaml:"currency1"`
	Fee         uint32 `yaml:"fee"`
	TickSpacing int32  `yaml:"tick_spacing"`
	Hooks       string `yaml:"hooks"`
}

// StaticPoolConfig defines a V3 pool tracked by explicit on-chain address.
type StaticPoolConfig struct {
	Address string `yaml:"address"`
	Fee     uint32 `yaml:"fee"`
}

// SubgraphPoolConfig discovers pools from a Uniswap subgraph.
type SubgraphPoolConfig struct {
	Enabled                bool          `yaml:"enabled"`
	Endpoint               string        `yaml:"endpoint"`
	RefreshInterval        time.Duration `yaml:"refresh_interval"`
	First                  int           `yaml:"first"`
	Skip                   int           `yaml:"skip"`
	OrderBy                string        `yaml:"order_by"`
	OrderDirection         string        `yaml:"order_direction"`
	MinTotalValueLockedUSD string        `yaml:"min_total_value_locked_usd"`
	MinVolume24hUSD        string        `yaml:"min_volume_24h_usd"`
	Token0                 string        `yaml:"token0"`
	Token1                 string        `yaml:"token1"`
	FeeTiers               []uint32      `yaml:"fee_tiers"`
}

// DefaultV4HooksAddress is the zero address used for pools without custom hooks.
const DefaultV4HooksAddress = "0x0000000000000000000000000000000000000000"

// V4SubgraphPoolConfig discovers V4 pools from a Uniswap V4 subgraph.
type V4SubgraphPoolConfig struct {
	SubgraphPoolConfig `yaml:",inline"`
	Hooks              []string `yaml:"hooks"`
}

// BalancerSubgraphPoolConfig discovers Balancer weighted/stable pools from a subgraph.
type BalancerSubgraphPoolConfig struct {
	SubgraphPoolConfig `yaml:",inline"`
	PoolTypes          []string `yaml:"pool_types"`
	// Schema selects the subgraph query shape: "v2" (Balancer V2) or "v3" (Balancer V3).
	// When empty, it is inferred from the endpoint URL.
	Schema string `yaml:"schema"`
}

func (c BalancerSubgraphPoolConfig) ResolvedSchema() string {
	switch strings.ToLower(strings.TrimSpace(c.Schema)) {
	case "v2", "v3":
		return strings.ToLower(strings.TrimSpace(c.Schema))
	}
	endpoint := strings.ToLower(c.Endpoint)
	if strings.Contains(endpoint, "v3-pools") || strings.Contains(endpoint, "v3-vault") {
		return "v3"
	}
	return "v2"
}

func (c BalancerSubgraphPoolConfig) IsEnabled() bool {
	return c.SubgraphPoolConfig.IsEnabled()
}

func (c BalancerSubgraphPoolConfig) ResolvedPoolTypes() []string {
	if len(c.PoolTypes) == 0 {
		return []string{"Weighted", "Stable"}
	}
	return c.PoolTypes
}

func DefaultV4SubgraphHooks() []string {
	return []string{DefaultV4HooksAddress}
}

func (c V4SubgraphPoolConfig) IsEnabled() bool {
	return c.SubgraphPoolConfig.IsEnabled()
}

// ResolvedHooks returns configured hook addresses, defaulting to the zero hook.
func (c V4SubgraphPoolConfig) ResolvedHooks() []string {
	if len(c.Hooks) == 0 {
		return DefaultV4SubgraphHooks()
	}
	return c.Hooks
}

type PoolConfig = StaticPoolConfig

type LogConfig struct {
	Level            string   `yaml:"level"`
	Format           string   `yaml:"format"`
	File             string   `yaml:"file"`
	ErrorFile        string   `yaml:"error_file"`
	OutputPaths      []string `yaml:"output_paths"`
	ErrorOutputPaths []string `yaml:"error_output_paths"`
}

// ResolvedOutputPaths returns zap output paths. stdout is always included unless
// output_paths is set explicitly.
func (c LogConfig) ResolvedOutputPaths() []string {
	if len(c.OutputPaths) > 0 {
		return c.OutputPaths
	}
	paths := []string{"stdout"}
	if file := strings.TrimSpace(c.File); file != "" {
		paths = append(paths, file)
	}
	return paths
}

// ResolvedErrorOutputPaths returns zap error output paths.
func (c LogConfig) ResolvedErrorOutputPaths() []string {
	if len(c.ErrorOutputPaths) > 0 {
		return c.ErrorOutputPaths
	}
	paths := []string{"stderr"}
	if file := strings.TrimSpace(c.ErrorFile); file != "" {
		paths = append(paths, file)
	}
	return paths
}

func Default() Config {
	return Config{
		App:     AppConfig{Name: "univ3-arbitrage"},
		ChainID: 1,
		Blockchain: BlockchainConfig{
			FactoryAddress:         "0x1F98431c8aD98523631AE4a59f267346ea31F984",
			MulticallAddress:       "0xcA11bde05977b3631167028862bE2a173976CA11",
			PoolManagerAddress:     "0x000000000004444c5dc75cB358380D2e3dE08A90",
			StateViewAddress:       "0x7ffe42c4a5deea5b0fec41c94c136cf115597227",
			BalancerVaultAddress:   "0xBA12222222228d8Ba445958a75a0704d566BF2C8",
			BalancerVaultV3Address: "0xbA1333333333a1BA1108E8412f11850A5C319bA9",
		},
		Sync: SyncConfig{
			CatchupBatchSize: 2000,
			SnapshotInterval: 5000,
			SnapshotFallback: 10 * time.Minute,
			ReorgMaxDepth:    128,
			Univ3: Univ3SyncConfig{
				Enabled: true,
				Subgraph: SubgraphPoolConfig{
					First:                  100,
					OrderBy:                "volume24h",
					OrderDirection:         "desc",
					MinTotalValueLockedUSD: "1000000",
					MinVolume24hUSD:        "200000",
					RefreshInterval:        10 * time.Minute,
				},
			},
			PancakeV3: PancakeV3SyncConfig{
				Enabled: false,
				Subgraph: SubgraphPoolConfig{
					First:                  100,
					OrderBy:                "volume24h",
					OrderDirection:         "desc",
					MinTotalValueLockedUSD: "1000000",
					MinVolume24hUSD:        "200000",
					RefreshInterval:        10 * time.Minute,
				},
			},
			QuickSwapV3: QuickSwapV3SyncConfig{
				Enabled: false,
				Subgraph: SubgraphPoolConfig{
					First:                  100,
					OrderBy:                "volume24h",
					OrderDirection:         "desc",
					MinTotalValueLockedUSD: "1000000",
					MinVolume24hUSD:        "200000",
					RefreshInterval:        10 * time.Minute,
				},
			},
			Univ4: Univ4SyncConfig{
				Subgraph: V4SubgraphPoolConfig{
					SubgraphPoolConfig: SubgraphPoolConfig{
						First:                  100,
						OrderBy:                "volume24h",
						OrderDirection:         "desc",
						MinTotalValueLockedUSD: "1000000",
						MinVolume24hUSD:        "200000",
						RefreshInterval:        10 * time.Minute,
					},
					Hooks: DefaultV4SubgraphHooks(),
				},
			},
			Balancer: BalancerSyncConfig{
				Enabled: false,
				Subgraph: BalancerSubgraphPoolConfig{
					SubgraphPoolConfig: SubgraphPoolConfig{
						First:                  100,
						OrderBy:                "totalLiquidity",
						OrderDirection:         "desc",
						MinTotalValueLockedUSD: "1000000",
						RefreshInterval:        10 * time.Minute,
					},
					PoolTypes: []string{"Weighted", "Stable"},
				},
			},
		},
		HTTP: HTTPConfig{
			Enabled: true,
			Addr:    ":8080",
		},
		Quote: QuoteConfig{
			MaxHops: 3,
		},
		Arbitrage: ArbitrageConfig{
			Triangle: TriangleArbitrageConfig{
				Enabled:             false,
				MinNetProfitWei:     "1",
				MinAmount:           "1000000",
				MaxAmount:           "100000000000000",
				OptimizerIterations: 8,
			},
			Spread: SpreadArbitrageConfig{
				Enabled:             false,
				MinNetProfitWei:     "1",
				MinAmount:           "1000000",
				MaxAmount:           "100000000000000",
				OptimizerIterations: 8,
			},
			FlashLoan: FlashLoanConfig{
				BalancerFeePPM: "0",
				Univ4FeePPM:    "0",
			},
			Execution: ExecutionConfig{
				Enabled:             false,
				FlashbotsPaymentBPS: 8000,
			},
		},
		Log: LogConfig{
			Level:  "info",
			Format: "console",
		},
	}
}

func (c HTTPConfig) ListenAddr() string {
	if c.Addr == "" {
		return ":8080"
	}
	return c.Addr
}

func Load(path string) (Config, error) {
	cfg := Default()

	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config file: %w", err)
	}
	content, err = expandEnvironmentPlaceholders(content)
	if err != nil {
		return Config{}, err
	}
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config file: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func expandEnvironmentPlaceholders(content []byte) ([]byte, error) {
	var missing string
	expanded := environmentPlaceholderPattern.ReplaceAllStringFunc(string(content), func(placeholder string) string {
		matches := environmentPlaceholderPattern.FindStringSubmatch(placeholder)
		name := matches[1]
		value, ok := os.LookupEnv(name)
		if !ok && missing == "" {
			missing = name
		}
		return value
	})
	if missing != "" {
		return nil, fmt.Errorf("environment variable %s referenced by config is not set", missing)
	}
	return []byte(expanded), nil
}

func (c Config) Validate() error {
	if !c.Persistence.Memory && c.Persistence.Database.URL == "" {
		return fmt.Errorf("persistence.database.url is required unless persistence.memory is enabled")
	}
	normalizedChains := c.NormalizedChains()
	if len(normalizedChains) == 0 {
		return fmt.Errorf("at least one enabled chain is required")
	}
	seenChains := make(map[uint64]struct{})
	for _, chain := range normalizedChains {
		if chain.ChainID == 0 {
			return fmt.Errorf("chain_id is required")
		}
		if _, ok := seenChains[chain.ChainID]; ok {
			return fmt.Errorf("duplicate chain_id %d", chain.ChainID)
		}
		seenChains[chain.ChainID] = struct{}{}
		if err := validateChainConfig(chain); err != nil {
			return err
		}
	}
	return nil
}

func validateChainConfig(c ChainConfig) error {
	prefix := chainErrorPrefix(c)
	if c.Sync.Univ3.Subgraph.Enabled && c.Sync.Univ3.Subgraph.Endpoint == "" {
		return fmt.Errorf("%ssync.univ3.subgraph.endpoint is required when subgraph is enabled", prefix)
	}
	if c.Sync.PancakeV3.Subgraph.Enabled && c.Sync.PancakeV3.Subgraph.Endpoint == "" {
		return fmt.Errorf("%ssync.pancakev3.subgraph.endpoint is required when subgraph is enabled", prefix)
	}
	if c.Sync.QuickSwapV3.Subgraph.Enabled && c.Sync.QuickSwapV3.Subgraph.Endpoint == "" {
		return fmt.Errorf("%ssync.quickswapv3.subgraph.endpoint is required when subgraph is enabled", prefix)
	}
	if c.Sync.Univ4.Subgraph.Enabled && c.Sync.Univ4.Subgraph.Endpoint == "" {
		return fmt.Errorf("%ssync.univ4.subgraph.endpoint is required when subgraph is enabled", prefix)
	}
	if c.Sync.Balancer.Subgraph.Enabled && c.Sync.Balancer.Subgraph.Endpoint == "" {
		return fmt.Errorf("%ssync.balancer.subgraph.endpoint is required when subgraph is enabled", prefix)
	}
	for i, pool := range c.Sync.Univ3.Pools {
		if pool.Address == "" {
			return fmt.Errorf("%ssync.univ3.pools[%d].address is required", prefix, i)
		}
	}
	for i, pool := range c.Sync.PancakeV3.Pools {
		if pool.Address == "" {
			return fmt.Errorf("%ssync.pancakev3.pools[%d].address is required", prefix, i)
		}
	}
	for i, pool := range c.Sync.QuickSwapV3.Pools {
		if pool.Address == "" {
			return fmt.Errorf("%ssync.quickswapv3.pools[%d].address is required", prefix, i)
		}
	}
	for i, pool := range c.Sync.Univ4.PoolManager.Pools {
		if pool.Currency0 == "" || pool.Currency1 == "" {
			return fmt.Errorf("%ssync.univ4.poolmanager.pools[%d] requires currency0 and currency1", prefix, i)
		}
	}
	for i, pool := range c.Sync.Balancer.Pools {
		if pool.ID == "" || pool.Address == "" {
			return fmt.Errorf("%ssync.balancer.pools[%d] requires id and address", prefix, i)
		}
		poolType := strings.ToLower(strings.TrimSpace(pool.Type))
		if poolType != "weighted" && poolType != "stable" {
			return fmt.Errorf("%ssync.balancer.pools[%d].type must be weighted or stable", prefix, i)
		}
	}
	if c.Sync.Univ4.IsActive() {
		if c.Blockchain.PoolManagerAddress == "" {
			return fmt.Errorf("%sblockchain.pool_manager_address is required when sync.univ4 is enabled", prefix)
		}
		if !isStrictHexAddress(c.Blockchain.PoolManagerAddress) {
			return fmt.Errorf("%sblockchain.pool_manager_address must be a 20-byte hex address", prefix)
		}
		if c.Blockchain.StateViewAddress == "" {
			return fmt.Errorf("%sblockchain.state_view_address is required when sync.univ4 is enabled", prefix)
		}
		if !isStrictHexAddress(c.Blockchain.StateViewAddress) {
			return fmt.Errorf("%sblockchain.state_view_address must be a 20-byte hex address", prefix)
		}
	}
	if c.Sync.Balancer.IsActive() {
		if c.Blockchain.BalancerVaultAddress == "" {
			return fmt.Errorf("%sblockchain.balancer_vault_address is required when sync.balancer is enabled", prefix)
		}
		if !isStrictHexAddress(c.Blockchain.BalancerVaultAddress) {
			return fmt.Errorf("%sblockchain.balancer_vault_address must be a 20-byte hex address", prefix)
		}
	}
	if c.Arbitrage.Execution.Enabled {
		if !isStrictHexAddress(c.Arbitrage.Execution.ExecutorAddress) {
			return fmt.Errorf("%sarbitrage.execution.executor_address must be a 20-byte hex address", prefix)
		}
		if strings.TrimSpace(c.Arbitrage.Execution.PrivateKey) == "" {
			return fmt.Errorf("%sarbitrage.execution.private_key is required when execution is enabled", prefix)
		}
		if strings.TrimSpace(c.Arbitrage.Execution.BroadcastToken) == "" {
			return fmt.Errorf("%sarbitrage.execution.broadcast_token is required when execution is enabled", prefix)
		}
		if c.Arbitrage.Execution.FlashbotsPaymentBPS > 10_000 {
			return fmt.Errorf("%sarbitrage.execution.flashbots_payment_bps must be <= 10000", prefix)
		}
		if c.Arbitrage.Execution.SettlementSlippageBPS > 10_000 {
			return fmt.Errorf("%sarbitrage.execution.settlement_slippage_bps must be <= 10000", prefix)
		}
		for i, address := range c.Arbitrage.Execution.AllowedRouters {
			if !isStrictHexAddress(address) {
				return fmt.Errorf("%sarbitrage.execution.allowed_routers[%d] must be a 20-byte hex address", prefix, i)
			}
		}
		for i, address := range c.Arbitrage.Execution.AllowedSpenders {
			if !isStrictHexAddress(address) {
				return fmt.Errorf("%sarbitrage.execution.allowed_approval_spenders[%d] must be a 20-byte hex address", prefix, i)
			}
		}
	}
	return nil
}

func chainErrorPrefix(c ChainConfig) string {
	if c.ChainID == 0 {
		return ""
	}
	return fmt.Sprintf("chains[%d]: ", c.ChainID)
}

func isStrictHexAddress(address string) bool {
	address = strings.TrimSpace(address)
	hex := strings.TrimPrefix(address, "0x")
	hex = strings.TrimPrefix(hex, "0X")
	return len(hex) == 40 && common.IsHexAddress(address)
}

func (c Config) PersistenceConfig() persistence.Config {
	useMemory := c.Persistence.Memory
	if env := strings.TrimSpace(os.Getenv("USE_MEMORY_DB")); env != "" {
		useMemory = env == "true" || env == "1"
	}
	return persistence.Config{
		DatabaseURL: c.Persistence.Database.URL,
		UseMemory:   useMemory,
	}
}

// NormalizedChains returns per-chain runtime configs. Legacy single-chain YAML
// is represented as one chain using top-level fields.
func (c Config) NormalizedChains() []ChainConfig {
	if len(c.Chains) == 0 {
		return []ChainConfig{{
			Name:       normalizedChainName("", c.ChainID),
			ChainID:    c.ChainID,
			RPC:        c.RPC,
			Blockchain: c.Blockchain,
			Sync:       c.Sync,
			Quote:      c.Quote,
			Arbitrage:  c.Arbitrage,
		}}
	}
	chains := make([]ChainConfig, 0, len(c.Chains))
	for _, chain := range c.Chains {
		if !chain.IsEnabled() {
			continue
		}
		if chain.ChainID == 0 {
			chain.ChainID = c.ChainID
		}
		chain.Name = normalizedChainName(chain.Name, chain.ChainID)
		if chain.RPC.URL == "" {
			chain.RPC.URL = c.RPC.URL
		}
		if chain.RPC.WSURL == "" {
			chain.RPC.WSURL = c.RPC.WSURL
		}
		chain.Blockchain = mergeBlockchainConfig(c.Blockchain, chain.Blockchain)
		chain.Sync = mergeSyncConfig(c.Sync, chain.Sync)
		chain.Quote = mergeQuoteConfig(c.Quote, chain.Quote)
		chain.Arbitrage = mergeArbitrageConfig(c.Arbitrage, chain.Arbitrage)
		chains = append(chains, chain)
	}
	return chains
}

// RuntimeConfigForChain returns a Config view for one normalized chain.
func (c Config) RuntimeConfigForChain(chain ChainConfig) Config {
	runtime := c
	runtime.ChainID = chain.ChainID
	runtime.RPC = chain.RPC
	runtime.Blockchain = chain.Blockchain
	runtime.Sync = chain.Sync
	runtime.Quote = chain.Quote
	runtime.Arbitrage = chain.Arbitrage
	runtime.Chains = nil
	return runtime
}

func (c Config) PrimaryRuntimeConfig() Config {
	chains := c.NormalizedChains()
	if len(chains) == 0 {
		return c
	}
	return c.RuntimeConfigForChain(chains[0])
}

func (c Config) PrimaryChainName() string {
	chains := c.NormalizedChains()
	if len(chains) == 0 {
		return ""
	}
	return chains[0].Name
}

func normalizedChainName(name string, chainID uint64) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	if chainID == 0 {
		return ""
	}
	return fmt.Sprintf("chain-%d", chainID)
}

func mergeBlockchainConfig(base, override BlockchainConfig) BlockchainConfig {
	if override.FactoryAddress == "" {
		override.FactoryAddress = base.FactoryAddress
	}
	if override.MulticallAddress == "" {
		override.MulticallAddress = base.MulticallAddress
	}
	if override.PoolManagerAddress == "" {
		override.PoolManagerAddress = base.PoolManagerAddress
	}
	if override.StateViewAddress == "" {
		override.StateViewAddress = base.StateViewAddress
	}
	if override.BalancerVaultAddress == "" {
		override.BalancerVaultAddress = base.BalancerVaultAddress
	}
	if override.BalancerVaultV3Address == "" {
		override.BalancerVaultV3Address = base.BalancerVaultV3Address
	}
	return override
}

func mergeSyncConfig(base, override SyncConfig) SyncConfig {
	if override.CatchupBatchSize == 0 {
		override.CatchupBatchSize = base.CatchupBatchSize
	}
	if override.CatchupPoolGroupSize == 0 {
		override.CatchupPoolGroupSize = base.CatchupPoolGroupSize
	}
	if override.CatchupBlockSpan == 0 {
		override.CatchupBlockSpan = base.CatchupBlockSpan
	}
	if override.CatchupHeaderConcurrency == 0 {
		override.CatchupHeaderConcurrency = base.CatchupHeaderConcurrency
	}
	if override.BootstrapStaleBlockThreshold == 0 {
		override.BootstrapStaleBlockThreshold = base.BootstrapStaleBlockThreshold
	}
	if override.SnapshotInterval == 0 {
		override.SnapshotInterval = base.SnapshotInterval
	}
	if override.SnapshotFallback == 0 {
		override.SnapshotFallback = base.SnapshotFallback
	}
	if override.ReorgMaxDepth == 0 {
		override.ReorgMaxDepth = base.ReorgMaxDepth
	}
	return override
}

func mergeQuoteConfig(base, override QuoteConfig) QuoteConfig {
	if override.MaxHops == 0 {
		override.MaxHops = base.MaxHops
	}
	return override
}

func mergeArbitrageConfig(base, override ArbitrageConfig) ArbitrageConfig {
	override.FlashLoan = mergeFlashLoanConfig(base.FlashLoan, override.FlashLoan)
	override.Execution = mergeExecutionConfig(base.Execution, override.Execution)
	return override
}

func mergeFlashLoanConfig(base, override FlashLoanConfig) FlashLoanConfig {
	if override.BalancerFeePPM == "" {
		override.BalancerFeePPM = base.BalancerFeePPM
	}
	if override.Univ4FeePPM == "" {
		override.Univ4FeePPM = base.Univ4FeePPM
	}
	return override
}

func mergeExecutionConfig(base, override ExecutionConfig) ExecutionConfig {
	if override.RPCURL == "" {
		override.RPCURL = base.RPCURL
	}
	if override.ExecutorAddress == "" {
		override.ExecutorAddress = base.ExecutorAddress
	}
	if override.PrivateKey == "" {
		override.PrivateKey = base.PrivateKey
	}
	if override.FlashbotsRPCURL == "" {
		override.FlashbotsRPCURL = base.FlashbotsRPCURL
	}
	if override.FlashbotsPaymentBPS == 0 {
		override.FlashbotsPaymentBPS = base.FlashbotsPaymentBPS
	}
	if override.SettlementSlippageBPS == 0 {
		override.SettlementSlippageBPS = base.SettlementSlippageBPS
	}
	if override.GasLimit == 0 {
		override.GasLimit = base.GasLimit
	}
	if override.GasPriceWei == "" {
		override.GasPriceWei = base.GasPriceWei
	}
	if override.WETHAddress == "" {
		override.WETHAddress = base.WETHAddress
	}
	if override.SwapRouterV3 == "" {
		override.SwapRouterV3 = base.SwapRouterV3
	}
	if override.SwapRouterPancakeV3 == "" {
		override.SwapRouterPancakeV3 = base.SwapRouterPancakeV3
	}
	if override.UniversalRouter == "" {
		override.UniversalRouter = base.UniversalRouter
	}
	if override.BalancerRouterV3 == "" {
		override.BalancerRouterV3 = base.BalancerRouterV3
	}
	return override
}

func (c Config) MemoryMode() bool {
	return c.PersistenceConfig().UseMemory
}

func (c Config) DatabaseURL() string {
	return c.Persistence.Database.URL
}

func (c Config) BlockchainConfig() chaininfra.Config {
	factory := common.HexToAddress(c.Blockchain.FactoryAddress)
	if (factory == common.Address{}) {
		factory = common.HexToAddress(Default().Blockchain.FactoryAddress)
	}
	multicall := common.HexToAddress(c.Blockchain.MulticallAddress)
	if (multicall == common.Address{}) {
		multicall = common.HexToAddress(Default().Blockchain.MulticallAddress)
	}
	poolManager := common.HexToAddress(c.Blockchain.PoolManagerAddress)
	if (poolManager == common.Address{}) {
		poolManager = common.HexToAddress(Default().Blockchain.PoolManagerAddress)
	}
	stateView := common.HexToAddress(c.Blockchain.StateViewAddress)
	if (stateView == common.Address{}) {
		stateView = common.HexToAddress(Default().Blockchain.StateViewAddress)
	}
	balancerVault := common.HexToAddress(c.Blockchain.BalancerVaultAddress)
	if (balancerVault == common.Address{}) {
		balancerVault = common.HexToAddress(Default().Blockchain.BalancerVaultAddress)
	}
	balancerVaultV3 := common.HexToAddress(c.Blockchain.BalancerVaultV3Address)
	if (balancerVaultV3 == common.Address{}) {
		balancerVaultV3 = common.HexToAddress(Default().Blockchain.BalancerVaultV3Address)
	}
	return chaininfra.Config{
		RPCURL:                 c.RPC.URL,
		WSURL:                  c.RPC.WSURL,
		FactoryAddress:         factory,
		MulticallAddress:       multicall,
		PoolManagerAddress:     poolManager,
		StateViewAddress:       stateView,
		BalancerVaultAddress:   balancerVault,
		BalancerVaultV3Address: balancerVaultV3,
	}
}

func (c Config) SyncConfig() syncapp.Config {
	syncCfg := syncapp.DefaultConfig()
	if c.Sync.CatchupBatchSize > 0 {
		syncCfg.CatchupBatchSize = c.Sync.CatchupBatchSize
	}
	if c.Sync.CatchupPoolGroupSize > 0 {
		syncCfg.CatchupPoolGroupSize = c.Sync.CatchupPoolGroupSize
	}
	if c.Sync.CatchupBlockSpan > 0 {
		syncCfg.CatchupBlockSpan = c.Sync.CatchupBlockSpan
	}
	if c.Sync.CatchupHeaderConcurrency > 0 {
		syncCfg.CatchupHeaderConcurrency = c.Sync.CatchupHeaderConcurrency
	}
	if c.Sync.BootstrapStaleBlockThreshold > 0 {
		syncCfg.BootstrapStaleBlockThreshold = c.Sync.BootstrapStaleBlockThreshold
	}
	if c.Sync.SnapshotInterval > 0 {
		syncCfg.SnapshotInterval = c.Sync.SnapshotInterval
	}
	if c.Sync.SnapshotFallback > 0 {
		syncCfg.SnapshotFallback = c.Sync.SnapshotFallback
	}
	if c.Sync.ReorgMaxDepth > 0 {
		syncCfg.ReorgMaxDepth = c.Sync.ReorgMaxDepth
	}
	return syncCfg
}

func (c Config) StaticPoolAddresses() []common.Address {
	addresses := make([]common.Address, 0, len(c.Sync.Univ3.Pools))
	for _, pool := range c.Sync.Univ3.Pools {
		if pool.Address == "" {
			continue
		}
		addresses = append(addresses, common.HexToAddress(pool.Address))
	}
	return addresses
}

// PoolAddresses returns statically configured V3 pool addresses.
func (c Config) PoolAddresses() []common.Address {
	return c.StaticPoolAddresses()
}

func (c Config) SubgraphPoolSource() SubgraphPoolConfig {
	return c.Sync.Univ3.Subgraph
}

func (c SubgraphPoolConfig) IsEnabled() bool {
	return c.Enabled && c.Endpoint != ""
}

func (c Univ3SyncConfig) IsActive() bool {
	if !c.Enabled {
		return false
	}
	return len(c.Pools) > 0 || c.Subgraph.IsEnabled()
}

func (c PancakeV3SyncConfig) IsActive() bool {
	if !c.Enabled {
		return false
	}
	return len(c.Pools) > 0 || c.Subgraph.IsEnabled()
}

func (c QuickSwapV3SyncConfig) IsActive() bool {
	if !c.Enabled {
		return false
	}
	return len(c.Pools) > 0 || c.Subgraph.IsEnabled()
}

func (c Univ4SyncConfig) IsActive() bool {
	if !c.Enabled {
		return false
	}
	return len(c.PoolManager.Pools) > 0 || c.Subgraph.IsEnabled()
}

func (c BalancerSyncConfig) IsActive() bool {
	if !c.Enabled {
		return false
	}
	return len(c.Pools) > 0 || c.Subgraph.IsEnabled()
}

func (c Config) TriangleArbitrageEnabled() bool {
	return c.Arbitrage.Triangle.Enabled
}

func (c Config) SpreadArbitrageEnabled() bool {
	return c.Arbitrage.Spread.Enabled
}

func (c Config) ArbitrageEnabled() bool {
	return c.TriangleArbitrageEnabled() || c.SpreadArbitrageEnabled()
}

func (c ExecutionConfig) Executor() common.Address {
	return common.HexToAddress(c.ExecutorAddress)
}

func (c ExecutionConfig) WETH() common.Address {
	if strings.TrimSpace(c.WETHAddress) == "" {
		return common.Address{}
	}
	return common.HexToAddress(c.WETHAddress)
}

func (c ExecutionConfig) SwapRouterV3Address() common.Address {
	if strings.TrimSpace(c.SwapRouterV3) == "" {
		return common.Address{}
	}
	return common.HexToAddress(c.SwapRouterV3)
}

func (c ExecutionConfig) SwapRouterPancakeV3Address() common.Address {
	if strings.TrimSpace(c.SwapRouterPancakeV3) == "" {
		return common.Address{}
	}
	return common.HexToAddress(c.SwapRouterPancakeV3)
}

func (c ExecutionConfig) UniversalRouterAddress() common.Address {
	if strings.TrimSpace(c.UniversalRouter) == "" {
		return common.Address{}
	}
	return common.HexToAddress(c.UniversalRouter)
}

func (c ExecutionConfig) BalancerRouterV3Address() common.Address {
	if strings.TrimSpace(c.BalancerRouterV3) == "" {
		return common.Address{}
	}
	return common.HexToAddress(c.BalancerRouterV3)
}

// ResolveRPCURL returns the dedicated execution RPC when set, otherwise fallback.
func (c ExecutionConfig) ResolveRPCURL(fallback string) string {
	if url := strings.TrimSpace(c.RPCURL); url != "" {
		return url
	}
	return strings.TrimSpace(fallback)
}

func (c ExecutionConfig) GasPrice() *big.Int {
	return parseConfigBigInt(c.GasPriceWei, nil)
}

func (c ExecutionConfig) AllowedRouterAddresses() []common.Address {
	return hexAddresses(c.AllowedRouters)
}

func (c ExecutionConfig) AllowedSpenderAddresses() []common.Address {
	return hexAddresses(c.AllowedSpenders)
}

func hexAddresses(values []string) []common.Address {
	addresses := make([]common.Address, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		addresses = append(addresses, common.HexToAddress(value))
	}
	return addresses
}

func (c FlashLoanConfig) BalancerFee() *big.Int {
	return parseConfigBigInt(c.BalancerFeePPM, big.NewInt(0))
}

func (c FlashLoanConfig) Univ4Fee() *big.Int {
	return parseConfigBigInt(c.Univ4FeePPM, big.NewInt(0))
}

func (c SpreadArbitrageConfig) StartTokenAddresses() []common.Address {
	addresses := make([]common.Address, 0, len(c.StartTokens))
	for _, token := range c.StartTokens {
		if token == "" {
			continue
		}
		addresses = append(addresses, common.HexToAddress(token))
	}
	return addresses
}

func (c SpreadArbitrageConfig) MinNetProfit() *big.Int {
	return parseConfigBigInt(c.MinNetProfitWei, big.NewInt(1))
}

func (c SpreadArbitrageConfig) OptimizerMinAmount() *big.Int {
	return parseConfigBigInt(c.MinAmount, big.NewInt(1_000_000))
}

func (c SpreadArbitrageConfig) OptimizerMaxAmount() *big.Int {
	return parseConfigBigInt(c.MaxAmount, big.NewInt(100_000_000_000_000))
}

func (c TriangleArbitrageConfig) StartTokenAddresses() []common.Address {
	addresses := make([]common.Address, 0, len(c.StartTokens))
	for _, token := range c.StartTokens {
		if token == "" {
			continue
		}
		addresses = append(addresses, common.HexToAddress(token))
	}
	return addresses
}

func parseConfigBigInt(value string, fallback *big.Int) *big.Int {
	value = strings.TrimSpace(value)
	if value == "" {
		if fallback == nil {
			return nil
		}
		return new(big.Int).Set(fallback)
	}
	parsed, ok := new(big.Int).SetString(value, 10)
	if !ok {
		if fallback == nil {
			return nil
		}
		return new(big.Int).Set(fallback)
	}
	return parsed
}

func (c TriangleArbitrageConfig) MinNetProfit() *big.Int {
	return parseConfigBigInt(c.MinNetProfitWei, big.NewInt(1))
}

func (c TriangleArbitrageConfig) OptimizerMinAmount() *big.Int {
	return parseConfigBigInt(c.MinAmount, big.NewInt(1_000_000))
}

func (c TriangleArbitrageConfig) OptimizerMaxAmount() *big.Int {
	return parseConfigBigInt(c.MaxAmount, big.NewInt(100_000_000_000_000))
}
