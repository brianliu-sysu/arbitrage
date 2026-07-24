package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
)

type stubProtocolBootstrapper struct {
	err      error
	panicRun bool
}

func (s *stubProtocolBootstrapper) StartBootstrapAt(context.Context, blockchain.BlockHeader) error {
	if s.panicRun {
		panic("boom")
	}
	return s.err
}

func TestRunBootstrapTasksReturnsProtocolFailure(t *testing.T) {
	err := runBootstrapTasks(context.Background(), blockchain.BlockHeader{Number: 10}, []bootstrapTask{
		{name: "univ3", bootstrapper: &stubProtocolBootstrapper{}},
		{name: "univ4", bootstrapper: &stubProtocolBootstrapper{err: errors.New("rpc unavailable")}},
	})
	if err == nil || !strings.Contains(err.Error(), "univ4 bootstrap") {
		t.Fatalf("expected named bootstrap failure, got %v", err)
	}
}

func TestRunBootstrapTasksConvertsPanicToError(t *testing.T) {
	err := runBootstrapTasks(context.Background(), blockchain.BlockHeader{Number: 10}, []bootstrapTask{{
		name:         "balancer",
		bootstrapper: &stubProtocolBootstrapper{panicRun: true},
	}})
	if err == nil || !strings.Contains(err.Error(), "balancer bootstrap panicked") {
		t.Fatalf("expected bootstrap panic error, got %v", err)
	}
}
