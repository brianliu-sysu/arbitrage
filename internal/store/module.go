package store

import (
	"github.com/brianliu-sysu/arbitrage/internal/storage/postgres"
	"go.uber.org/fx"
)

// Module 提供 legacy Storer 接口（适配 storage Repository）。
var Module = fx.Module(
	"store",
	fx.Provide(newStorerFromRepos),
)

func newStorerFromRepos(repos *postgres.Repositories) Storer {
	if repos == nil {
		return NewNoopStore()
	}
	return newStorerAdapter(repos.Pool)
}
