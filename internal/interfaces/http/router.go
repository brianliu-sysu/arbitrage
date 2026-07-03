package httpapi

import "github.com/gin-gonic/gin"

// Handlers groups HTTP handlers exposed by the API.
type Handlers struct {
	Quote *QuoteHandler
}

// NewRouter registers HTTP routes on a Gin engine.
func NewRouter(handlers Handlers) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Recovery())

	if handlers.Quote != nil {
		router.POST("/quote", handlers.Quote.HandleQuote)
	}

	return router
}
