package runtime

import (
	"context"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
)

type bootstrapTask struct {
	name         string
	bootstrapper protocolBootstrapper
}

type protocolBootstrapper interface {
	StartBootstrapAt(context.Context, blockchain.BlockHeader) error
}

type protocolBootstrapPlan struct {
	tasks []bootstrapTask
}

func (p *protocolBootstrapPlan) add(name string, bootstrapper protocolBootstrapper) {
	if p != nil && bootstrapper != nil {
		p.tasks = append(p.tasks, bootstrapTask{name: name, bootstrapper: bootstrapper})
	}
}

func (p *protocolBootstrapPlan) hasAny() bool {
	return p != nil && len(p.tasks) > 0
}
