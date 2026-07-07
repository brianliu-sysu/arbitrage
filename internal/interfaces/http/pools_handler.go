package httpapi

import (
	"net/http"
	"strings"

	poolsapp "github.com/brianliu-sysu/uniswapv3/internal/application/pools"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/gin-gonic/gin"
)

// PoolsHandler exposes tracked pool metadata over HTTP.
type PoolsHandler struct {
	pools *poolsapp.AppService
}

func NewPoolsHandler(pools *poolsapp.AppService) *PoolsHandler {
	return &PoolsHandler{pools: pools}
}

// HandleList serves GET /api/v1/pools.
func (h *PoolsHandler) HandleList(c *gin.Context) {
	if h.pools == nil {
		c.JSON(http.StatusInternalServerError, errorHTTPResponse{Error: "pools service is not configured"})
		return
	}

	resp, err := h.pools.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorHTTPResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// HandleDiagnostics serves GET /api/v1/pools/diagnostics.
func (h *PoolsHandler) HandleDiagnostics(c *gin.Context) {
	if h.pools == nil {
		c.JSON(http.StatusInternalServerError, errorHTTPResponse{Error: "pools service is not configured"})
		return
	}

	poolType := strings.TrimSpace(c.Query("poolType"))
	poolIDQuery := strings.TrimSpace(c.Query("poolId"))
	poolAddressQuery := strings.TrimSpace(c.Query("poolAddress"))

	if poolType == "" && poolIDQuery == "" && poolAddressQuery == "" {
		resp, err := h.pools.DiagnosticsAll(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, errorHTTPResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	if poolType == "" {
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: "poolType is required when poolId or poolAddress is set"})
		return
	}

	req := poolsapp.DiagnosticsRequest{PoolType: poolType}
	switch poolType {
	case poolsapp.PoolTypeUniv4:
		poolID, err := parsePoolID(poolIDQuery)
		if err != nil {
			c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
			return
		}
		req.PoolID = poolID
	case poolsapp.PoolTypeBalancer:
		poolID, err := parsePoolID(poolIDQuery)
		if err != nil {
			c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
			return
		}
		req.BalancerPoolID = marketbalancer.PoolID(poolID.Hash())
	case poolsapp.PoolTypeUniv3, poolsapp.PoolTypePancakeV3:
		poolAddress, err := parseAddress(poolAddressQuery, "poolAddress")
		if err != nil {
			c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: err.Error()})
			return
		}
		req.PoolAddress = poolAddress
	default:
		c.JSON(http.StatusBadRequest, errorHTTPResponse{Error: "poolType must be univ3, univ4, pancakev3, or balancer"})
		return
	}

	resp, err := h.pools.Diagnostics(c.Request.Context(), req)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, errorHTTPResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}
