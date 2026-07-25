package committedmarket

import (
	"context"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
)

// Snapshot is one immutable, internally consistent market version.
type Snapshot interface {
	Version() domainchain.MarketVersion
	LoadRoutePools(context.Context, quoteunified.Route) (quoteunified.RoutePools, error)
}

// Reader captures the currently committed market snapshot.
type Reader interface {
	Snapshot() Snapshot
}
