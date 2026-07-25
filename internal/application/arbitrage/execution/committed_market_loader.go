package execution

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/application/committedmarket"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
)

// CommittedMarketRoutePoolLoader loads execution state from one committed snapshot.
type CommittedMarketRoutePoolLoader struct {
	market committedmarket.Reader
}

func NewCommittedMarketRoutePoolLoader(market committedmarket.Reader) *CommittedMarketRoutePoolLoader {
	return &CommittedMarketRoutePoolLoader{market: market}
}

func (l *CommittedMarketRoutePoolLoader) LoadRoutePools(
	ctx context.Context,
	route quoteunified.Route,
) (quoteunified.RoutePools, error) {
	if l == nil || l.market == nil {
		return quoteunified.RoutePools{}, fmt.Errorf("committed market is not configured")
	}
	snapshot := l.market.Snapshot()
	if snapshot == nil || snapshot.Version().IsZero() {
		return quoteunified.RoutePools{}, fmt.Errorf("committed market snapshot is not available")
	}
	return snapshot.LoadRoutePools(ctx, route)
}
