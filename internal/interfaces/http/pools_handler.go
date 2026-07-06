package httpapi

import (
	"net/http"

	poolsapp "github.com/brianliu-sysu/uniswapv3/internal/application/pools"
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
