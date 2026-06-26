// main 启动 Uniswap V3 多链报价服务。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/brianliu-sysu/arbitrage/internal/config"
	"github.com/brianliu-sysu/arbitrage/internal/httpapi"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/service"
	"github.com/brianliu-sysu/arbitrage/internal/store"
	"github.com/brianliu-sysu/arbitrage/internal/tracing"
	"github.com/brianliu-sysu/arbitrage/internal/utils"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML config file")
	flag.Parse()

	// ---- 1. 配置加载与校验 ----
	cfg := mustLoadConfig(*configPath)

	// ---- 2. 日志初始化 ----
	logger := mustInitLogger(cfg)
	defer logger.Close()

	// ---- 3. 链路追踪 ----
	tracingShutdown := mustInitTracing(cfg, logger)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tracingShutdown(ctx)
	}()

	// ---- 4. 持久化存储 ----
	st := initStore(cfg, logger)
	if st != nil {
		defer st.Close()
	}

	// ---- 5. 初始化链与池子 ----
	chains := cfg.GetChains()
	multiChain := service.NewMultiChainService(logger)

	totalPools, addedCount := setupAllChains(chains, cfg, logger, st, multiChain)
	if addedCount == 0 {
		logger.Error("zero pools started successfully, exiting")
		os.Exit(1)
	}

	logger.Info("Uniswap V3 Multi-Pool Quote Service starting",
		"chains", len(chains), "pools", totalPools, "http_port", cfg.HTTPPort,
	)

	// ---- 6. 启动 HTTP API ----
	httpSrv := startHTTPServer(cfg, multiChain, logger)

	// ---- 7. 启动定时任务 ----
	cronScheduler := setupPeriodicTasks(logger, len(chains), totalPools)
	defer cronScheduler.Stop()

	// ---- 8. 等待信号并优雅关闭 ----
	waitForShutdown()
	logger.Info("shutting down...")
	cronScheduler.Stop()

	shutdownGracefully(httpSrv, multiChain, st, tracingShutdown, logger)
	logger.Info("graceful shutdown complete")
}

// ---------------------------------------------------------------------------
// 初始化辅助函数
// ---------------------------------------------------------------------------

// mustLoadConfig 加载并校验配置文件，失败则 Fatal。
func mustLoadConfig(path string) *config.AppConfig {
	cfg, err := config.Load(path)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}
	return cfg
}

// mustInitLogger 根据配置创建 Logger，失败则 Fatal。
func mustInitLogger(cfg *config.AppConfig) logx.Logger {
	logger, err := logx.NewWithFile(cfg.LogFile, cfg.LogLevel)
	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}
	return logger
}

// mustInitTracing 初始化 OpenTelemetry 链路追踪，失败则 Exit。
// 返回关闭函数，调用方需在退出前 defer 调用。
func mustInitTracing(cfg *config.AppConfig, logger logx.Logger) func(context.Context) error {
	shutdown, err := tracing.Init(tracing.Config{
		Endpoint:    cfg.TracingEndpoint,
		ServiceName: "arbitrage",
	})
	if err != nil {
		logger.Error("failed to init tracing", "error", err)
		os.Exit(1)
	}
	return shutdown
}

// initStore 初始化持久化存储，失败返回 nil（降级运行）。
func initStore(cfg *config.AppConfig, logger logx.Logger) store.Storer {
	if cfg.DBURL == "" {
		return nil
	}

	// 启动时自动执行迁移
	_ = store.RunMigrations(cfg.DBURL)

	st, err := store.NewPostgresStore(context.Background(), cfg.DBURL)
	if err != nil {
		logger.Error("failed to connect to database, running without persistence", "error", err)
		return nil
	}

	logger.Info("database connected", "url", maskEndpoint(cfg.DBURL))
	return st
}

// ---------------------------------------------------------------------------
// 链与池子初始化
// ---------------------------------------------------------------------------

// setupAllChains 遍历所有链配置，为每条链创建 MultiPoolService 并添加池子。
// 返回总池子数和实际成功添加的池子数。
func setupAllChains(
	chains []config.ChainConfig,
	cfg *config.AppConfig,
	logger logx.Logger,
	st store.Storer,
	multiChain *service.MultiChainService,
) (totalPools, addedCount int) {
	for _, ch := range chains {
		if ch.WSEndpoint == "" {
			ch.WSEndpoint = os.Getenv("ETH_WS_URL")
		}
		if ch.WSEndpoint == "" {
			logger.Error("chain has empty ws_endpoint, skipping", "chain", ch.Name)
			continue
		}

		added, autoAdded := setupSingleChain(ch, cfg, logger, st, multiChain)
		addedCount += added + autoAdded
		totalPools += added + autoAdded

		logger.Info("chain started", "chain", ch.Name,
			"manualPools", len(ch.Pools), "autoDiscovered", autoAdded)
	}
	return
}

// setupSingleChain 为单条链创建服务并添加手动配置的池子 + 自动发现池子。
// 返回手动添加成功数和自动发现添加数。
func setupSingleChain(
	ch config.ChainConfig,
	cfg *config.AppConfig,
	logger logx.Logger,
	st store.Storer,
	multiChain *service.MultiChainService,
) (addedCount, autoAdded int) {
	baseTokens := make([]common.Address, len(ch.BaseTokens))
	for i, t := range ch.BaseTokens {
		baseTokens[i] = common.HexToAddress(t)
	}

	maxHops := ch.MaxHops
	if maxHops == 0 {
		maxHops = cfg.MaxHops
	}

	factoryAddr := common.HexToAddress(ch.FactoryAddress)
	multicallAddr := common.HexToAddress(ch.GetMulticallAddress())
	quoterAddr := common.HexToAddress(ch.GetQuoterAddress())

	svc := service.NewMultiPoolService(
		ch.Name, ch.WSEndpoint, ch.RPCEndpoint, maxHops, baseTokens,
		cfg.MaxBlockGapForFullSync, factoryAddr,
		multicallAddr, quoterAddr, logger, st,
	)
	multiChain.AddChain(ch.Name, svc)

	// 顺序：DB 池子 -> 手动配置池子。
	// 批量添加后只重建一次 PathFinder（AddPoolsBatch 内部保证）。
	poolEntries := make([]service.PoolEntry, 0, len(ch.Pools))
	seen := make(map[common.Address]struct{})

	if st != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		preloaded, err := st.LoadAll(ctx, ch.Name)
		cancel()
		if err != nil {
			logger.Warn("failed to load chain pools from DB, continue with config pools",
				"chain", ch.Name, "error", err)
		} else {
			addrs := make([]string, 0, len(preloaded))
			for addr := range preloaded {
				addrs = append(addrs, addr)
			}
			sort.Strings(addrs)
			for _, addrStr := range addrs {
				addr := common.HexToAddress(addrStr)
				if _, ok := seen[addr]; ok {
					continue
				}
				seen[addr] = struct{}{}
				poolEntries = append(poolEntries, service.PoolEntry{
					PoolAddress:            addr,
					HealthCheckIntervalSec: cfg.HealthCheckIntervalSec,
					SyncFromBlock:          preloaded[addrStr].BlockNumber,
				})
			}
		}
	}

	for _, pc := range ch.Pools {
		addr := common.HexToAddress(pc.PoolAddress)
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		poolEntries = append(poolEntries, service.PoolEntry{
			PoolAddress:            addr,
			HealthCheckIntervalSec: cfg.HealthCheckIntervalSec,
			SyncFromBlock:          pc.SyncFromBlock,
		})
	}

	if len(poolEntries) > 0 {
		if err := svc.AddPoolsBatch(poolEntries); err != nil {
			logger.Error("failed to add pools to chain", "chain", ch.Name, "error", err)
		} else {
			addedCount = len(poolEntries)
		}
	}

	// Subgraph 自动发现（异步，不阻塞主流程启动）。
	if ad := ch.GetAutoDiscover(); ad.Enabled {
		logger.Info("auto-discover scheduled in background", "chain", ch.Name)
		utils.SafeGo(logger, func() {
			added := svc.AutoDiscoverPools(
				ad.SubgraphURL, ad.OrderBy, ad.MinTVLUSD, ad.MinVolumeUSD, ad.MaxPools,
			)
			logger.Info("auto-discover finished", "chain", ch.Name, "added", added)
		})
	}

	return
}

// ---------------------------------------------------------------------------
// HTTP 服务
// ---------------------------------------------------------------------------

// startHTTPServer 根据配置启动 HTTP API 服务，禁用时返回 nil。
func startHTTPServer(cfg *config.AppConfig, multiChain *service.MultiChainService, logger logx.Logger) *httpapi.Server {
	if cfg.HTTPPort <= 0 {
		return nil
	}

	httpAddr := fmt.Sprintf(":%d", cfg.HTTPPort)
	srv := httpapi.NewServer(httpAddr, multiChain, nil, logger, cfg.HTTPRateLimit, cfg.APIKey)
	utils.SafeGo(logger, func() {
		if err := srv.Start(); err != nil {
			logger.Error("HTTP server stopped", "error", err)
		}
	})
	logger.Info("HTTP API listening", "addr", fmt.Sprintf("0.0.0.0:%d", cfg.HTTPPort))
	return srv
}

// ---------------------------------------------------------------------------
// 信号与优雅关闭
// ---------------------------------------------------------------------------

// waitForShutdown 阻塞直到收到 SIGINT / SIGTERM / SIGHUP。
// SIGHUP 仅打印提示不退出。
func waitForShutdown() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	for {
		sig := <-sigCh
		switch sig {
		case syscall.SIGHUP:
			fmt.Fprintf(os.Stderr, "\nReceived SIGHUP: log rotation hint (no config reload)\n")
		default:
			fmt.Fprintf(os.Stderr, "\nReceived signal: %v\n", sig)
			return
		}
	}
}

// shutdownGracefully 按正确顺序关闭所有组件：HTTP → 链服务 → 存储 → 追踪 → 日志。
// 超时时间为 30 秒。
func shutdownGracefully(
	httpSrv *httpapi.Server,
	multiChain *service.MultiChainService,
	st store.Storer,
	tracingShutdown func(context.Context) error,
	logger logx.Logger,
) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	utils.SafeGo(logger, func() {
		if httpSrv != nil {
			httpSrv.ShutdownGraceful(shutdownCtx)
		}
		multiChain.StopAll()
		if st != nil {
			st.Close()
		}
		if err := tracingShutdown(shutdownCtx); err != nil {
			logger.Error("tracing shutdown error", "error", err)
		}
		logger.Info("all services stopped")
		close(done)
	})

	select {
	case <-done:
		// ok
	case <-shutdownCtx.Done():
		logger.Error("graceful shutdown timed out, forcing exit")
		os.Exit(1)
	}
}

func maskEndpoint(url string) string {
	if len(url) > 50 {
		return url[:50] + "..."
	}
	return url
}
