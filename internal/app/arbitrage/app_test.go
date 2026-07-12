package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunBootstrapTasksReturnsProtocolFailure(t *testing.T) {
	err := runBootstrapTasks(context.Background(), []bootstrapTask{
		{name: "univ3", run: func(context.Context) error { return nil }},
		{name: "univ4", run: func(context.Context) error { return errors.New("rpc unavailable") }},
	})
	if err == nil || !strings.Contains(err.Error(), "univ4 bootstrap") {
		t.Fatalf("expected named bootstrap failure, got %v", err)
	}
}

func TestRunBootstrapTasksConvertsPanicToError(t *testing.T) {
	err := runBootstrapTasks(context.Background(), []bootstrapTask{{
		name: "balancer",
		run:  func(context.Context) error { panic("boom") },
	}})
	if err == nil || !strings.Contains(err.Error(), "balancer bootstrap panicked") {
		t.Fatalf("expected bootstrap panic error, got %v", err)
	}
}
