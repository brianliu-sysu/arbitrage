package config

import (
	"fmt"

	"go.uber.org/fx"
)

// Module 提供应用配置加载。
var Module = fx.Module(
	"config",
	fx.Provide(
		fx.Annotate(
			loadAndValidate,
			fx.ParamTags(`name:"config_path"`),
		),
	),
)

func loadAndValidate(path string) (*AppConfig, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	return cfg, nil
}
