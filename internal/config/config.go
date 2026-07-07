package config

import (
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	chaininfra "github.com/brianliu-sysu/uniswapv3/internal/infrastructure/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence"
	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"
)

// Config is the root application configuration loaded from YAML.
type Config struct {
	App         AppConfig         `yaml:"app"`
	RPC         RPCConfig         `yaml:"rpc"`
	Persistence PersistenceConfig `yaml:"persistence"`
	Blockchain  BlockchainConfig  `yaml:"blockchain"`
	Sync        SyncConfig        `yaml:"sync"`
	HTTP        HTTPConfig        `yaml:"http"`
	Quote       QuoteConfig       `yaml:"quote"`
	Arbitrage   ArbitrageConfig   `yaml:"arbitrage"`
	Log         LogConfig         `yaml:"log"`
}

type AppConfig struct {
	Name string `yaml:"name"`
}

type HTTPConfig struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr"`
}

type QuoteConfig struct {
	MaxHops int `yaml:"max_hops"`
}

type ArbitrageConfig struct {
	Triangle TriangleArbitrageConfig `yaml:"triangle"`
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
	FactoryAddress          string `yaml:"factory_address"`
	MulticallAddress        string `yaml:"multicall_address"`
	PoolManagerAddress      string `yaml:"pool_manager_address"`
	StateViewAddress        string `yaml:"state_view_address"`
	BalancerVaultAddress    string `yaml:"balancer_vault_address"`
	BalancerVaultV3Address  string `yaml:"balancer_vault_v3_address"`
}

type SyncConfig struct {
	CatchupBatchSize             uint64              `yaml:"catchup_batch_size"`
	CatchupPoolGroupSize         uint64              `yaml:"catchup_pool_group_size"`
	CatchupBlockSpan             uint64              `yaml:"catchup_block_span"`
	CatchupHeaderConcurrency     int                 `yaml:"catchup_header_concurrency"`
	BootstrapStaleBlockThreshold uint64              `yaml:"bootstrap_stale_block_threshold"`
	SnapshotInterval             uint64              `yaml:"snapshot_interval"`
	SnapshotFallback             time.Duration       `yaml:"snapshot_fallback"`
	ReorgMaxDepth                uint64              `yaml:"reorg_max_depth"`
	Univ3                        Univ3SyncConfig     `yaml:"univ3"`
	PancakeV3                    PancakeV3SyncConfig `yaml:"pancakev3"`
	Univ4                        Univ4SyncConfig     `yaml:"univ4"`
	Balancer                     BalancerSyncConfig  `yaml:"balancer"`
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
		App: AppConfig{Name: "univ3-arbitrage"},
		Blockchain: BlockchainConfig{
			FactoryAddress:       "0x1F98431c8aD98523631AE4a59f267346ea31F984",
			MulticallAddress:     "0xcA11bde05977b3631167028862bE2a173976CA11",
			PoolManagerAddress:   "0x000000000004444c5dc75cB358380D2e3dE08A90",
			StateViewAddress:     "0x7ffe42c4a5deea5b0fec41c94c136cf115597227",
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
				OptimizerIterations: 16,
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
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config file: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if !c.Persistence.Memory && c.Persistence.Database.URL == "" {
		return fmt.Errorf("persistence.database.url is required unless persistence.memory is enabled")
	}
	if c.Sync.Univ3.Subgraph.Enabled && c.Sync.Univ3.Subgraph.Endpoint == "" {
		return fmt.Errorf("sync.univ3.subgraph.endpoint is required when subgraph is enabled")
	}
	if c.Sync.PancakeV3.Subgraph.Enabled && c.Sync.PancakeV3.Subgraph.Endpoint == "" {
		return fmt.Errorf("sync.pancakev3.subgraph.endpoint is required when subgraph is enabled")
	}
	if c.Sync.Univ4.Subgraph.Enabled && c.Sync.Univ4.Subgraph.Endpoint == "" {
		return fmt.Errorf("sync.univ4.subgraph.endpoint is required when subgraph is enabled")
	}
	if c.Sync.Balancer.Subgraph.Enabled && c.Sync.Balancer.Subgraph.Endpoint == "" {
		return fmt.Errorf("sync.balancer.subgraph.endpoint is required when subgraph is enabled")
	}
	for i, pool := range c.Sync.Univ3.Pools {
		if pool.Address == "" {
			return fmt.Errorf("sync.univ3.pools[%d].address is required", i)
		}
	}
	for i, pool := range c.Sync.PancakeV3.Pools {
		if pool.Address == "" {
			return fmt.Errorf("sync.pancakev3.pools[%d].address is required", i)
		}
	}
	for i, pool := range c.Sync.Univ4.PoolManager.Pools {
		if pool.Currency0 == "" || pool.Currency1 == "" {
			return fmt.Errorf("sync.univ4.poolmanager.pools[%d] requires currency0 and currency1", i)
		}
	}
	for i, pool := range c.Sync.Balancer.Pools {
		if pool.ID == "" || pool.Address == "" {
			return fmt.Errorf("sync.balancer.pools[%d] requires id and address", i)
		}
		poolType := strings.ToLower(strings.TrimSpace(pool.Type))
		if poolType != "weighted" && poolType != "stable" {
			return fmt.Errorf("sync.balancer.pools[%d].type must be weighted or stable", i)
		}
	}
	if c.Sync.Univ4.IsActive() {
		if c.Blockchain.PoolManagerAddress == "" {
			return fmt.Errorf("blockchain.pool_manager_address is required when sync.univ4 is enabled")
		}
		if !isStrictHexAddress(c.Blockchain.PoolManagerAddress) {
			return fmt.Errorf("blockchain.pool_manager_address must be a 20-byte hex address")
		}
		if c.Blockchain.StateViewAddress == "" {
			return fmt.Errorf("blockchain.state_view_address is required when sync.univ4 is enabled")
		}
		if !isStrictHexAddress(c.Blockchain.StateViewAddress) {
			return fmt.Errorf("blockchain.state_view_address must be a 20-byte hex address")
		}
	}
	if c.Sync.Balancer.IsActive() {
		if c.Blockchain.BalancerVaultAddress == "" {
			return fmt.Errorf("blockchain.balancer_vault_address is required when sync.balancer is enabled")
		}
		if !isStrictHexAddress(c.Blockchain.BalancerVaultAddress) {
			return fmt.Errorf("blockchain.balancer_vault_address must be a 20-byte hex address")
		}
	}
	return nil
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
		return new(big.Int).Set(fallback)
	}
	parsed, ok := new(big.Int).SetString(value, 10)
	if !ok {
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
