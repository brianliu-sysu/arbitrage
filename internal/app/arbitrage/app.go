package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	quoteapp "github.com/brianliu-sysu/uniswapv3/internal/application/quote"
	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/config"
	domainquote "github.com/brianliu-sysu/uniswapv3/internal/domain/quote"
	chaininfra "github.com/brianliu-sysu/uniswapv3/internal/infrastructure/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/registry"
	httpapi "github.com/brianliu-sysu/uniswapv3/internal/interfaces/http"
	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

// Params holds runtime options for the arbitrage application.
type Params struct {
	ConfigPath string
}

// ParseFlags parses standard CLI flags for the arbitrage binary.
func ParseFlags() Params {
	configPath := flag.String("config", config.DefaultPath, "path to config yaml")
	flag.Parse()
	return Params{ConfigPath: *configPath}
}

// Module wires dependencies and starts pool sync on application lifecycle.
func Module(params Params) fx.Option {
	return fx.Options(
		fx.Supply(params),
		fx.Provide(
			loadConfig,
			newPersistence,
			newBlockchain,
			newPoolRegistry,
			newSyncServices,
			newSyncOrchestrator,
			newQuoteAppService,
			newHTTPRouter,
		),
		fx.Invoke(registerSyncLifecycle, registerHTTPLifecycle),
	)
}

func loadConfig(params Params) (config.Config, error) {
	path := params.ConfigPath
	if path == "" {
		path = config.DefaultPath
	}
	cfg, err := config.Load(path)
	if err != nil {
		return config.Config{}, err
	}
	if cfg.RPC.URL == "" {
		return config.Config{}, fmt.Errorf("rpc.url is required")
	}
	if cfg.RPC.WSURL == "" {
		return config.Config{}, fmt.Errorf("rpc.ws_url is required for block head subscription")
	}
	return cfg, nil
}

func newPersistence(cfg config.Config) (*persistence.Services, error) {
	pCfg := cfg.PersistenceConfig()
	if os.Getenv("USE_MEMORY_DB") == "true" {
		pCfg.UseMemory = true
	}
	return persistence.NewServices(context.Background(), pCfg)
}

func newBlockchain(cfg config.Config) (*chaininfra.Services, error) {
	return chaininfra.NewServices(cfg.BlockchainConfig())
}

func newPoolRegistry(cfg config.Config) *registry.CompositeRegistry {
	return registry.NewCompositeRegistry(cfg.Pools)
}

func newSyncServices(
	cfg config.Config,
	store *persistence.Services,
	chain *chaininfra.Services,
	poolRegistry *registry.CompositeRegistry,
) *syncapp.Services {
	deps := chain.SyncDeps()
	persistDeps := store.SyncDeps()

	deps.Config = cfg.SyncConfig()
	deps.Pools = persistDeps.Pools
	deps.Snapshots = persistDeps.Snapshots
	deps.Checkpoints = persistDeps.Checkpoints
	deps.Registry = poolRegistry
	deps.Health = append(deps.Health, persistDeps.Health...)

	return syncapp.NewServices(deps)
}

func newSyncOrchestrator(services *syncapp.Services, chain *chaininfra.Services) *syncapp.SyncOrchestrator {
	return services.NewOrchestrator(chain.Client)
}

func newQuoteAppService(
	cfg config.Config,
	store *persistence.Services,
	poolRegistry *registry.CompositeRegistry,
	syncServices *syncapp.Services,
) *quoteapp.QuoteAppService {
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}
	return quoteapp.NewQuoteAppService(
		store.Pools,
		poolRegistry,
		domainquote.NewQuoteService(),
		syncServices.Readiness,
		maxHops,
	)
}

func newHTTPRouter(quoteService *quoteapp.QuoteAppService) *gin.Engine {
	return httpapi.NewRouter(httpapi.Handlers{
		Quote: httpapi.NewQuoteHandler(quoteService),
	})
}

type syncLifecycle struct {
	cancel       context.CancelFunc
	orchestrator *syncapp.SyncOrchestrator
	chain        *chaininfra.Services
	store        *persistence.Services
	cfg          config.Config
}

func registerSyncLifecycle(
	lifecycle fx.Lifecycle,
	cfg config.Config,
	orchestrator *syncapp.SyncOrchestrator,
	chain *chaininfra.Services,
	store *persistence.Services,
) {
	runner := &syncLifecycle{
		orchestrator: orchestrator,
		chain:        chain,
		store:        store,
		cfg:          cfg,
	}
	lifecycle.Append(fx.Hook{
		OnStart: runner.start,
		OnStop:  runner.stop,
	})
}

func (r *syncLifecycle) start(_ context.Context) error {
	runCtx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	log.Printf("starting pool sync (persistence=%s, subgraph=%t, static_pools=%d)",
		r.store.BackendName(),
		r.cfg.Pools.Subgraph.IsEnabled(),
		len(r.cfg.Pools.Static),
	)

	go func() {
		if err := r.orchestrator.Start(runCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("pool sync stopped: %v", err)
		}
	}()
	return nil
}

func (r *syncLifecycle) stop(_ context.Context) error {
	if r.cancel != nil {
		r.cancel()
	}
	r.chain.Close()
	r.store.Close()
	log.Println("pool sync shutdown complete")
	return nil
}

type httpLifecycle struct {
	server *http.Server
}

func registerHTTPLifecycle(lifecycle fx.Lifecycle, cfg config.Config, router *gin.Engine) {
	if !cfg.HTTP.Enabled {
		log.Println("http server disabled")
		return
	}

	runner := &httpLifecycle{
		server: &http.Server{
			Addr:              cfg.HTTP.ListenAddr(),
			Handler:           router,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
	lifecycle.Append(fx.Hook{
		OnStart: runner.start,
		OnStop:  runner.stop,
	})
}

func (h *httpLifecycle) start(_ context.Context) error {
	go func() {
		log.Printf("starting http server on %s", h.server.Addr)
		if err := h.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server stopped: %v", err)
		}
	}()
	return nil
}

func (h *httpLifecycle) stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := h.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}
	log.Println("http server shutdown complete")
	return nil
}
