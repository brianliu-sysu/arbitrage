package combined

import (
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	syncv3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ3"
	syncpancakev3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/pancakev3"
	syncv4 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// ProtocolReadinessDiagnostics summarizes readiness for one sync protocol.
type ProtocolReadinessDiagnostics struct {
	Enabled     bool `json:"enabled"`
	SystemReady bool `json:"systemReady"`
	ReadyPools  int  `json:"readyPools"`
	TotalPools  int  `json:"totalPools"`
}

// ReadinessDiagnostics summarizes cross-protocol sync readiness.
type ReadinessDiagnostics struct {
	ArbitrageReady bool                         `json:"arbitrageReady"`
	V3             ProtocolReadinessDiagnostics `json:"univ3"`
	Pancake        ProtocolReadinessDiagnostics `json:"pancakev3"`
	V4             ProtocolReadinessDiagnostics `json:"univ4"`
}

// SyncReadiness adapts sync readiness services for unified quoting.
type SyncReadiness struct {
	V3      *syncv3.ReadinessService
	Pancake *syncpancakev3.ReadinessService
	V4      *syncv4.ReadinessService
}

func (r *SyncReadiness) IsSystemReady() bool {
	if r == nil {
		return true
	}
	if r.V3 != nil && !r.V3.IsSystemReady() {
		return false
	}
	if r.Pancake != nil && !r.Pancake.IsSystemReady() {
		return false
	}
	if r.V4 != nil && !r.V4.IsSystemReady() {
		return false
	}
	return true
}

// Diagnostics returns a snapshot of readiness across enabled sync protocols.
func (r *SyncReadiness) Diagnostics() ReadinessDiagnostics {
	d := ReadinessDiagnostics{
		ArbitrageReady: r.IsSystemReady(),
	}
	if r.V3 != nil {
		snap := r.V3.Snapshot()
		d.V3 = ProtocolReadinessDiagnostics{
			Enabled:     true,
			SystemReady: snap.SystemReady,
			ReadyPools:  snap.ReadyPools,
			TotalPools:  snap.TotalPools,
		}
	}
	if r.Pancake != nil {
		snap := r.Pancake.Snapshot()
		d.Pancake = ProtocolReadinessDiagnostics{
			Enabled:     true,
			SystemReady: snap.SystemReady,
			ReadyPools:  snap.ReadyPools,
			TotalPools:  snap.TotalPools,
		}
	}
	if r.V4 != nil {
		snap := r.V4.Snapshot()
		d.V4 = ProtocolReadinessDiagnostics{
			Enabled:     true,
			SystemReady: snap.SystemReady,
			ReadyPools:  snap.ReadyPools,
			TotalPools:  snap.TotalPools,
		}
	}
	return d
}

func (r *SyncReadiness) IsV3PoolReady(poolAddress common.Address) bool {
	if r == nil || r.V3 == nil {
		return false
	}
	return r.V3.IsPoolReady(poolAddress)
}

func (r *SyncReadiness) IsPancakeV3PoolReady(poolAddress common.Address) bool {
	if r == nil || r.Pancake == nil {
		return false
	}
	return r.Pancake.IsPoolReady(poolAddress)
}

func (r *SyncReadiness) IsV4PoolReady(poolID marketuniv4.PoolID) bool {
	if r == nil || r.V4 == nil {
		return false
	}
	return r.V4.IsPoolReady(poolID)
}
