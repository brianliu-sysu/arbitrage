package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/gin-gonic/gin"
)

func TestRateLimitMiddlewareAllowsFirstRequest(t *testing.T) {
	s := &Server{rateLimit: 1, logger: logx.Nop()}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(s.rateLimitMiddleware())
	r.GET("/ok", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want %d", w.Code, http.StatusOK)
	}
}
