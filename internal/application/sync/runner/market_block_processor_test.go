package runner_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/runner"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
)

type blockPreparerFunc func(context.Context, blockchain.BlockHeader, []blockchain.RawLog) (syncapp.PreparedBlock, error)

func (f blockPreparerFunc) PrepareBlock(ctx context.Context, head blockchain.BlockHeader, logs []blockchain.RawLog) (syncapp.PreparedBlock, error) {
	return f(ctx, head, logs)
}

func TestMarketBlockProcessorRejectsNilPreparedBlock(t *testing.T) {
	processor := syncapp.NewMarketBlockProcessor([]syncapp.NamedHeadHandler{{
		Name: "univ3",
		Handler: blockPreparerFunc(func(context.Context, blockchain.BlockHeader, []blockchain.RawLog) (syncapp.PreparedBlock, error) {
			return nil, nil
		}),
	}})

	_, err := processor.Process(context.Background(), blockchain.BlockHeader{Number: 1}, nil)
	if err == nil || !strings.Contains(err.Error(), "prepare protocol univ3 returned nil block") {
		t.Fatalf("expected nil prepared block error, got %v", err)
	}
}

func TestMarketBlockProcessorRollsBackAfterApplyContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var rollbackContextErr error
	first := blockPreparerFunc(func(context.Context, blockchain.BlockHeader, []blockchain.RawLog) (syncapp.PreparedBlock, error) {
		return &preparedBlockFunc{
			apply: func(context.Context) error { return nil },
			rollback: func(ctx context.Context) error {
				rollbackContextErr = ctx.Err()
				return nil
			},
		}, nil
	})
	second := blockPreparerFunc(func(context.Context, blockchain.BlockHeader, []blockchain.RawLog) (syncapp.PreparedBlock, error) {
		return &preparedBlockFunc{
			apply: func(context.Context) error {
				cancel()
				return errors.New("apply failed")
			},
		}, nil
	})
	processor := syncapp.NewMarketBlockProcessor([]syncapp.NamedHeadHandler{
		{Name: "univ3", Handler: first},
		{Name: "pancakev3", Handler: second},
	})

	if _, err := processor.Process(ctx, blockchain.BlockHeader{Number: 1}, nil); err == nil {
		t.Fatal("expected apply failure")
	}
	if rollbackContextErr != nil {
		t.Fatalf("rollback inherited canceled apply context: %v", rollbackContextErr)
	}
}
