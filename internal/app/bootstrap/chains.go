package bootstrap

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/fx"

	"github.com/brianliu-sysu/arbitrage/internal/cache"
	"github.com/brianliu-sysu/arbitrage/internal/config"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/service"
	"github.com/brianliu-sysu/arbitrage/internal/store"
)

type chainBootstrapper struct {
	cfg        *config.AppConfig
	logger     logx.Logger
	st         store.Storer
	tokenCache cache.TokenCache
	logCache   cache.AppliedLogCache
	multiChain *service.MultiChainService

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
	TokenCache cache.TokenCache
	LogCache   cache.AppliedLogCache
	MultiChain *service.MultiChainService
}

// RegisterChains 在应用启动时创建全部链服务并加载池子。
func RegisterChains(p chainBootstrapParams) {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	b := &chainBootstrapper{
		cfg:        p.Config,
		logger:     p.Logger,
		st:         p.Store,
		tokenCache: p.TokenCache,
		logCache:   p.LogCache,
		multiChain: p.MultiChain,
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
	chains := b.cfg.GetChains()
	for _, ch := range chains {
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
		b.logger, b.st, b.tokenCache, b.logCache,
	)
	if err := b.multiChain.AddChain(ch.Name, svc); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	preloaded, err := b.st.LoadAll(ctx, ch.Name)
	if err != nil {
		b.logger.Warn("failed to load chain pools from DB, continue with config pools",
			"chain", ch.Name, "error", err)
	}

	poolEntries := make([]service.PoolEntry, 0, len(preloaded)+len(ch.Pools))
	seen := make(map[common.Address]struct{})
	for addrStr, snap := range preloaded {
		addr := common.HexToAddress(addrStr)
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		poolEntries = append(poolEntries, service.PoolEntry{
			PoolAddress:            addr,
			HealthCheckIntervalSec: b.cfg.HealthCheckIntervalSec,
			SyncFromBlock:          snap.BlockNumber,
			PoolSnapshot:           snap,
		})
	}
	for _, pc := range ch.Pools {
		addr := common.HexToAddress(pc.PoolAddress)
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		poolEntries = append(poolEntries, service.PoolEntry{
			PoolAddress:            addr,
			HealthCheckIntervalSec: b.cfg.HealthCheckIntervalSec,
			SyncFromBlock:          pc.SyncFromBlock,
		})
	}

	if err := svc.AddPoolsBatch(poolEntries); err != nil {
		return fmt.Errorf("add pools to chain %s: %w", ch.Name, err)
	}

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
