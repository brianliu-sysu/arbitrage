package config

import "go.uber.org/fx"

const DefaultPath = "configs/config.yaml"

func New(cfg Config) Config {
	return cfg
}

func LoadModule(configPath string) fx.Option {
	path := configPath
	if path == "" {
		path = DefaultPath
	}
	return fx.Provide(func() (Config, error) {
		return Load(path)
	})
}
