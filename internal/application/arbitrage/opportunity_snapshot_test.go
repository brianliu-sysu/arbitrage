package arbitrageapp

import (
	"context"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/application/committedmarket"
	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
)

type countingMarketReader struct {
	snapshot committedmarket.Snapshot
	calls    int
}

func (r *countingMarketReader) Snapshot() committedmarket.Snapshot {
	r.calls++
	return r.snapshot
}

type fixedMarketSnapshot struct {
	version domainchain.MarketVersion
}

func (s *fixedMarketSnapshot) Version() domainchain.MarketVersion {
	return s.version
}

func (s *fixedMarketSnapshot) LoadRoutePools(
	context.Context,
	quoteunified.Route,
) (quoteunified.RoutePools, error) {
	return quoteunified.RoutePools{}, nil
}

func TestOpportunityServiceCapturesOneSnapshotPerGenerate(t *testing.T) {
	version := domainchain.MarketVersion{Number: 10, Generation: 1}
	market := &countingMarketReader{snapshot: &fixedMarketSnapshot{version: version}}
	service := NewOpportunityService(market, nil, nil, nil, nil, nil, 0, nil, nil)

	if _, err := service.Generate(context.Background(), GenerateRequest{Version: version}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if market.calls != 1 {
		t.Fatalf("expected one snapshot capture, got %d", market.calls)
	}
}

func TestOpportunityServiceRejectsDifferentSnapshotVersion(t *testing.T) {
	market := &countingMarketReader{snapshot: &fixedMarketSnapshot{
		version: domainchain.MarketVersion{Number: 11, Generation: 2},
	}}
	service := NewOpportunityService(market, nil, nil, nil, nil, nil, 0, nil, nil)

	if _, err := service.Generate(context.Background(), GenerateRequest{
		Version: domainchain.MarketVersion{Number: 10, Generation: 1},
	}); err == nil {
		t.Fatal("expected market version mismatch")
	}
}

func TestOpportunityServiceRequiresSnapshotVersion(t *testing.T) {
	market := &countingMarketReader{snapshot: &fixedMarketSnapshot{
		version: domainchain.MarketVersion{Number: 10, Generation: 1},
	}}
	service := NewOpportunityService(market, nil, nil, nil, nil, nil, 0, nil, nil)

	if _, err := service.Generate(context.Background(), GenerateRequest{}); err == nil {
		t.Fatal("expected missing market version to be rejected")
	}
}
