package app

import (
	"context"
	"testing"
)

func TestApplicationLifecycleIsIdempotent(t *testing.T) {
	application := &Application{}
	if err := application.Start(context.Background()); err != nil {
		t.Fatalf("start application: %v", err)
	}
	if err := application.Start(context.Background()); err != nil {
		t.Fatalf("start application twice: %v", err)
	}
	if err := application.Stop(context.Background()); err != nil {
		t.Fatalf("stop application: %v", err)
	}
	if err := application.Stop(context.Background()); err != nil {
		t.Fatalf("stop application twice: %v", err)
	}
}

func TestApplicationCannotRestartAfterStop(t *testing.T) {
	application := &Application{}
	if err := application.Stop(context.Background()); err != nil {
		t.Fatalf("stop application: %v", err)
	}
	if err := application.Start(context.Background()); err == nil {
		t.Fatal("expected restarting stopped application to fail")
	}
}
