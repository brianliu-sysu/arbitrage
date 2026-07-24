package app

import (
	"flag"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
)

type Params struct {
	ConfigPath string
}

// ParseFlags parses standard CLI flags for the arbitrage binary.
func ParseFlags() Params {
	configPath := flag.String("config", config.DefaultPath, "path to config yaml")
	flag.Parse()
	return Params{ConfigPath: *configPath}
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
	normalizedChains := cfg.NormalizedChains()
	if len(normalizedChains) == 0 {
		return config.Config{}, fmt.Errorf("at least one chain must be enabled")
	}
	for _, chain := range normalizedChains {
		if chain.RPC.URL == "" {
			return config.Config{}, fmt.Errorf("chains[%d]: rpc.url is required", chain.ChainID)
		}
		if chain.RPC.WSURL == "" {
			return config.Config{}, fmt.Errorf("chains[%d]: rpc.ws_url is required for block head subscription", chain.ChainID)
		}
	}
	return cfg, nil
}
