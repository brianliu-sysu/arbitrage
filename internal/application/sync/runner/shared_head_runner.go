package runner

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
)

// BlockHandler consumes one canonical head with logs fetched by the shared runner.
// SharedHeadRunner subscribes once to new heads and fans each head out to every
// enabled protocol before advancing. This keeps cross-protocol pool state aligned
// at the same block before arbitrage generation.
type SharedHeadRunner struct {
	subscriber  HeadSubscriber
	logger      *zap.Logger
	coordinator HeadCoordinator
	logFetcher  HeadLogFetcher
	processor   *MarketBlockProcessor
	blocks      CanonicalBlockReader
	history     *blockHistory
	reorgDepth  uint64
	// applyMu serializes the complete prepare/apply/finalize transaction for each head.
	applyMu   sync.Mutex
	localHead blockchain.BlockHeader

	reconnectInitialDelay time.Duration
	reconnectMaxDelay     time.Duration
}

type SharedHeadDependencies struct {
	Subscriber  HeadSubscriber
	LogFetcher  HeadLogFetcher
	Blocks      CanonicalBlockReader
	Coordinator HeadCoordinator
}

type HeadCoordinator interface {
	PrepareHead(context.Context, blockchain.BlockHeader) error
	FinalizeHead(context.Context, blockchain.BlockHeader) error
}

type headApplyResult struct {
	logCount  int
	protocols []string
}

func (r *SharedHeadRunner) InitializeLocalHead(ctx context.Context, head blockchain.BlockHeader) error {
	if r == nil {
		return nil
	}
	r.applyMu.Lock()
	defer r.applyMu.Unlock()
	fromBlock := uint64(0)
	if head.Number > r.reorgDepth {
		fromBlock = head.Number - r.reorgDepth
	}
	blockNumbers := blockRange(fromBlock, head.Number)
	headers, err := r.blocks.GetBlockHeaders(ctx, blockNumbers)
	if err != nil {
		return fmt.Errorf("initialize local block history: %w", err)
	}
	canonicalHead, ok := headers[head.Number]
	if !ok {
		return fmt.Errorf("initialize local block history: head %d is missing", head.Number)
	}
	if canonicalHead.Hash != head.Hash {
		return fmt.Errorf("initialize local block history: head %d hash changed", head.Number)
	}
	r.history.Reset(headers)
	r.localHead = canonicalHead
	return nil
}

func NewSharedHeadRunner(
	deps SharedHeadDependencies,
	handlers []NamedHeadHandler,
	reorgDepth uint64,
	logger *zap.Logger,
) (*SharedHeadRunner, error) {
	if deps.Subscriber == nil {
		return nil, errors.New("shared head subscriber is required")
	}
	if deps.LogFetcher == nil {
		return nil, errors.New("shared head log fetcher is required")
	}
	if deps.Blocks == nil {
		return nil, errors.New("shared head block reader is required")
	}
	if reorgDepth == 0 {
		return nil, errors.New("shared head reorg depth must be greater than zero")
	}
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
	if len(out) == 0 {
		return nil, errors.New("at least one shared head handler is required")
	}
	return &SharedHeadRunner{
		subscriber:            deps.Subscriber,
		logger:                logger,
		coordinator:           deps.Coordinator,
		logFetcher:            deps.LogFetcher,
		processor:             NewMarketBlockProcessor(out),
		blocks:                deps.Blocks,
		history:               newBlockHistory(reorgDepth),
		reorgDepth:            reorgDepth,
		reconnectInitialDelay: time.Second,
		reconnectMaxDelay:     30 * time.Second,
	}, nil
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

// Subscribe establishes the initial head stream, retrying transient
// subscription failures until the context is canceled.
func (r *SharedHeadRunner) Subscribe(ctx context.Context) (<-chan blockchain.BlockHeader, error) {
	if r == nil {
		return nil, nil
	}
	return r.subscribeWithBackoff(ctx, false)
}

func (r *SharedHeadRunner) subscribeWithBackoff(
	ctx context.Context,
	waitBeforeFirstAttempt bool,
) (<-chan blockchain.BlockHeader, error) {
	delay := r.reconnectInitialDelay
	waitBeforeAttempt := waitBeforeFirstAttempt
	for {
		if waitBeforeAttempt {
			if err := waitForReconnect(ctx, delay); err != nil {
				return nil, err
			}
			delay = nextReconnectDelay(delay, r.reconnectMaxDelay)
		}
		heads, err := r.subscriber.SubscribeNewHead(ctx)
		if err == nil {
			return heads, nil
		}
		waitBeforeAttempt = true
	}
}

// Run blocks while streaming heads and applying them across all handlers.
func (r *SharedHeadRunner) Run(ctx context.Context) error {
	heads, err := r.Subscribe(ctx)
	if err != nil {
		return err
	}
	return r.RunSubscribed(ctx, heads, nil)
}

// RunSubscribed consumes an already-established stream. Establishing the
// stream before the final latest-head read closes the startup subscription
// race. It aligns to the latest head before reporting readiness; closed streams
// are still reconnected with backoff.
func (r *SharedHeadRunner) RunSubscribed(
	ctx context.Context,
	heads <-chan blockchain.BlockHeader,
	onReady func(),
) error {
	if r == nil {
		return nil
	}
	if heads == nil {
		return fmt.Errorf("shared head subscription is not configured")
	}
	current, err := r.blocks.GetLatestBlockHeader(ctx)
	if err != nil {
		return fmt.Errorf("load initial shared head: %w", err)
	}
	if current.Number > 0 {
		if err := r.HandleHead(ctx, current); err != nil {
			return fmt.Errorf("apply initial shared head: %w", err)
		}
	}
	if onReady != nil {
		onReady()
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case head, ok := <-heads:
			if !ok {
				heads, err = r.subscribeWithBackoff(ctx, true)
				if err != nil {
					return err
				}
				continue
			}
			if err := r.applyHeadSoft(ctx, head); err != nil {
				return err
			}
		}
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
	if r == nil {
		return nil
	}
	r.applyMu.Lock()
	defer r.applyMu.Unlock()
	return r.handleHeadLocked(ctx, head)
}

func (r *SharedHeadRunner) handleHeadLocked(ctx context.Context, head blockchain.BlockHeader) error {
	if ShouldSkipHeadNotification(r.localHead, head) {
		return nil
	}
	started := time.Now()
	if err := r.prepareHead(ctx, head); err != nil {
		return err
	}
	if err := r.catchUpHeadGap(ctx, head); err != nil {
		return err
	}
	recovered, err := r.recoverReorg(ctx, head)
	if err != nil {
		return err
	}
	if recovered {
		if err := r.finalizeHead(ctx, head); err != nil {
			return fmt.Errorf("finalize recovered shared head %d: %w", head.Number, err)
		}
		r.localHead = head
		return nil
	}
	result, err := r.applyHead(ctx, head)
	if err != nil {
		return err
	}
	if err := r.finalizeHead(ctx, head); err != nil {
		return fmt.Errorf("finalize shared head %d: %w", head.Number, err)
	}
	r.localHead = head
	r.history.Commit(head)
	r.logAppliedHead(head, result, time.Since(started))
	return nil
}

func (r *SharedHeadRunner) prepareHead(ctx context.Context, head blockchain.BlockHeader) error {
	if r.coordinator == nil {
		return nil
	}
	if err := r.coordinator.PrepareHead(ctx, head); err != nil {
		return fmt.Errorf("prepare shared head %d: %w", head.Number, err)
	}
	return nil
}

func (r *SharedHeadRunner) catchUpHeadGap(ctx context.Context, head blockchain.BlockHeader) error {
	if !NeedsHeadGapCatchup(r.localHead, head) {
		return nil
	}
	fromBlock := r.localHead.Number + 1
	toBlock := head.Number - 1
	gapHead, err := r.processCanonicalRange(ctx, fromBlock, toBlock)
	if err != nil {
		return fmt.Errorf("catch up shared head gap %d -> %d: %w", r.localHead.Number, head.Number, err)
	}
	r.localHead = gapHead
	return nil
}

func (r *SharedHeadRunner) recoverReorg(ctx context.Context, head blockchain.BlockHeader) (bool, error) {
	if r.localHead.Number == 0 {
		return false, nil
	}
	reorg, err := DetectReorg(ctx, r.blocks, r.history, r.reorgDepth, r.localHead, head)
	if err != nil {
		return false, fmt.Errorf("detect shared reorg: %w", err)
	}
	if reorg == nil {
		return false, nil
	}
	replayed, err := r.recoverCanonicalBranch(ctx, *reorg)
	if err != nil {
		return false, fmt.Errorf("recover shared reorg: %w", err)
	}
	r.history.ReplaceAfter(reorg.CommonAncestor, replayed)
	return true, nil
}

func (r *SharedHeadRunner) applyHead(ctx context.Context, head blockchain.BlockHeader) (headApplyResult, error) {
	logs, err := r.logFetcher.FetchBlockLogs(ctx, head.Hash)
	if err != nil {
		return headApplyResult{}, fmt.Errorf("fetch shared logs for head %d: %w", head.Number, err)
	}
	applied, err := r.processor.Process(ctx, head, logs)
	if err != nil {
		return headApplyResult{}, fmt.Errorf("process shared head %d: %w", head.Number, err)
	}
	return headApplyResult{logCount: len(logs), protocols: applied}, nil
}

func (r *SharedHeadRunner) finalizeHead(ctx context.Context, head blockchain.BlockHeader) error {
	if r.coordinator == nil {
		return nil
	}
	return r.coordinator.FinalizeHead(ctx, head)
}

func (r *SharedHeadRunner) logAppliedHead(head blockchain.BlockHeader, result headApplyResult, duration time.Duration) {
	r.logger.Debug("shared head applied",
		zap.Uint64("block", head.Number),
		zap.String("hash", head.Hash.Hex()),
		zap.Int("logs", result.logCount),
		zap.Strings("protocols", result.protocols),
		zap.Int64("duration_ms", duration.Milliseconds()),
	)
}

func (r *SharedHeadRunner) processCanonicalRange(
	ctx context.Context,
	fromBlock, toBlock uint64,
) (blockchain.BlockHeader, error) {
	headers, err := r.blocks.GetBlockHeaders(ctx, blockRange(fromBlock, toBlock))
	if err != nil {
		return blockchain.BlockHeader{}, fmt.Errorf("load canonical block headers %d-%d: %w", fromBlock, toBlock, err)
	}
	var last blockchain.BlockHeader
	for blockNumber := fromBlock; blockNumber <= toBlock; blockNumber++ {
		head, ok := headers[blockNumber]
		if !ok {
			return blockchain.BlockHeader{}, fmt.Errorf("canonical block header %d is missing from batch", blockNumber)
		}
		logs, err := r.logFetcher.FetchBlockLogs(ctx, head.Hash)
		if err != nil {
			return blockchain.BlockHeader{}, fmt.Errorf("fetch canonical block %d logs: %w", blockNumber, err)
		}
		if _, err := r.processor.Process(ctx, head, logs); err != nil {
			return blockchain.BlockHeader{}, fmt.Errorf("process canonical block %d: %w", blockNumber, err)
		}
		r.history.Commit(head)
		last = head
	}
	return last, nil
}

func (r *SharedHeadRunner) recoverCanonicalBranch(ctx context.Context, reorg blockchain.Reorg) ([]blockchain.BlockHeader, error) {
	recovery, err := r.processor.prepareReorg(ctx, reorg)
	if err != nil {
		return nil, err
	}
	rollback := func(cause error) error {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), preparedBlockRollbackTimeout)
		defer cancel()
		return errors.Join(cause, recovery.Rollback(rollbackCtx))
	}
	branchNumbers := blockRange(reorg.CommonAncestor+1, reorg.RemoteHead.Number)
	headers, err := r.blocks.GetBlockHeaders(ctx, branchNumbers)
	if err != nil {
		return nil, rollback(fmt.Errorf("load recovery block headers: %w", err))
	}
	branch := make([]blockchain.BlockHeader, 0, len(branchNumbers))
	for _, blockNumber := range branchNumbers {
		head, ok := headers[blockNumber]
		if !ok {
			return nil, rollback(fmt.Errorf("recovery block header %d is missing from batch", blockNumber))
		}
		branch = append(branch, head)
	}
	for blockNumber := recovery.ReplayFrom(); blockNumber <= reorg.RemoteHead.Number; blockNumber++ {
		head, ok := headers[blockNumber]
		if !ok {
			return nil, rollback(fmt.Errorf("recovery block header %d is missing from batch", blockNumber))
		}
		logs, err := r.logFetcher.FetchBlockLogs(ctx, head.Hash)
		if err != nil {
			return nil, rollback(fmt.Errorf("fetch recovery block %d logs: %w", blockNumber, err))
		}
		if err := recovery.Process(ctx, head, logs); err != nil {
			return nil, rollback(fmt.Errorf("process recovery block %d: %w", blockNumber, err))
		}
	}
	if err := recovery.Commit(ctx); err != nil {
		return nil, rollback(err)
	}
	return branch, nil
}

func blockRange(fromBlock, toBlock uint64) []uint64 {
	if fromBlock > toBlock {
		return nil
	}
	blockNumbers := make([]uint64, 0, toBlock-fromBlock+1)
	for blockNumber := fromBlock; blockNumber <= toBlock; blockNumber++ {
		blockNumbers = append(blockNumbers, blockNumber)
	}
	return blockNumbers
}

func waitForReconnect(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nextReconnectDelay(current, max time.Duration) time.Duration {
	if current <= 0 {
		current = time.Second
	}
	next := current * 2
	if next > max {
		return max
	}
	return next
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
