package runtime

import (
	"time"

	"go.uber.org/zap"
)

type protocolReadiness interface {
	Name() string
	IsReady() bool
}

func (r *syncLifecycle) runArbitrageRouteWatcher() {
	r.startSafeGoroutine("arbitrage-route-watcher", func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		protocolReady := make(map[string]bool)
		for {
			select {
			case <-r.runCtx.Done():
				return
			case <-ticker.C:
				if r.tryRefreshArbitrageRoutes(protocolReady) {
					r.runtime.Arbitrage.LogDiagnostics(r.runCtx, r.logger, "routes_refreshed")
				}
			}
		}
	})
}

func (r *syncLifecycle) tryRefreshArbitrageRoutes(protocolReady map[string]bool) bool {
	if r.runtime == nil || r.runtime.Arbitrage == nil {
		return false
	}

	newlyReady := newlyReadyProtocols(r.arbitrageProtocols(), protocolReady)
	if len(newlyReady) == 0 {
		return false
	}

	routes, err := r.runtime.Arbitrage.RefreshArbitrageRoutes(r.runCtx)
	if err != nil {
		r.logger.Warn("refresh arbitrage routes failed", zap.Error(err))
		return false
	}
	for _, protocol := range newlyReady {
		protocolReady[protocol] = true
	}

	r.logger.Info("arbitrage routes refreshed",
		zap.Uint64("chain_id", r.runtime.cfg.ChainID),
		zap.Int("routes", routes),
		zap.Int("start_tokens", len(r.runtime.Arbitrage.StartTokens())),
	)
	return true
}

func newlyReadyProtocols(protocols []protocolReadiness, current map[string]bool) []string {
	ready := make([]string, 0, len(protocols))
	for _, protocol := range protocols {
		if protocol.IsReady() && !current[protocol.Name()] {
			ready = append(ready, protocol.Name())
		}
	}
	return ready
}

func (r *syncLifecycle) arbitrageProtocols() []protocolReadiness {
	protocols := make([]protocolReadiness, 0, len(r.runtime.protocols.modules))
	for _, module := range r.runtime.protocols.modules {
		protocols = append(protocols, module)
	}
	return protocols
}
