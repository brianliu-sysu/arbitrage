package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New builds a zap logger from application log config.
func New(cfg config.LogConfig) (*zap.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	encoding := "console"
	if strings.EqualFold(strings.TrimSpace(cfg.Format), "json") {
		encoding = "json"
	}

	outputPaths := cfg.ResolvedOutputPaths()
	errorOutputPaths := cfg.ResolvedErrorOutputPaths()
	for _, path := range append(append([]string{}, outputPaths...), errorOutputPaths...) {
		if err := ensureLogPath(path); err != nil {
			return nil, err
		}
	}

	zapCfg := zap.Config{
		Level:            zap.NewAtomicLevelAt(level),
		Development:      encoding == "console",
		Encoding:         encoding,
		OutputPaths:      outputPaths,
		ErrorOutputPaths: errorOutputPaths,
		EncoderConfig:    encoderConfig(encoding),
	}

	logger, err := zapCfg.Build()
	if err != nil {
		return nil, fmt.Errorf("build logger: %w", err)
	}
	return logger, nil
}

func parseLevel(level string) (zapcore.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		return zapcore.InfoLevel, nil
	case "debug":
		return zapcore.DebugLevel, nil
	case "warn", "warning":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	default:
		return zapcore.InfoLevel, fmt.Errorf("unsupported log level %q", level)
	}
}

func encoderConfig(encoding string) zapcore.EncoderConfig {
	if encoding == "json" {
		return zap.NewProductionEncoderConfig()
	}
	cfg := zap.NewDevelopmentEncoderConfig()
	cfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	return cfg
}

func ensureLogPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || path == "stdout" || path == "stderr" {
		return nil
	}

	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create log directory %s: %w", dir, err)
	}
	return nil
}
