package marketstore

import (
	"context"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

func (v *View) BlockNumber() uint64 {
	if v == nil {
		return 0
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active.version.Number
}

func (v *View) Version() domainchain.MarketVersion {
	if v == nil {
		return domainchain.MarketVersion{}
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active.version
}

func (v *View) Generation() uint64 { return v.Version().Generation }

type listedIDs[ID comparable] []ID

func (ids listedIDs[ID]) List(context.Context) ([]ID, error) { return ids, nil }

func (v *View) IsSystemReady() bool { return v.BlockNumber() > 0 }

func (v *View) IsV3PoolReady(id common.Address) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active.univ3[id] != nil
}
func (v *View) IsPancakeV3PoolReady(id common.Address) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active.pancake[id] != nil
}
func (v *View) IsQuickSwapV3PoolReady(id common.Address) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active.quickSwap[id] != nil
}
func (v *View) IsV4PoolReady(id marketuniv4.PoolID) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active.univ4[id] != nil
}
func (v *View) IsBalancerPoolReady(id marketbalancer.PoolID) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active.balancer[id] != nil
}

type addressReadiness struct {
	view *View
	kind uint8
}

func (r addressReadiness) IsSystemReady() bool { return r.view.IsSystemReady() }
func (r addressReadiness) BlockNumber() uint64 { return r.view.BlockNumber() }
func (r addressReadiness) Generation() uint64  { return r.view.Version().Generation }
func (r addressReadiness) IsPoolReady(id common.Address) bool {
	switch r.kind {
	case 1:
		return r.view.IsV3PoolReady(id)
	case 2:
		return r.view.IsPancakeV3PoolReady(id)
	default:
		return r.view.IsQuickSwapV3PoolReady(id)
	}
}

type v4Readiness struct{ view *View }

func (r v4Readiness) IsSystemReady() bool                    { return r.view.IsSystemReady() }
func (r v4Readiness) BlockNumber() uint64                    { return r.view.BlockNumber() }
func (r v4Readiness) Generation() uint64                     { return r.view.Version().Generation }
func (r v4Readiness) IsPoolReady(id marketuniv4.PoolID) bool { return r.view.IsV4PoolReady(id) }

type balancerReadiness struct{ view *View }

func (r balancerReadiness) IsSystemReady() bool { return r.view.IsSystemReady() }
func (r balancerReadiness) BlockNumber() uint64 { return r.view.BlockNumber() }
func (r balancerReadiness) Generation() uint64  { return r.view.Version().Generation }
func (r balancerReadiness) IsPoolReady(id marketbalancer.PoolID) bool {
	return r.view.IsBalancerPoolReady(id)
}

// Readiness exposes the committed-view readiness contract without leaking its
// private adapter implementation.
type Readiness[PoolID comparable] interface {
	IsSystemReady() bool
	BlockNumber() uint64
	Generation() uint64
	IsPoolReady(PoolID) bool
}

func (v *View) Univ3Readiness() Readiness[common.Address] {
	return addressReadiness{view: v, kind: 1}
}

func (v *View) PancakeReadiness() Readiness[common.Address] {
	return addressReadiness{view: v, kind: 2}
}

func (v *View) QuickSwapReadiness() Readiness[common.Address] {
	return addressReadiness{view: v, kind: 3}
}

func (v *View) Univ4Readiness() Readiness[marketuniv4.PoolID] {
	return v4Readiness{view: v}
}

func (v *View) BalancerReadiness() Readiness[marketbalancer.PoolID] {
	return balancerReadiness{view: v}
}
