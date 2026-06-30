package pool

import "go.uber.org/fx"

// Module 提供池子领域依赖。
var Module = fx.Module(
	"pool",
	fx.Provide(NewCache),
)
