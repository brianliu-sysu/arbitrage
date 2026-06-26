package main

import (
	"log"
	"os"
	"runtime"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/robfig/cron/v3"
)

// setupPeriodicTasks 创建并启动定时任务调度器。
//
// 包含以下定时任务：
//   - 每 5 分钟：打印 GC 堆内存和 goroutine 数量统计
//   - 每 10 分钟：打印汇总统计（链数、池子数等）
//
// 调用方需在退出前调用返回的 cron 实例的 Stop() 方法以优雅关闭。
func setupPeriodicTasks(logger logx.Logger, numChains, numPools int) *cron.Cron {
	cronLogger := cron.VerbosePrintfLogger(log.New(os.Stderr, "[cron] ", log.LstdFlags))
	scheduler := cron.New(
		cron.WithSeconds(),
		cron.WithLogger(cronLogger),
	)

	// 每 5 分钟打印 GC 统计
	_, _ = scheduler.AddFunc("0 */5 * * * *", func() {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		logger.Debug("gc stats",
			"heapMB", m.HeapInuse/1024/1024,
			"goroutines", runtime.NumGoroutine(),
		)
	})

	// 每 10 分钟打印汇总统计
	_, _ = scheduler.AddFunc("0 */10 * * * *", func() {
		logger.Info("periodic stats",
			"chains", numChains,
			"pools", numPools,
			"goroutines", runtime.NumGoroutine(),
		)
	})

	scheduler.Start()
	return scheduler
}
