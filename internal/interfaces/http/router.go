package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const apiV1Prefix = "/api/v1"

// Handlers groups HTTP handlers exposed by the API.
type Handlers struct {
	Health        *HealthHandler
	QuoteCombined *QuoteCombinedHandler
	QuoteV3        *QuoteV3Handler
	QuoteV4        *QuoteV4Handler
	QuotePancakeV3 *QuotePancakeV3Handler
}

// NewRouter registers HTTP routes on a Gin engine.
func NewRouter(handlers Handlers) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Recovery(), corsMiddleware())

	if handlers.Health != nil {
		router.GET("/health", handlers.Health.HandleHealth)
	}

	v1 := router.Group(apiV1Prefix)
	{
		if handlers.Health != nil {
			v1.GET("/health", handlers.Health.HandleHealth)
		}
		if handlers.QuoteCombined != nil {
			v1.POST("/quote", handlers.QuoteCombined.HandleQuote)
		}
		if handlers.QuoteV3 != nil {
			v1.POST("/univ3/quote", handlers.QuoteV3.HandleQuote)
		}
		if handlers.QuoteV4 != nil {
			v1.POST("/univ4/quote", handlers.QuoteV4.HandleQuote)
		}
		if handlers.QuotePancakeV3 != nil {
			v1.POST("/pancakev3/quote", handlers.QuotePancakeV3.HandleQuote)
		}
	}

	return router
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
