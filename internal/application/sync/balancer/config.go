package balancersync

import (
	"context"
	"math/big"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

type Config = syncapp.Config

func DefaultConfig() Config {
	return syncapp.DefaultConfig()
}

type RawLog = syncapp.RawLog
type BlockReader = syncapp.BlockReader
type HeadSubscriber = syncapp.HeadSubscriber
type HealthProbe = syncapp.HealthProbe

// LogFilter selects Vault/pool logs for tracked Balancer pools within a block range.
type LogFilter struct {
	PoolIDs       []marketbalancer.PoolID
	PoolAddresses []common.Address
	FromBlock     uint64
	ToBlock       uint64
}

// BootstrapData is on-chain Balancer pool state read during cold bootstrap.
type BootstrapData struct {
	Spec              marketbalancer.PoolSpec
	Tokens            []common.Address
	Balances          map[common.Address]*big.Int
	Weights           map[common.Address]*big.Int
	Amplification     *big.Int
	SwapFeePercentage *big.Int
	BlockNumber       uint64
}

// LogFetcher fetches raw Vault/pool logs from the chain.
type LogFetcher interface {
	FetchLogs(ctx context.Context, filter LogFilter) ([]RawLog, error)
}

// EventParser converts raw logs into Balancer domain pool events.
type EventParser interface {
	ParsePoolEvents(logs []RawLog) ([]marketbalancer.PoolEvent, error)
}

// PoolAddressBinder lets parsers resolve pool-contract logs back to Vault PoolIDs.
type PoolAddressBinder interface {
	SetPoolAddressMap(map[common.Address]marketbalancer.PoolID)
}

// PoolBootstrapReader reads live Balancer pool state from the chain.
type PoolBootstrapReader interface {
	ReadBootstrapData(ctx context.Context, poolID marketbalancer.PoolID, spec marketbalancer.PoolSpec, blockNumber uint64) (*BootstrapData, error)
}

// ChangedPoolsListener receives pools updated after a block is applied.
type ChangedPoolsListener interface {
	OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []marketbalancer.PoolID) error
}

// NopChangedPoolsListener ignores pool change notifications.
type NopChangedPoolsListener struct{}

func (NopChangedPoolsListener) OnPoolsChanged(context.Context, uint64, []marketbalancer.PoolID) error {
	return nil
}

// ServiceDeps contains external dependencies required to construct Balancer sync services.
type ServiceDeps struct {
	Config      Config
	Pools       marketbalancer.PoolRepository
	Checkpoints blockchain.BalancerCheckpointRepository
	Snapshots   marketbalancer.SnapshotRepository
	Registry    marketbalancer.PoolRegistry
	Fetcher     LogFetcher
	Parser      EventParser
	Blocks      BlockReader
	Bootstrap   PoolBootstrapReader
	Subscriber  HeadSubscriber
	Health      []HealthProbe
	Listener    ChangedPoolsListener
}
