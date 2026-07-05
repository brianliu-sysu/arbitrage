package combined

import (
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	syncv3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ3"
	syncpancakev3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/pancakev3"
	syncv4 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ4"
	"github.com/ethereum/go-ethereum/common"
)

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
