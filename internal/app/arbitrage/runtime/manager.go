package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/app/arbitrage/httpserver"
	"github.com/brianliu-sysu/uniswapv3/internal/config"
	"go.uber.org/zap"
)

// Manager owns every chain runtime and coordinates their process-level
// lifecycle. It is the only runtime API needed by the application root.
type Manager struct {
	sync   []*syncLifecycle
	http   *httpserver.Server
	httpUp bool
}

// NewManager constructs all configured chain runtimes and their entrypoints.
func NewManager(cfg config.Config, logger *zap.Logger) (*Manager, error) {
	runtimes, err := newRuntimeSet(cfg, logger)
	if err != nil {
		return nil, err
	}
	lifecycles, err := newSyncLifecycles(logger, runtimes)
	if err != nil {
		runtimes.Close()
		return nil, err
	}
	manager := &Manager{
		sync: lifecycles,
	}
	if cfg.HTTP.Enabled {
		manager.http = httpserver.New(cfg.HTTP.ListenAddr(), newHTTPRouter(runtimes, logger), logger)
	} else {
		logger.Info("http server disabled")
	}
	return manager, nil
}

// Start starts chain synchronization before accepting HTTP traffic.
func (m *Manager) Start(ctx context.Context) error {
	if m == nil {
		return nil
	}
	for _, runner := range m.sync {
		if err := runner.start(ctx); err != nil {
			return fmt.Errorf("start pool sync: %w", err)
		}
	}
	if m.http != nil {
		if err := m.http.Start(ctx); err != nil {
			return fmt.Errorf("start http server: %w", err)
		}
		m.httpUp = true
	}
	return nil
}

// Stop shuts down HTTP and chain runtimes in reverse construction order.
func (m *Manager) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	var shutdownErrors []error
	if m.httpUp && m.http != nil {
		if err := m.http.Stop(ctx); err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
		m.httpUp = false
	}
	for i := len(m.sync) - 1; i >= 0; i-- {
		if err := m.sync[i].stop(ctx); err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
	}
	return errors.Join(shutdownErrors...)
}
