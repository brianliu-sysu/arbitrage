package logging_test

import (
	"runtime"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/logging"
)

func TestNewLoggerStdout(t *testing.T) {
	logger, err := logging.New(config.LogConfig{
		Level:  "debug",
		Format: "json",
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	defer logger.Sync()

	logger.Info("test log entry")
}

func TestNewLoggerWritesToFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("zap keeps log file handles open on Windows until process exit")
	}

	dir := t.TempDir()
	logFile := dir + "/arbitrage.log"

	logger, err := logging.New(config.LogConfig{
		Level:  "info",
		Format: "json",
		File:   logFile,
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	logger.Info("test log entry")
	if err := logger.Sync(); err != nil {
		t.Fatalf("sync logger: %v", err)
	}
}

func TestNewLoggerRejectsInvalidLevel(t *testing.T) {
	_, err := logging.New(config.LogConfig{Level: "invalid"})
	if err == nil {
		t.Fatal("expected invalid level error")
	}
}
