package bootstrap

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/fx"

	"github.com/brianliu-sysu/arbitrage/internal/blockchain"
	"github.com/brianliu-sysu/arbitrage/internal/cache"
	"github.com/brianliu-sysu/arbitrage/internal/config"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/brianliu-sysu/arbitrage/internal/pool/snapshot"
	"github.com/brianliu-sysu/arbitrage/internal/service"
	"github.com/brianliu-sysu/arbitrage/internal/storage"
	"github.com/brianliu-sysu/arbitrage/internal/storage/postgres"
	"github.com/brianliu-sysu/arbitrage/internal/store"
)

type chainBootstrapper struct {
	cfg        *config.AppConfig
	logger     logx.Logger
	st         store.Storer
	repos      *postgres.Repositories
	poolCache  *pool.Cache
	tokenCache cache.TokenCache
	logCache   cache.AppliedLogCache
	multiChain *service.MultiChainService
	registry   *blockchain.ProcessorRegistry

	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgWG     sync.WaitGroup
}

type chainBootstrapParams struct {
	fx.In
	Lifecycle  fx.Lifecycle
	Config     *config.AppConfig
	Logger     logx.Logger
	Store      store.Storer
	Repos      *postgres.Repositories
	PoolCache  *pool.Cache
	TokenCache cache.TokenCache
	LogCache   cache.AppliedLogCache
	MultiChain *service.MultiChainService
}

// RegisterChains 在应用启动时创建全部链服务、恢复快照并启动 BlockSync。
func RegisterChains(p chainBootstrapParams) {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	b := &chainBootstrapper{
		cfg:        p.Config,
		logger:     p.Logger,
		st:         p.Store,
		repos:      p.Repos,
		poolCache:  p.PoolCache,
		tokenCache: p.TokenCache,
		logCache:   p.LogCache,
		multiChain: p.MultiChain,
		registry:   blockchain.NewProcessorRegistry(),
		bgCtx:      bgCtx,
		bgCancel:   bgCancel,
	}

	p.Lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			return b.setupAllChains()
		},
		OnStop: func(context.Context) error {
			b.bgCancel()
			b.bgWG.Wait()
			return nil
		},
	})
}

func (b *chainBootstrapper) setupAllChains() error {
	for _, ch := range b.cfg.GetChains() {
		if err := b.setupSingleChain(ch); err != nil {
			return err
		}
	}
	return nil
}

func (b *chainBootstrapper) setupSingleChain(ch config.ChainConfig) error {
	baseTokens := make([]common.Address, len(ch.BaseTokens))
	for i, t := range ch.BaseTokens {
		baseTokens[i] = common.HexToAddress(t)
	}

	maxHops := ch.MaxHops
	if maxHops == 0 {
		maxHops = b.cfg.MaxHops
	}

	svc := service.NewMultiPoolService(
		ch.Name, ch.WSEndpoint, ch.RPCEndpoint, maxHops, baseTokens,
		b.cfg.MaxBlockGapForFullSync,
		common.HexToAddress(ch.FactoryAddress),
		common.HexToAddress(ch.GetMulticallAddress()),
		common.HexToAddress(ch.GetQuoterAddress()),
		b.logger, b.st, b.tokenCache, b.logCache, b.poolCache,
	)
	if err := b.multiChain.AddChain(ch.Name, svc); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	loader := snapshot.NewLoader(b.repos.Pool)
	preloaded, err := loader.RestoreAll(ctx, ch.Name)
	if err != nil {
		b.logger.Warn("failed to load chain pools from DB, continue with config pools",
			"chain", ch.Name, "error", err)
	}

	poolEntries := buildPoolEntries(b.cfg.HealthCheckIntervalSec, preloaded, ch.Pools)
	if err := svc.AddPoolsBatch(poolEntries); err != nil {
		return fmt.Errorf("add pools to chain %s: %w", ch.Name, err)
	}

	b.startBlockSync(ch, svc)

	if ad := ch.GetAutoDiscover(); ad.Enabled {
		b.bgWG.Add(1)
		go func() {
			defer b.bgWG.Done()
			select {
			case <-b.bgCtx.Done():
				return
			default:
			}
			added := svc.AutoDiscoverPools(ad.SubgraphURL, ad.OrderBy, ad.MinTVLUSD, ad.MinVolumeUSD, ad.MaxPools)
			b.logger.Info("auto-discover finished", "chain", ch.Name, "added", added)
		}()
	}

	return nil
}

func buildPoolEntries(healthSec int, preloaded map[string]*storage.PoolSnapshot, configPools []config.PoolConfig) []service.PoolEntry {
	poolEntries := make([]service.PoolEntry, 0, len(preloaded)+len(configPools))
	seen := make(map[common.Address]struct{})

	for addrStr, snap := range preloaded {
		addr := common.HexToAddress(addrStr)
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		poolEntries = append(poolEntries, service.PoolEntry{
			PoolAddress:            addr,
			HealthCheckIntervalSec: healthSec,
			SyncFromBlock:          snap.BlockNumber,
			PoolSnapshot:           toStoreSnapshot(snap),
		})
	}
	for _, pc := range configPools {
		addr := common.HexToAddress(pc.PoolAddress)
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		poolEntries = append(poolEntries, service.PoolEntry{
			PoolAddress:            addr,
			HealthCheckIntervalSec: healthSec,
			SyncFromBlock:          pc.SyncFromBlock,
		})
	}
	return poolEntries
}

func toStoreSnapshot(s *storage.PoolSnapshot) *store.PoolSnapshot {
	if s == nil {
		return nil
	}
	tickData := make(map[int32]store.TickLiquiditySnapshot, len(s.TickData))
	for tick, t := range s.TickData {
		tickData[tick] = store.TickLiquiditySnapshot{
			LiquidityNet:   copyBigInt(t.LiquidityNet),
			LiquidityGross: copyBigInt(t.LiquidityGross),
		}
	}
	return &store.PoolSnapshot{
		ChainName:    s.ChainName,
		PoolAddress:  s.PoolAddress,
		BlockNumber:  s.BlockNumber,
		Tick:         s.Tick,
		SqrtPriceX96: copyBigInt(s.SqrtPriceX96),
		Liquidity:    copyBigInt(s.Liquidity),
		Price0In1:    s.Price0In1,
		Token0Symbol: s.Token0Symbol,
		Token1Symbol: s.Token1Symbol,
		Fee:          s.Fee,
		TickData:     tickData,
	}
}

func copyBigInt(v *big.Int) *big.Int {
	if v == nil {
		return nil
	}
	return new(big.Int).Set(v)
}

func (b *chainBootstrapper) startBlockSync(ch config.ChainConfig, svc *service.MultiPoolService) {
	rpcClient := blockchain.NewClient(ch.RPCEndpoint)
	fetcher := blockchain.NewLogFetcher(rpcClient)

	processor, err := b.registry.BuildComposite(ch.Name, []blockchain.ProtocolID{
		blockchain.ProtocolUniswapV3,
	}, blockchain.ProcessorBuildParams{
		ChainName: ch.Name,
		Cache:     svc.PoolCache(),
		Fetcher:   fetcher,
		PoolRepo:  b.repos.Pool,
		SyncRepo:  b.repos.Sync,
		Logger:    b.logger,
	})
	if err != nil {
		b.logger.Error("build block processor failed", "chain", ch.Name, "error", err)
		return
	}

	if u, ok := processor.(blockchain.PoolAddressUpdater); ok {
		svc.SetPoolUpdater(u)
	}

	b.bgWG.Add(1)
	go func() {
		defer b.bgWG.Done()

		ctx, cancel := context.WithCancel(b.bgCtx)
		defer cancel()

		headCtx, headCancel := context.WithTimeout(ctx, 15*time.Second)
		currentBlock, err := blockchain.RPCBlockNumber(headCtx, rpcClient)
		headCancel()
		if err != nil {
			b.logger.Warn("block sync: fetch head failed", "chain", ch.Name, "error", err)
			return
		}

		startBlock := b.resolveCatchUpStart(ch.Name, svc)
		blockSync := blockchain.NewBlockSync(ch.Name, nil, processor, b.repos.Sync, b.logger)

		if startBlock < currentBlock {
			catchUpCtx, catchUpCancel := context.WithTimeout(ctx, 10*time.Minute)
			if err := blockSync.CatchUpFrom(catchUpCtx, startBlock+1, currentBlock); err != nil {
				b.logger.Warn("block sync catch-up failed", "chain", ch.Name, "error", err)
			} else {
				b.logger.Info("block sync catch-up done", "chain", ch.Name, "from", startBlock+1, "to", currentBlock)
			}
			catchUpCancel()
		}

		wsSub := blockchain.NewWSHeaderSubscriber(ch.WSEndpoint, b.logger)
		blockSync = blockchain.NewBlockSync(ch.Name, wsSub, processor, b.repos.Sync, b.logger)
		b.logger.Info("block sync live loop started", "chain", ch.Name)
		if err := blockSync.Run(ctx); err != nil && ctx.Err() == nil {
			b.logger.Error("block sync stopped", "chain", ch.Name, "error", err)
		}
	}()
}

func (b *chainBootstrapper) resolveCatchUpStart(chainName string, svc *service.MultiPoolService) uint64 {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lastSync, err := b.repos.Sync.GetLastProcessedBlock(ctx, chainName)
	if err != nil {
		b.logger.Warn("read sync state failed", "chain", chainName, "error", err)
	}

	var maxPoolBlock uint64
	for _, addr := range svc.TrackedPoolAddresses() {
		if st, ok := svc.PoolCache().Get(addr); ok && st.BlockNumber > maxPoolBlock {
			maxPoolBlock = st.BlockNumber
		}
	}
	if maxPoolBlock > lastSync {
		return maxPoolBlock
	}
	return lastSync
}
