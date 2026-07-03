package config

import (
	"fmt"
	"os"
	"time"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	chaininfra "github.com/brianliu-sysu/uniswapv3/internal/infrastructure/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence"
	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"
)

// Config is the root application configuration loaded from YAML.
type Config struct {
	App        AppConfig        `yaml:"app"`
	RPC        RPCConfig        `yaml:"rpc"`
	Database   DatabaseConfig   `yaml:"database"`
	Redis      RedisConfig      `yaml:"redis"`
	Blockchain BlockchainConfig `yaml:"blockchain"`
	Sync       SyncConfig       `yaml:"sync"`
	Pools      PoolsConfig      `yaml:"pools"`
	HTTP       HTTPConfig       `yaml:"http"`
	Quote      QuoteConfig      `yaml:"quote"`
	Log        LogConfig        `yaml:"log"`
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

type RPCConfig struct {
	URL    string `yaml:"url"`
	WSURL  string `yaml:"ws_url"`
}

type DatabaseConfig struct {
	URL string `yaml:"url"`
}

type RedisConfig struct {
	URL string `yaml:"url"`
}

type BlockchainConfig struct {
	FactoryAddress   string `yaml:"factory_address"`
	MulticallAddress string `yaml:"multicall_address"`
}

type SyncConfig struct {
	CatchupBatchSize uint64        `yaml:"catchup_batch_size"`
	SnapshotInterval uint64        `yaml:"snapshot_interval"`
	SnapshotFallback time.Duration `yaml:"snapshot_fallback"`
	ReorgMaxDepth    uint64        `yaml:"reorg_max_depth"`
}

type PoolsConfig struct {
	Static   []StaticPoolConfig `yaml:"static"`
	Subgraph SubgraphPoolConfig `yaml:"subgraph"`
}

// StaticPoolConfig defines a pool tracked by explicit on-chain address.
type StaticPoolConfig struct {
	Address string `yaml:"address"`
	Fee     uint32 `yaml:"fee"`
}

// SubgraphPoolConfig discovers pools from a Uniswap V3 subgraph.
type SubgraphPoolConfig struct {
	Enabled                bool          `yaml:"enabled"`
	Endpoint               string        `yaml:"endpoint"`
	RefreshInterval        time.Duration `yaml:"refresh_interval"`
	First                  int           `yaml:"first"`
	Skip                   int           `yaml:"skip"`
	OrderBy                string        `yaml:"order_by"`
	OrderDirection         string        `yaml:"order_direction"`
	MinTotalValueLockedUSD string        `yaml:"min_total_value_locked_usd"`
	Token0                 string        `yaml:"token0"`
	Token1                 string        `yaml:"token1"`
	FeeTiers               []uint32      `yaml:"fee_tiers"`
}

type PoolConfig = StaticPoolConfig

type LogConfig struct {
	Level string `yaml:"level"`
}

func Default() Config {
	return Config{
		App: AppConfig{Name: "univ3-arbitrage"},
		Blockchain: BlockchainConfig{
			FactoryAddress:   "0x1F98431c8aD98523631AE4a59f267346ea31F984",
			MulticallAddress: "0xcA11bde05977b3631167028862bE2a173976CA11",
		},
		Sync: SyncConfig{
			CatchupBatchSize: 2000,
			SnapshotInterval: 5000,
			SnapshotFallback: 10 * time.Minute,
			ReorgMaxDepth:    128,
		},
		Pools: PoolsConfig{
			Subgraph: SubgraphPoolConfig{
				First:          100,
				OrderBy:        "totalValueLockedUSD",
				OrderDirection: "desc",
				RefreshInterval: 10 * time.Minute,
			},
		},
		HTTP: HTTPConfig{
			Enabled: true,
			Addr:    ":8080",
		},
		Quote: QuoteConfig{
			MaxHops: 3,
		},
		Log: LogConfig{Level: "info"},
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
	if c.Database.URL == "" {
		return fmt.Errorf("database.url is required")
	}
	if c.Pools.Subgraph.Enabled && c.Pools.Subgraph.Endpoint == "" {
		return fmt.Errorf("pools.subgraph.endpoint is required when subgraph is enabled")
	}
	for i, pool := range c.Pools.Static {
		if pool.Address == "" {
			return fmt.Errorf("pools.static[%d].address is required", i)
		}
	}
	return nil
}

func (c Config) PersistenceConfig() persistence.Config {
	return persistence.Config{
		DatabaseURL: c.Database.URL,
		UseMemory:   false,
	}
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
	return chaininfra.Config{
		RPCURL:           c.RPC.URL,
		WSURL:            c.RPC.WSURL,
		FactoryAddress:   factory,
		MulticallAddress: multicall,
	}
}

func (c Config) SyncConfig() syncapp.Config {
	syncCfg := syncapp.DefaultConfig()
	if c.Sync.CatchupBatchSize > 0 {
		syncCfg.CatchupBatchSize = c.Sync.CatchupBatchSize
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
	addresses := make([]common.Address, 0, len(c.Pools.Static))
	for _, pool := range c.Pools.Static {
		if pool.Address == "" {
			continue
		}
		addresses = append(addresses, common.HexToAddress(pool.Address))
	}
	return addresses
}

// PoolAddresses returns statically configured pool addresses.
func (c Config) PoolAddresses() []common.Address {
	return c.StaticPoolAddresses()
}

func (c Config) SubgraphPoolSource() SubgraphPoolConfig {
	return c.Pools.Subgraph
}

func (c SubgraphPoolConfig) IsEnabled() bool {
	return c.Enabled && c.Endpoint != ""
}
