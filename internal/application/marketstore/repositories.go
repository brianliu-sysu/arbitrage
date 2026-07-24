package marketstore

import (
	"context"
	"fmt"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

func readOnlyError() error { return fmt.Errorf("committed market view is read-only") }

type Univ3Repository struct{ view *View }
type PancakeRepository struct{ view *View }
type QuickSwapRepository struct{ view *View }
type Univ4Repository struct{ view *View }
type BalancerRepository struct{ view *View }

func (v *View) Univ3Repository() *Univ3Repository         { return &Univ3Repository{view: v} }
func (v *View) PancakeRepository() *PancakeRepository     { return &PancakeRepository{view: v} }
func (v *View) QuickSwapRepository() *QuickSwapRepository { return &QuickSwapRepository{view: v} }
func (v *View) Univ4Repository() *Univ4Repository         { return &Univ4Repository{view: v} }
func (v *View) BalancerRepository() *BalancerRepository   { return &BalancerRepository{view: v} }

func (r *Univ3Repository) Get(_ context.Context, id common.Address) (*marketuniv3.Pool, error) {
	r.view.mu.RLock()
	defer r.view.mu.RUnlock()
	if pool := r.view.active.univ3[id]; pool != nil {
		return pool.Clone(), nil
	}
	return nil, nil
}
func (r *Univ3Repository) Save(context.Context, *marketuniv3.Pool) error { return readOnlyError() }
func (r *Univ3Repository) Delete(context.Context, common.Address) error  { return readOnlyError() }
func (r *Univ3Repository) AdvanceSyncProgress(context.Context, common.Address, uint64) error {
	return readOnlyError()
}
func (r *Univ3Repository) AdvanceSyncProgressMany(context.Context, []common.Address, uint64) error {
	return readOnlyError()
}

func (r *PancakeRepository) Get(_ context.Context, id common.Address) (*marketpancake.Pool, error) {
	r.view.mu.RLock()
	defer r.view.mu.RUnlock()
	if pool := r.view.active.pancake[id]; pool != nil {
		return pool.Clone(), nil
	}
	return nil, nil
}
func (r *PancakeRepository) Save(context.Context, *marketpancake.Pool) error { return readOnlyError() }
func (r *PancakeRepository) Delete(context.Context, common.Address) error    { return readOnlyError() }
func (r *PancakeRepository) AdvanceSyncProgress(context.Context, common.Address, uint64) error {
	return readOnlyError()
}
func (r *PancakeRepository) AdvanceSyncProgressMany(context.Context, []common.Address, uint64) error {
	return readOnlyError()
}

func (r *QuickSwapRepository) Get(_ context.Context, id common.Address) (*marketquick.Pool, error) {
	r.view.mu.RLock()
	defer r.view.mu.RUnlock()
	if pool := r.view.active.quickSwap[id]; pool != nil {
		return pool.Clone(), nil
	}
	return nil, nil
}
func (r *QuickSwapRepository) Save(context.Context, *marketquick.Pool) error { return readOnlyError() }
func (r *QuickSwapRepository) Delete(context.Context, common.Address) error  { return readOnlyError() }
func (r *QuickSwapRepository) AdvanceSyncProgress(context.Context, common.Address, uint64) error {
	return readOnlyError()
}
func (r *QuickSwapRepository) AdvanceSyncProgressMany(context.Context, []common.Address, uint64) error {
	return readOnlyError()
}

func (r *Univ4Repository) Get(_ context.Context, id marketuniv4.PoolID) (*marketuniv4.Pool, error) {
	r.view.mu.RLock()
	defer r.view.mu.RUnlock()
	if pool := r.view.active.univ4[id]; pool != nil {
		return pool.Clone(), nil
	}
	return nil, nil
}
func (r *Univ4Repository) Save(context.Context, *marketuniv4.Pool) error    { return readOnlyError() }
func (r *Univ4Repository) Delete(context.Context, marketuniv4.PoolID) error { return readOnlyError() }
func (r *Univ4Repository) AdvanceSyncProgress(context.Context, marketuniv4.PoolID, uint64) error {
	return readOnlyError()
}
func (r *Univ4Repository) AdvanceSyncProgressMany(context.Context, []marketuniv4.PoolID, uint64) error {
	return readOnlyError()
}

func (r *BalancerRepository) Get(_ context.Context, id marketbalancer.PoolID) (*marketbalancer.Pool, error) {
	r.view.mu.RLock()
	defer r.view.mu.RUnlock()
	if pool := r.view.active.balancer[id]; pool != nil {
		return pool.Clone(), nil
	}
	return nil, nil
}
func (r *BalancerRepository) Save(context.Context, *marketbalancer.Pool) error {
	return readOnlyError()
}
func (r *BalancerRepository) Delete(context.Context, marketbalancer.PoolID) error {
	return readOnlyError()
}
func (r *BalancerRepository) AdvanceSyncProgress(context.Context, marketbalancer.PoolID, uint64) error {
	return readOnlyError()
}
func (r *BalancerRepository) AdvanceSyncProgressMany(context.Context, []marketbalancer.PoolID, uint64) error {
	return readOnlyError()
}
