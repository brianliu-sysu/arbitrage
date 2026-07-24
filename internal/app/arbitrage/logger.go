package app

import (
	"github.com/brianliu-sysu/uniswapv3/internal/config"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/logging"
	"go.uber.org/zap"
)

func newLogger(cfg config.Config) (*zap.Logger, error) {
	return logging.New(cfg.Log)
}
