// main 启动 Uniswap V3 多链报价服务。
package main

import (
	"context"
	"flag"
	"net/http"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/config"
	"github.com/brianliu-sysu/arbitrage/internal/httpapi"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/service"
	"github.com/brianliu-sysu/arbitrage/internal/store"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/brianliu-sysu/arbitrage/internal/tracing"
	"github.com/ethereum/go-ethereum/common"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// 配置校验
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	logger, err := logx.NewWithFile(cfg.LogFile, cfg.LogLevel)
	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}

	tracingShutdown, err := tracing.Init(tracing.Config{
		Endpoint:    cfg.TracingEndpoint,
		ServiceName: "arbitrage",
	})
	if err != nil {
		logger.Error("failed to init tracing", "error", err)
		os.Exit(1)
	}

	chains := cfg.GetChains()
	if len(chains) == 0 {
		logger.Error("no chains configured")
		os.Exit(1)
	}

	// 初始化持久化存储
	var st store.Storer
	if cfg.DBURL != "" {
		// 启动时自动执行迁移（正式部署使用 cmd/migrate）
		_ = store.RunMigrations(cfg.DBURL)

		var err error
		st, err = store.NewPostgresStore(context.Background(), cfg.DBURL)
		if err != nil {
			logger.Error("failed to connect to database, running without persistence", "error", err)
			st = nil
		}
		logger.Info("database connected", "url", maskEndpoint(cfg.DBURL))
	}

	multiChain := service.NewMultiChainService(logger)
	totalPools := 0

	type lastPrintEntry struct{ t time.Time }
	printTimes := make(map[string]*lastPrintEntry)
	var printMu sync.Mutex

	addedCount := 0

	for _, ch := range chains {
		totalPools += len(ch.Pools)
		if ch.WSEndpoint == "" {
			ch.WSEndpoint = os.Getenv("ETH_WS_URL")
		}
		if ch.WSEndpoint == "" {
			logger.Error("chain has empty ws_endpoint, skipping", "chain", ch.Name)
			continue
		}

		bridgeAddrs := make([]common.Address, len(ch.BridgeTokens))
		for i, t := range ch.BridgeTokens {
			bridgeAddrs[i] = common.HexToAddress(t)
		}
		factoryAddr := common.HexToAddress(ch.FactoryAddress)
		baseTokenAddrs := []common.Address{
			common.HexToAddress(ch.WETH),
			common.HexToAddress(ch.USDC),
			common.HexToAddress(ch.USDT),
		}
		maxHops := ch.MaxHops
		if maxHops == 0 {
			maxHops = cfg.MaxHops
		}

		// P0-8: 启动依赖检查 —— 验证 RPC 可达
		if err := checkRPCReachable(ch.RPCEndpoint, 5*time.Second); err != nil {
			logger.Warn("RPC endpoint not reachable, chain will still start", "chain", ch.Name, "error", err)
		}

		svc := service.NewMultiPoolService(
			ch.WSEndpoint, ch.RPCEndpoint, maxHops, bridgeAddrs,
			cfg.MaxBlockGapForFullSync, factoryAddr, baseTokenAddrs,
			logger, st,
		)
		multiChain.AddChain(ch.Name, svc)

		for _, pc := range ch.Pools {
			poolAddr := common.HexToAddress(pc.PoolAddress)
			if err := svc.AddPool(poolAddr, cfg.HealthCheckIntervalSec, pc.SyncFromBlock); err != nil {
				logger.Error("failed to add pool", "chain", ch.Name, "pool", pc.PoolAddress, "error", err)
			} else {
				addedCount++
			}
		}
		// 自动发现：通过 Subgraph 获取 Top N 池子
		if ad := ch.GetAutoDiscover(); ad.Enabled {
			autoAdded := svc.AutoDiscoverPools(ad.SubgraphURL, ad.OrderBy, ad.MinTVLUSD, ad.MinVolumeUSD, ad.MaxPools)
			addedCount += autoAdded
			totalPools += autoAdded
		}

		logger.Info("chain started", "chain", ch.Name, "pools", len(ch.Pools))
	}

	// P0-8: 零池子则退出
	if addedCount == 0 {
		logger.Error("zero pools started successfully, exiting")
		os.Exit(1)
	}

	logger.Info("Uniswap V3 Multi-Pool Quote Service starting",
		"chains", len(chains), "pools", totalPools,
		"http_port", cfg.HTTPPort,
	)

	multiChain.SetOnPriceUpdateAll(func(chain string, addr common.Address, price0In1, price1In0 float64, tick int32) {
		printMu.Lock()
		key := chain + ":" + addr.Hex()
		entry, ok := printTimes[key]
		if !ok {
			entry = &lastPrintEntry{}
			printTimes[key] = entry
		}
		now := time.Now()
		if now.Sub(entry.t) < time.Second {
			printMu.Unlock()
			return
		}
		entry.t = now
		printMu.Unlock()
		logger.Debug("price update", "chain", chain, "pool", addr.Hex(), "price0In1", price0In1, "price1In0", price1In0, "tick", tick)
	})

	logger.Info("all pools across all chains started, listening for events")

	var httpSrv *httpapi.Server
	if cfg.HTTPPort > 0 {
		httpAddr := fmt.Sprintf(":%d", cfg.HTTPPort)
		httpSrv = httpapi.NewServer(httpAddr, multiChain, nil, logger, cfg.HTTPRateLimit, cfg.APIKey)
		go func() {
			if err := httpSrv.Start(); err != nil {
				logger.Error("HTTP server stopped", "error", err)
			}
		}()
		logger.Info("HTTP API listening", "addr", fmt.Sprintf("0.0.0.0:%d", cfg.HTTPPort))
	}

	// P0-1: 优雅关闭
	// P2-12: 周期性强制 GC（每 5 分钟）
	stopGC := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				logger.Debug("gc stats", "heapMB", m.HeapInuse/1024/1024, "goroutines", runtime.NumGoroutine())
			case <-stopGC:
				return
			}
		}
	}()

	// P2-13: 定期汇总统计（每 10 分钟）
	stopStats := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				logger.Info("periodic stats", "chains", len(chains), "pools", totalPools, "goroutines", runtime.NumGoroutine())
			case <-stopStats:
				return
			}
		}
	}()

	waitForShutdown()
	logger.Info("shutting down...")
	close(stopGC)
	close(stopStats)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		// 关闭顺序：HTTP → 健康检查 → 事件订阅 → RPC → DB → Logger
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
		logger.Close()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("graceful shutdown complete")
	case <-shutdownCtx.Done():
		logger.Error("graceful shutdown timed out, forcing exit")
		os.Exit(1)
	}
}

func waitForShutdown() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	for {
		sig := <-sigCh
		switch sig {
		case syscall.SIGHUP:
			fmt.Fprintf(os.Stderr, "\nReceived SIGHUP: log rotation hint (no config reload)\n")
			// SIGHUP 仅用于日志旋转提示，完整热加载需要更复杂的实现
		default:
			fmt.Fprintf(os.Stderr, "\nReceived signal: %v\n", sig)
			return
		}
	}
}

// P0-8: checkRPCReachable 验证 HTTP RPC 端点可达。
func checkRPCReachable(rpcURL string, timeout time.Duration) error {
	if rpcURL == "" {
		return fmt.Errorf("empty RPC URL")
	}
	client := &http.Client{Timeout: timeout}
	body := `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`
	resp, err := client.Post(rpcURL, "application/json", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("RPC unreachable: %w", err)
	}
	resp.Body.Close()
	return nil
}

func maskEndpoint(url string) string {
	if len(url) > 50 {
		return url[:50] + "..."
	}
	return url
}