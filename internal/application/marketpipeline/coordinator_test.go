package marketpipeline

import (
	"context"
	"errors"
	"testing"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/marketchange"
	"github.com/ethereum/go-ethereum/common"
)

type recordingPublisher struct {
	versions []domainchain.MarketVersion
	err      error
}

func (p *recordingPublisher) Publish(_ context.Context, version domainchain.MarketVersion, _ marketchange.Changes) error {
	p.versions = append(p.versions, version)
	return p.err
}

func TestCoordinatorWaitsForEveryEnabledProtocol(t *testing.T) {
	publisher := &recordingPublisher{}
	coordinator := NewCoordinator([]Protocol{ProtocolUniv3, ProtocolUniv4}, publisher, nil)
	head := domainchain.BlockHeader{Number: 12, Hash: common.HexToHash("0x12")}
	coordinator.PrepareHead(head)

	reportBlock(t, coordinator, ProtocolUniv3, head.Number)
	if _, _, committed := coordinator.CommitHead(context.Background(), head); committed {
		t.Fatal("committed before all protocols reported")
	}

	reportBlock(t, coordinator, ProtocolUniv4, head.Number)
	version, _, committed := coordinator.CommitHead(context.Background(), head)
	if !committed || version.Number != head.Number || len(publisher.versions) != 1 {
		t.Fatalf("unexpected commit: committed=%v version=%+v publishes=%d", committed, version, len(publisher.versions))
	}
}

func TestCoordinatorKeepsPendingBlockWhenPublishFails(t *testing.T) {
	publisher := &recordingPublisher{err: errors.New("publish failed")}
	coordinator := NewCoordinator([]Protocol{ProtocolUniv3}, publisher, nil)
	head := domainchain.BlockHeader{Number: 13, Hash: common.HexToHash("0x13")}
	coordinator.PrepareHead(head)
	reportBlock(t, coordinator, ProtocolUniv3, head.Number)

	if _, _, committed := coordinator.CommitHead(context.Background(), head); committed {
		t.Fatal("failed publish must not commit")
	}
	publisher.err = nil
	if _, _, committed := coordinator.CommitHead(context.Background(), head); !committed {
		t.Fatal("retry publish did not commit")
	}
}

func TestCoordinatorRecommitsSameHeightAfterHashChanges(t *testing.T) {
	publisher := &recordingPublisher{}
	coordinator := NewCoordinator([]Protocol{ProtocolUniv3}, publisher, nil)
	headA := domainchain.BlockHeader{Number: 20, Hash: common.HexToHash("0xa20")}
	coordinator.PrepareHead(headA)
	reportBlock(t, coordinator, ProtocolUniv3, headA.Number)
	if _, _, committed := coordinator.CommitHead(context.Background(), headA); !committed {
		t.Fatal("first hash did not commit")
	}

	headB := domainchain.BlockHeader{Number: 20, Hash: common.HexToHash("0xb20")}
	coordinator.PrepareHead(headB)
	reportBlock(t, coordinator, ProtocolUniv3, headB.Number)
	if _, _, committed := coordinator.CommitHead(context.Background(), headB); !committed {
		t.Fatal("replacement hash did not commit")
	}
	if len(publisher.versions) != 2 || publisher.versions[1].Generation <= publisher.versions[0].Generation {
		t.Fatalf("expected a newer generation, got %+v", publisher.versions)
	}
}

func reportBlock(t *testing.T, coordinator *Coordinator, protocol Protocol, blockNumber uint64) {
	t.Helper()
	if err := coordinator.ReportApplied(context.Background(), ProtocolBlockReport{
		Protocol: protocol, BlockNumber: blockNumber,
	}); err != nil {
		t.Fatalf("report block: %v", err)
	}
}
