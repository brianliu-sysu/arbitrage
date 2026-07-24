package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	appruntime "github.com/brianliu-sysu/uniswapv3/internal/app/arbitrage/runtime"
	"go.uber.org/zap"
)

const shutdownTimeout = 15 * time.Second

// Application owns the explicitly constructed arbitrage runtime and its lifecycle.
type Application struct {
	logger  *zap.Logger
	runtime *appruntime.Manager

	mu      sync.Mutex
	started bool
	stopped bool
}

// New constructs the complete arbitrage application dependency graph.
func New(params Params) (_ *Application, err error) {
	cfg, err := loadConfig(params)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	logger, err := newLogger(cfg)
	if err != nil {
		return nil, fmt.Errorf("create logger: %w", err)
	}

	runtimeManager, err := appruntime.NewManager(cfg, logger)
	if err != nil {
		_ = logger.Sync()
		return nil, fmt.Errorf("create runtime manager: %w", err)
	}

	return &Application{
		logger:  logger,
		runtime: runtimeManager,
	}, nil
}

// Start starts pool synchronization before accepting HTTP traffic.
func (a *Application) Start(ctx context.Context) error {
	if a == nil {
		return errors.New("application is nil")
	}
	a.mu.Lock()
	if a.started {
		a.mu.Unlock()
		return nil
	}
	if a.stopped {
		a.mu.Unlock()
		return errors.New("application has already stopped")
	}
	a.started = true
	a.mu.Unlock()

	if err := a.runtime.Start(ctx); err != nil {
		return a.failStart(err)
	}
	return nil
}

// Stop shuts down HTTP, pool synchronization, persistence, blockchain clients, and logging.
func (a *Application) Stop(ctx context.Context) error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return nil
	}
	a.stopped = true
	a.mu.Unlock()

	var shutdownErrors []error
	if err := a.runtime.Stop(ctx); err != nil {
		shutdownErrors = append(shutdownErrors, err)
	}
	if a.logger != nil {
		if err := a.logger.Sync(); err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
	}
	return errors.Join(shutdownErrors...)
}

// Run starts the application, waits for cancellation, and performs bounded shutdown.
func (a *Application) Run(ctx context.Context) error {
	if err := a.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return a.Stop(stopCtx)
}

func (a *Application) failStart(startErr error) error {
	stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if stopErr := a.Stop(stopCtx); stopErr != nil {
		return errors.Join(startErr, stopErr)
	}
	return startErr
}
