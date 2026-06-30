// Package api 提供 HTTP 报价 API 服务。
package api

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/utils"
	"github.com/brianliu-sysu/arbitrage/internal/metrics"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// HTTPServer HTTP 服务器抽象，便于替换实现（测试 mock、HTTPS 等）。
type HTTPServer interface {
	ListenAndServe() error
	Close() error
}

// Server HTTP 报价 API 服务器。
type Server struct {
	svc       QuoteProvider
	srv       HTTPServer
	addr      string
	rateLimit int    // 每秒最大请求数，0 不限
	apiKey    string // API key (X-API-Key header)，空则跳过验证
	logger    logx.Logger

	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgWG     sync.WaitGroup
}

// NewServer 创建 HTTP 服务器。
//
// addr 为监听地址（如 ":8080"），srv 为 HTTP 服务器实现（nil 则使用默认 *http.Server）。
// svc 为报价服务实现，logger 为日志记录器。
func NewServer(addr string, svc QuoteProvider, srv HTTPServer, logger logx.Logger, rateLimit int, apiKey string) *Server {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	hs := &Server{
		svc:       svc,
		srv:       srv,
		addr:      addr,
		rateLimit: rateLimit,
		apiKey:    apiKey,
		logger:    logger,
		bgCtx:     bgCtx,
		bgCancel:  bgCancel,
	}

	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(
		otelgin.Middleware("arbitrage"),
		metricsMiddleware(),
		hs.rateLimitMiddleware(),
		hs.authMiddleware(),
		gin.Logger(),
		gin.Recovery(),
	)

	hs.registerRoutes(engine)

	if hs.srv == nil {
		hs.srv = &http.Server{
			Addr:    addr,
			Handler: engine,
		}
	}

	return hs
}

// SetupRouter 构建并返回 gin 路由引擎（用于测试）。
func (s *Server) SetupRouter() *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Logger(), gin.Recovery())
	s.registerRoutes(engine)
	return engine
}

// registerRoutes 注册所有路由。
func (s *Server) registerRoutes(r *gin.Engine) {
	r.GET("/health", s.handleHealth)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	chain := r.Group("/api/v1/:chain")
	{
		chain.GET("/pools", s.handleListPools)
		chain.GET("/pools/:address", s.handleGetPool)
		chain.GET("/pools/:address/price", s.handleGetPrice)
		chain.POST("/pools/:address/quote", s.handleQuote)
		chain.POST("/quote", s.handleCrossQuote)
	}
}

// authMiddleware API Key 验证（如果配置了 apiKey）。
func (s *Server) authMiddleware() gin.HandlerFunc {
	if s.apiKey == "" {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		if c.GetHeader("X-API-Key") != s.apiKey {
			c.JSON(401, gin.H{"error": "unauthorized: invalid or missing X-API-Key"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// Start 启动 HTTP 服务器（阻塞）。
func (s *Server) Start() error {
	s.logger.Info("api listening", "addr", s.addr)
	return s.srv.ListenAndServe()
}

// Shutdown 优雅关闭 HTTP 服务器（立即）。
func (s *Server) Shutdown() error {
	s.bgCancel()
	s.bgWG.Wait()
	return s.srv.Close()
}

// ShutdownGraceful 带超时的优雅关闭。
func (s *Server) ShutdownGraceful(ctx context.Context) error {
	s.bgCancel()
	s.bgWG.Wait()
	if stdSrv, ok := s.srv.(*http.Server); ok {
		return stdSrv.Shutdown(ctx)
	}
	return s.srv.Close()
}

// rateLimitMiddleware 基于令牌桶的简单限流。
func (s *Server) rateLimitMiddleware() gin.HandlerFunc {
	if s.rateLimit <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	tokens := make(chan struct{}, s.rateLimit)
	// 冷启动先填满令牌，避免启动后首秒全部 429。
	for i := 0; i < s.rateLimit; i++ {
		tokens <- struct{}{}
	}
	// 每秒补充令牌
	s.bgWG.Add(1)
	utils.SafeGo(s.logger, func() {
		defer s.bgWG.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.bgCtx.Done():
				return
			case <-ticker.C:
				for i := 0; i < s.rateLimit; i++ {
					select {
					case tokens <- struct{}{}:
					default:
					}
				}
			}
		}
	})
	return func(c *gin.Context) {
		select {
		case <-tokens:
			c.Next()
		default:
			c.JSON(429, gin.H{"error": "rate limit exceeded"})
			c.Abort()
		}
	}
}

// metricsMiddleware 记录每个 HTTP API 请求耗时。
func metricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		elapsed := time.Since(start).Seconds()
		metrics.HTTPRequestDuration.WithLabelValues(
			c.Request.Method,
			c.FullPath(),
			strconv.Itoa(c.Writer.Status()),
		).Observe(elapsed)
	}
}
