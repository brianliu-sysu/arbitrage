package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HealthHandler exposes liveness checks for load balancers and operators.
type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

type healthHTTPResponse struct {
	Status string `json:"status"`
}

// HandleHealth serves GET /health and GET /api/v1/health.
func (h *HealthHandler) HandleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, healthHTTPResponse{Status: "ok"})
}
