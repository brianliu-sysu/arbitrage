package syncapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// HeadHandler applies one canonical head for a single sync protocol.
type HeadHandler interface {
	HandleHead(ctx context.Context, head blockchain.BlockHeader) error
}

// NamedHeadHandler pairs a protocol label with its head handler.
type NamedHeadHandler struct {
	Name    string
	Handler HeadHandler
}

// SharedHeadRunner subscribes once to new heads and fans each head out to every
// enabled protocol before advancing. This keeps cross-protocol pool state aligned
// at the same block before arbitrage generation.
type SharedHeadRunner struct {
	subscriber HeadSubscriber
	handlers   []NamedHeadHandler
	logger     *zap.Logger
	beforeHead func(context.Context, blockchain.BlockHeader) error

	reconnectInitialDelay time.Duration
	reconnectMaxDelay     time.Duration
}

// SetBeforeHead configures a hook that runs before any protocol mutates state for a new head.
func (r *SharedHeadRunner) SetBeforeHead(hook func(context.Context, blockchain.BlockHeader) error) {
	if r == nil {
		return
	}
	r.beforeHead = hook
}

func NewSharedHeadRunner(subscriber HeadSubscriber, handlers []NamedHeadHandler, logger *zap.Logger) *SharedHeadRunner {
	if logger == nil {
		logger = zap.NewNop()
	}
	out := make([]NamedHeadHandler, 0, len(handlers))
	for _, handler := range handlers {
		if handler.Handler == nil || handler.Name == "" {
			continue
		}
		out = append(out, handler)
	}
	return &SharedHeadRunner{
		subscriber:            subscriber,
		handlers:              out,
		logger:                logger,
		reconnectInitialDelay: time.Second,
		reconnectMaxDelay:     30 * time.Second,
	}
}

func (r *SharedHeadRunner) SetReconnectBackoff(initial, max time.Duration) {
	if initial <= 0 {
		initial = time.Second
	}
	if max <= 0 || max < initial {
		max = initial
	}
	r.reconnectInitialDelay = initial
	r.reconnectMaxDelay = max
}

// Run blocks while streaming heads and applying them across all handlers.
func (r *SharedHeadRunner) Run(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if r.subscriber == nil {
		return fmt.Errorf("shared head subscriber is not configured")
	}
	if len(r.handlers) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	delay := r.reconnectInitialDelay
	for {
		heads, err := r.subscriber.SubscribeNewHead(ctx)
		if err != nil {
			if waitErr := waitForReconnect(ctx, delay); waitErr != nil {
				return waitErr
			}
			delay = nextReconnectDelay(delay, r.reconnectMaxDelay)
			continue
		}
		delay = r.reconnectInitialDelay

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case head, ok := <-heads:
				if !ok {
					if waitErr := waitForReconnect(ctx, delay); waitErr != nil {
						return waitErr
					}
					delay = nextReconnectDelay(delay, r.reconnectMaxDelay)
					goto reconnect
				}
				delay = r.reconnectInitialDelay
				if err := r.applyHeadSoft(ctx, head); err != nil {
					return err
				}
			}
		}
	reconnect:
	}
}

// applyHeadSoft applies a head and soft-fails transient RPC errors so sync keeps running.
func (r *SharedHeadRunner) applyHeadSoft(ctx context.Context, head blockchain.BlockHeader) error {
	err := r.HandleHead(ctx, head)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if isTransientApplyError(err) {
		r.logger.Warn("shared head apply failed, retrying",
			zap.Uint64("block", head.Number),
			zap.String("hash", head.Hash.Hex()),
			zap.Error(err),
		)
		if retryErr := r.HandleHead(ctx, head); retryErr == nil {
			return nil
		} else if ctx.Err() != nil {
			return ctx.Err()
		} else {
			err = retryErr
		}
	}
	r.logger.Warn("shared head apply failed, skipping block",
		zap.Uint64("block", head.Number),
		zap.String("hash", head.Hash.Hex()),
		zap.Error(err),
	)
	return nil
}

// HandleHead applies one head to every protocol handler and waits for all to finish.
func (r *SharedHeadRunner) HandleHead(ctx context.Context, head blockchain.BlockHeader) error {
	if r == nil || len(r.handlers) == 0 {
		return nil
	}
	started := time.Now()
	if r.beforeHead != nil {
		if err := r.beforeHead(ctx, head); err != nil {
			return fmt.Errorf("prepare shared head %d: %w", head.Number, err)
		}
	}

	group, groupCtx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	applied := make([]string, 0, len(r.handlers))
	for _, handler := range r.handlers {
		handler := handler
		group.Go(func() error {
			if err := handler.Handler.HandleHead(groupCtx, head); err != nil {
				return fmt.Errorf("%s: %w", handler.Name, err)
			}
			mu.Lock()
			applied = append(applied, handler.Name)
			mu.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return fmt.Errorf("apply shared head %d: %w", head.Number, err)
	}
	r.logger.Debug("shared head applied",
		zap.Uint64("block", head.Number),
		zap.String("hash", head.Hash.Hex()),
		zap.Strings("protocols", applied),
		zap.Int64("duration_ms", time.Since(started).Milliseconds()),
	)
	return nil
}

func isTransientApplyError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"unexpected eof",
		"connection reset",
		"connection refused",
		"broken pipe",
		"i/o timeout",
		"deadline exceeded",
		"tls handshake timeout",
		"server closed idle connection",
		"http2: client connection lost",
		"too many requests",
		"429",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
