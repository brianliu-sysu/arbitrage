package marketstore

import (
	"context"
	"fmt"
	"time"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

type commitPlan struct {
	version      domainchain.MarketVersion
	current      snapshot
	next         snapshot
	changes      Changes
	fullRegistry bool
}

type commitRegistries struct {
	univ3     Registry[common.Address]
	pancake   Registry[common.Address]
	quickSwap Registry[common.Address]
	univ4     Registry[marketuniv4.PoolID]
	balancer  Registry[marketbalancer.PoolID]
}

// commit builds a complete snapshot and only publishes it after every pool has
// been validated at the requested market version.
func (v *View) commit(ctx context.Context, version domainchain.MarketVersion, changes Changes) error {
	if v == nil {
		return nil
	}
	if err := validateMarketVersion(version); err != nil {
		return err
	}

	started := time.Now()
	plan := v.prepareCommit(version, changes)
	v.logCommitStarted(plan)

	if err := v.buildSnapshot(ctx, &plan); err != nil {
		return err
	}

	v.publishSnapshot(plan.next)
	v.notifyPublished(plan)
	v.logCommitCompleted(plan, started)
	return nil
}

func validateMarketVersion(version domainchain.MarketVersion) error {
	if version.Number == 0 {
		return fmt.Errorf("committed market view block number must be positive")
	}
	return nil
}

func (v *View) prepareCommit(version domainchain.MarketVersion, changes Changes) commitPlan {
	v.mu.RLock()
	current := v.active
	v.mu.RUnlock()

	next := cloneSnapshot(current)
	next.version = version
	return commitPlan{
		version:      version,
		current:      current,
		next:         next,
		changes:      changes,
		fullRegistry: current.version.IsZero(),
	}
}

func (v *View) buildSnapshot(ctx context.Context, plan *commitPlan) error {
	registries, err := v.registriesForCommit(ctx, plan)
	if err != nil {
		return err
	}
	return v.loadPools(ctx, plan, registries)
}

func (v *View) registriesForCommit(ctx context.Context, plan *commitPlan) (commitRegistries, error) {
	registries := commitRegistries{
		univ3:     v.sources.Univ3Registry,
		pancake:   v.sources.PancakeRegistry,
		quickSwap: v.sources.QuickSwapRegistry,
		univ4:     v.sources.Univ4Registry,
		balancer:  v.sources.BalancerRegistry,
	}
	if plan.fullRegistry {
		return registries, nil
	}

	ids, err := reconcileRegistry(ctx, v.sources.Univ3Registry, plan.next.univ3, plan.changes.Univ3)
	if err != nil {
		return commitRegistries{}, fmt.Errorf("reconcile univ3 registry: %w", err)
	}
	registries.univ3 = listedIDs[common.Address](ids)

	ids, err = reconcileRegistry(ctx, v.sources.PancakeRegistry, plan.next.pancake, plan.changes.PancakeV3)
	if err != nil {
		return commitRegistries{}, fmt.Errorf("reconcile pancakev3 registry: %w", err)
	}
	registries.pancake = listedIDs[common.Address](ids)

	ids, err = reconcileRegistry(ctx, v.sources.QuickSwapRegistry, plan.next.quickSwap, plan.changes.QuickSwapV3)
	if err != nil {
		return commitRegistries{}, fmt.Errorf("reconcile quickswapv3 registry: %w", err)
	}
	registries.quickSwap = listedIDs[common.Address](ids)

	v4IDsToLoad, err := reconcileRegistry(ctx, v.sources.Univ4Registry, plan.next.univ4, plan.changes.Univ4)
	if err != nil {
		return commitRegistries{}, fmt.Errorf("reconcile univ4 registry: %w", err)
	}
	registries.univ4 = listedIDs[marketuniv4.PoolID](v4IDsToLoad)

	balancerIDsToLoad, err := reconcileRegistry(ctx, v.sources.BalancerRegistry, plan.next.balancer, plan.changes.Balancer)
	if err != nil {
		return commitRegistries{}, fmt.Errorf("reconcile balancer registry: %w", err)
	}
	registries.balancer = listedIDs[marketbalancer.PoolID](balancerIDsToLoad)
	return registries, nil
}

func (v *View) loadPools(ctx context.Context, plan *commitPlan, registries commitRegistries) error {
	blockNumber := plan.version.Number
	if err := loadAddressPools(ctx, v.logger, blockNumber, registries.univ3, v.sources.Univ3Pools, plan.next.univ3, poolIDSet(plan.changes.Univ3), plan.fullRegistry, "univ3"); err != nil {
		return err
	}
	if err := loadAddressPools(ctx, v.logger, blockNumber, registries.pancake, v.sources.PancakePools, plan.next.pancake, poolIDSet(plan.changes.PancakeV3), plan.fullRegistry, "pancakev3"); err != nil {
		return err
	}
	if err := loadAddressPools(ctx, v.logger, blockNumber, registries.quickSwap, v.sources.QuickSwapPools, plan.next.quickSwap, poolIDSet(plan.changes.QuickSwapV3), plan.fullRegistry, "quickswapv3"); err != nil {
		return err
	}
	if err := loadIDPools(ctx, v.logger, blockNumber, registries.univ4, v.sources.Univ4Pools, plan.next.univ4, poolIDSet(plan.changes.Univ4), plan.fullRegistry, "univ4"); err != nil {
		return err
	}
	return loadIDPools(ctx, v.logger, blockNumber, registries.balancer, v.sources.BalancerPools, plan.next.balancer, poolIDSet(plan.changes.Balancer), plan.fullRegistry, "balancer")
}

func (v *View) publishSnapshot(next snapshot) {
	v.mu.Lock()
	v.active = next
	v.mu.Unlock()
}

func (v *View) notifyPublished(plan commitPlan) {
	if v.listener == nil {
		return
	}
	changes := plan.changes
	if plan.fullRegistry {
		changes = allSnapshotChanges(plan.next)
	}
	v.listener.AfterMarketPublished(plan.version, changes)
}

func allSnapshotChanges(value snapshot) Changes {
	changes := Changes{
		Univ3:       make([]common.Address, 0, len(value.univ3)),
		PancakeV3:   make([]common.Address, 0, len(value.pancake)),
		QuickSwapV3: make([]common.Address, 0, len(value.quickSwap)),
		Univ4:       make([]marketuniv4.PoolID, 0, len(value.univ4)),
		Balancer:    make([]marketbalancer.PoolID, 0, len(value.balancer)),
	}
	for id := range value.univ3 {
		changes.Univ3 = append(changes.Univ3, id)
	}
	for id := range value.pancake {
		changes.PancakeV3 = append(changes.PancakeV3, id)
	}
	for id := range value.quickSwap {
		changes.QuickSwapV3 = append(changes.QuickSwapV3, id)
	}
	for id := range value.univ4 {
		changes.Univ4 = append(changes.Univ4, id)
	}
	for id := range value.balancer {
		changes.Balancer = append(changes.Balancer, id)
	}
	return changes
}

func (v *View) logCommitStarted(plan commitPlan) {
	v.logger.Info("committed market view commit start",
		zap.Uint64("block", plan.version.Number),
		zap.Uint64("generation", plan.version.Generation),
		zap.String("hash", plan.version.Hash.Hex()),
		zap.Uint64("previous_block", plan.current.version.Number),
		zap.Uint64("previous_generation", plan.current.version.Generation),
		zap.Bool("full_registry_commit", plan.fullRegistry),
		zap.Int("univ3_changed", len(plan.changes.Univ3)),
		zap.Int("pancakev3_changed", len(plan.changes.PancakeV3)),
		zap.Int("quickswapv3_changed", len(plan.changes.QuickSwapV3)),
		zap.Int("univ4_changed", len(plan.changes.Univ4)),
		zap.Int("balancer_changed", len(plan.changes.Balancer)),
		zap.Int("univ3_snapshot", len(plan.current.univ3)),
		zap.Int("pancakev3_snapshot", len(plan.current.pancake)),
		zap.Int("quickswapv3_snapshot", len(plan.current.quickSwap)),
		zap.Int("univ4_snapshot", len(plan.current.univ4)),
		zap.Int("balancer_snapshot", len(plan.current.balancer)),
	)
}

func (v *View) logCommitCompleted(plan commitPlan, started time.Time) {
	v.logger.Info("committed market view commit done",
		zap.Uint64("block", plan.version.Number),
		zap.Uint64("generation", plan.version.Generation),
		zap.String("hash", plan.version.Hash.Hex()),
		zap.Int("univ3_snapshot", len(plan.next.univ3)),
		zap.Int("pancakev3_snapshot", len(plan.next.pancake)),
		zap.Int("quickswapv3_snapshot", len(plan.next.quickSwap)),
		zap.Int("univ4_snapshot", len(plan.next.univ4)),
		zap.Int("balancer_snapshot", len(plan.next.balancer)),
		zap.Int64("duration_ms", time.Since(started).Milliseconds()),
	)
}

func reconcileRegistry[ID comparable, P any](
	ctx context.Context,
	registry Registry[ID],
	existing map[ID]*P,
	changed []ID,
) ([]ID, error) {
	if registry == nil {
		return nil, nil
	}
	ids, err := registry.List(ctx)
	if err != nil {
		return nil, err
	}
	active := make(map[ID]struct{}, len(ids))
	loadSet := make(map[ID]struct{}, len(changed))
	for _, id := range changed {
		loadSet[id] = struct{}{}
	}
	for _, id := range ids {
		active[id] = struct{}{}
		if existing[id] == nil {
			loadSet[id] = struct{}{}
		}
	}
	for id := range existing {
		if _, ok := active[id]; !ok {
			delete(existing, id)
		}
	}
	toLoad := make([]ID, 0, len(loadSet))
	for id := range loadSet {
		if _, ok := active[id]; ok {
			toLoad = append(toLoad, id)
		}
	}
	return toLoad, nil
}
