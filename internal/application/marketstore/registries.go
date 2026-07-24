package marketstore

import (
	"context"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

type addressSnapshotKind uint8

const (
	addressSnapshotUniv3 addressSnapshotKind = iota + 1
	addressSnapshotPancakeV3
	addressSnapshotQuickSwapV3
)

type addressSnapshotRegistry struct {
	view *View
	kind addressSnapshotKind
}

func (r *addressSnapshotRegistry) List(context.Context) ([]common.Address, error) {
	if r == nil || r.view == nil {
		return nil, nil
	}
	r.view.mu.RLock()
	defer r.view.mu.RUnlock()

	var ids []common.Address
	switch r.kind {
	case addressSnapshotUniv3:
		ids = mapIDs(r.view.active.univ3)
	case addressSnapshotPancakeV3:
		ids = mapIDs(r.view.active.pancake)
	case addressSnapshotQuickSwapV3:
		ids = mapIDs(r.view.active.quickSwap)
	}
	return ids, nil
}

type univ4SnapshotRegistry struct{ view *View }

func (r *univ4SnapshotRegistry) List(context.Context) ([]marketuniv4.PoolID, error) {
	if r == nil || r.view == nil {
		return nil, nil
	}
	r.view.mu.RLock()
	defer r.view.mu.RUnlock()

	return mapIDs(r.view.active.univ4), nil
}

type balancerSnapshotRegistry struct{ view *View }

func (r *balancerSnapshotRegistry) List(context.Context) ([]marketbalancer.PoolID, error) {
	if r == nil || r.view == nil {
		return nil, nil
	}
	r.view.mu.RLock()
	defer r.view.mu.RUnlock()

	return mapIDs(r.view.active.balancer), nil
}

func mapIDs[ID comparable, Pool any](pools map[ID]*Pool) []ID {
	ids := make([]ID, 0, len(pools))
	for id := range pools {
		ids = append(ids, id)
	}
	return ids
}

func (v *View) Univ3Registry() Registry[common.Address] {
	return &addressSnapshotRegistry{view: v, kind: addressSnapshotUniv3}
}

func (v *View) PancakeRegistry() Registry[common.Address] {
	return &addressSnapshotRegistry{view: v, kind: addressSnapshotPancakeV3}
}

func (v *View) QuickSwapRegistry() Registry[common.Address] {
	return &addressSnapshotRegistry{view: v, kind: addressSnapshotQuickSwapV3}
}

func (v *View) Univ4Registry() Registry[marketuniv4.PoolID] {
	return &univ4SnapshotRegistry{view: v}
}

func (v *View) BalancerRegistry() Registry[marketbalancer.PoolID] {
	return &balancerSnapshotRegistry{view: v}
}
