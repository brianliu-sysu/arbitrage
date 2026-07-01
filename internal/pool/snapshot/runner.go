package snapshot

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/brianliu-sysu/arbitrage/internal/config"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/brianliu-sysu/arbitrage/internal/service"
	"github.com/brianliu-sysu/arbitrage/internal/storage"
	"github.com/brianliu-sysu/arbitrage/internal/subgraph"
	"github.com/ethereum/go-ethereum/common"
)

// PoolTarget 待快照的池子。
type PoolTarget struct {
	Address       common.Address
	SyncFromBlock uint64
}

// Runner 一次性快照任务：从配置收集池子、链上全量同步并写入 pool_states。
type Runner struct {
	cfg    *config.AppConfig
	repo   storage.PoolRepo
	logger logx.Logger
}

// NewRunner 创建快照运行器。
func NewRunner(cfg *config.AppConfig, repo storage.PoolRepo, logger logx.Logger) *Runner {
	return &Runner{cfg: cfg, repo: repo, logger: logger}
}

// Run 对配置中全部链（或指定链）执行快照。
func (r *Runner) Run(ctx context.Context, chainFilter string) error {
	if r.repo == nil {
		return fmt.Errorf("database is required for snapshot (set db_url in config)")
	}
	for _, ch := range r.cfg.GetChains() {
		if chainFilter != "" && ch.Name != chainFilter {
			continue
		}
		if err := r.runChain(ctx, ch); err != nil {
			return fmt.Errorf("chain %s: %w", ch.Name, err)
		}
	}
	return nil
}

func (r *Runner) runChain(ctx context.Context, ch config.ChainConfig) error {
	targets, err := CollectPoolTargets(ch)
	if err != nil {
		return err
	}
	r.logger.Info("snapshot chain start", "chain", ch.Name, "pools", len(targets))

	var failed int
	for _, t := range targets {
		if err := r.snapshotPool(ctx, ch, t); err != nil {
			failed++
			r.logger.Error("snapshot pool failed", "chain", ch.Name, "pool", t.Address.Hex(), "error", err)
		}
	}
	r.logger.Info("snapshot chain done", "chain", ch.Name, "total", len(targets), "failed", failed)
	if failed > 0 {
		return fmt.Errorf("%d pool(s) failed", failed)
	}
	return nil
}

// CollectPoolTargets 合并配置 pools 与 auto_discover 结果。
func CollectPoolTargets(ch config.ChainConfig) ([]PoolTarget, error) {
	seen := make(map[common.Address]struct{})
	var targets []PoolTarget

	add := func(addr common.Address, syncFrom uint64) {
		if addr == (common.Address{}) {
			return
		}
		if _, ok := seen[addr]; ok {
			return
		}
		seen[addr] = struct{}{}
		targets = append(targets, PoolTarget{Address: addr, SyncFromBlock: syncFrom})
	}

	for _, pc := range ch.Pools {
		add(common.HexToAddress(pc.PoolAddress), pc.SyncFromBlock)
	}

	ad := ch.GetAutoDiscover()
	if ad.Enabled {
		client := subgraph.NewClient(ad.SubgraphURL)
		pools, err := client.FetchTopPools(ad.OrderBy, ad.MinTVLUSD, ad.MinVolumeUSD, ad.MaxPools)
		if err != nil {
			return nil, fmt.Errorf("auto_discover: %w", err)
		}
		for _, sp := range pools {
			add(common.HexToAddress(sp.Address), 0)
		}
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("no pools configured for chain %s", ch.Name)
	}
	return targets, nil
}

func (r *Runner) snapshotPool(ctx context.Context, ch config.ChainConfig, target PoolTarget) error {
	addr := target.Address
	poolKey := strings.ToLower(addr.Hex())

	statuses, err := r.repo.ListSnapshotStatuses(ctx, ch.Name)
	if err != nil {
		return err
	}

	if statuses[poolKey] == storage.SnapshotDisabled ||
		statuses[poolKey] == storage.SnapshotReady {
		r.logger.Info("snapshot skipped (DISABLED or READY)", "pool", addr.Hex())
		return nil
	}

	if err := r.repo.SetSnapshotStatus(ctx, ch.Name, poolKey, storage.SnapshotInitializing); err != nil {
		return fmt.Errorf("mark INITIALIZING: %w", err)
	}

	svc, err := service.NewPoolQuoteService(
		ch.WSEndpoint, ch.RPCEndpoint,
		service.Config{
			ChainName:        ch.Name,
			PoolAddress:      addr,
			MulticallAddress: common.HexToAddress(ch.GetMulticallAddress()),
			QuoterAddress:    common.HexToAddress(ch.GetQuoterAddress()),
		},
		r.logger, nil, nil, nil,
	)
	if err != nil {
		_ = r.repo.SetSnapshotStatus(ctx, ch.Name, poolKey, storage.SnapshotFailed)
		return fmt.Errorf("create pool service: %w", err)
	}

	if err := svc.ResolvePoolMetadata(); err != nil {
		_ = r.repo.SetSnapshotStatus(ctx, ch.Name, poolKey, storage.SnapshotFailed)
		return fmt.Errorf("resolve metadata: %w", err)
	}
	if err := svc.DoFullSync(); err != nil {
		_ = r.repo.SetSnapshotStatus(ctx, ch.Name, poolKey, storage.SnapshotFailed)
		return fmt.Errorf("full sync: %w", err)
	}

	snap := StateToStorageSnapshot(ch.Name, svc.PoolState())
	snap.SnapshotStatus = storage.SnapshotReady
	if err := r.repo.Save(ctx, snap); err != nil {
		_ = r.repo.SetSnapshotStatus(ctx, ch.Name, poolKey, storage.SnapshotFailed)
		return fmt.Errorf("save snapshot: %w", err)
	}

	r.logger.Info("snapshot pool ready",
		"chain", ch.Name,
		"pool", addr.Hex(),
		"block", snap.BlockNumber,
		"ticks", len(snap.TickData),
	)
	return nil
}

// StateToStorageSnapshot 将内存池子状态转为 storage.PoolSnapshot。
func StateToStorageSnapshot(chainName string, state *pool.State) *storage.PoolSnapshot {
	if state == nil {
		return nil
	}
	copy := state.GetStateCopy()
	snap := &storage.PoolSnapshot{
		ChainName:    chainName,
		PoolAddress:  strings.ToLower(copy.Address.Hex()),
		BlockNumber:  copy.BlockNumber,
		Tick:         copy.Tick,
		SqrtPriceX96: new(big.Int).Set(copy.SqrtPriceX96),
		Liquidity:    new(big.Int).Set(copy.Liquidity),
		Fee:          copy.Fee,
		Token0Symbol: copy.Token0Symbol,
		Token1Symbol: copy.Token1Symbol,
		TickData:     make(map[int32]storage.TickLiquiditySnapshot),
	}
	for tick, tl := range copy.Ticks {
		snap.TickData[tick] = storage.TickLiquiditySnapshot{
			LiquidityNet:   new(big.Int).Set(tl.LiquidityNet),
			LiquidityGross: new(big.Int).Set(tl.LiquidityGross),
		}
	}
	return snap
}
