package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Server owns the HTTP transport lifecycle only. Handler construction remains
// with the runtime composition that owns the chain-specific dependencies.
type Server struct {
	server *http.Server
	logger *zap.Logger
	wg     sync.WaitGroup
}

// New constructs an HTTP server around an already assembled router.
func New(address string, router *gin.Engine, logger *zap.Logger) *Server {
	return &Server{
		server: &http.Server{
			Addr:              address,
			Handler:           router,
			ReadHeaderTimeout: 5 * time.Second,
		},
		logger: logger,
	}
}

// Start begins serving in a lifecycle-accounted background goroutine.
func (s *Server) Start(_ context.Context) error {
	if s == nil {
		return nil
	}
	startSafeGoroutine(&s.wg, func(recovered any) {
		s.logger.Error("http server panicked", zap.Any("panic", recovered), zap.Stack("stack"))
	}, func() {
		s.logger.Info("starting http server",
			zap.String("addr", s.server.Addr),
			zap.String("health", "GET /health, GET /api/v1/health"),
			zap.String("quote_cross_pool", "POST /api/v1/quote"),
			zap.String("quote_v3", "POST /api/v1/univ3/quote"),
			zap.String("quote_pancakev3", "POST /api/v1/pancakev3/quote"),
			zap.String("quote_quickswapv3", "POST /api/v1/quickswapv3/quote"),
			zap.String("quote_v4", "POST /api/v1/univ4/quote"),
		)
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("http server stopped", zap.Error(err))
		}
	})
	return nil
}

// Stop gracefully shuts down the listener and waits for its goroutine.
func (s *Server) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}
	s.wg.Wait()
	s.logger.Info("http server shutdown complete")
	return nil
}

func startSafeGoroutine(wg *sync.WaitGroup, onPanic func(any), run func()) {
	if wg != nil {
		wg.Add(1)
	}
	go func() {
		if wg != nil {
			defer wg.Done()
		}
		defer func() {
			if recovered := recover(); recovered != nil && onPanic != nil {
				onPanic(recovered)
			}
		}()
		run()
	}()
}
