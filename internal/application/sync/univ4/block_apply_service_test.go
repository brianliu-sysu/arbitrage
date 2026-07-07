package syncv4

import (
	"context"
	"math/big"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

type memoryV4CheckpointRepo struct {
	checkpoints map[marketv4.PoolID]*blockchain.V4Checkpoint
}

func newMemoryV4CheckpointRepo() *memoryV4CheckpointRepo {
	return &memoryV4CheckpointRepo{checkpoints: make(map[marketv4.PoolID]*blockchain.V4Checkpoint)}
}

func (r *memoryV4CheckpointRepo) Save(_ context.Context, checkpoint *blockchain.V4Checkpoint) error {
	r.checkpoints[checkpoint.PoolID] = checkpoint
	return nil
}

func (r *memoryV4CheckpointRepo) SaveMany(ctx context.Context, checkpoints []*blockchain.V4Checkpoint) error {
	for _, checkpoint := range checkpoints {
		if err := r.Save(ctx, checkpoint); err != nil {
			return err
		}
	}
	return nil
}

func (r *memoryV4CheckpointRepo) Get(_ context.Context, id marketv4.PoolID) (*blockchain.V4Checkpoint, error) {
	return r.checkpoints[id], nil
}

func (r *memoryV4CheckpointRepo) Delete(_ context.Context, id marketv4.PoolID) error {
	delete(r.checkpoints, id)
	return nil
}

func TestApplyBlockAdvancesIdlePoolWithoutReplacingState(t *testing.T) {
	ctx := context.Background()
	key := marketv4.PoolKey{
		Currency0:   common.HexToAddress("0x0000000000000000000000000000000000000001"),
		Currency1:   common.HexToAddress("0x0000000000000000000000000000000000000002"),
		Fee:         500,
		TickSpacing: 60,
	}
	poolID, err := marketv4.ComputePoolID(key)
	if err != nil {
		t.Fatalf("compute pool id: %v", err)
	}

	pool := marketv4.NewPool(poolID, key)
	pool.State = market.PoolState{
		SqrtPriceX96: big.NewInt(100),
		Tick:         0,
		Liquidity:    big.NewInt(1000),
	}
	pool.Ticks.GetOrCreate(-120).LiquidityGross = big.NewInt(10)
	if err := pool.Bitmap.FlipTick(-120, key.TickSpacing); err != nil {
		t.Fatalf("flip old tick: %v", err)
	}
	pool.LastBlockNumber = 9

	service := NewBlockApplyService(
		&bootstrapV4PoolRepo{pool: pool},
		newMemoryV4CheckpointRepo(),
		nil,
		NewReadinessService(),
		nil,
	)

	result, err := service.ApplyBlock(ctx, ApplyBlockRequest{
		BlockNumber:  10,
		TrackedPools: []marketv4.PoolID{poolID},
	})
	if err != nil {
		t.Fatalf("apply block: %v", err)
	}
	if len(result.ChangedPools) != 1 || result.ChangedPools[0] != poolID {
		t.Fatalf("expected idle pool to be reported changed, got %v", result.ChangedPools)
	}

	synced, err := service.pools.Get(ctx, poolID)
	if err != nil {
		t.Fatalf("load synced pool: %v", err)
	}
	if synced.LastBlockNumber != 10 {
		t.Fatalf("expected last block 10, got %d", synced.LastBlockNumber)
	}
	if synced.State.Tick != 0 || synced.State.SqrtPriceX96.Cmp(big.NewInt(100)) != 0 || synced.State.Liquidity.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("idle pool state should not be replaced by ApplyBlock: %+v", synced.State)
	}
	if tick, ok := synced.Ticks.Get(-120); !ok || tick.LiquidityGross.Cmp(big.NewInt(10)) != 0 {
		t.Fatalf("expected existing tick -120 to remain, got tick=%+v ok=%v", tick, ok)
	}
	if initialized, err := synced.Bitmap.IsInitialized(-120, key.TickSpacing); err != nil || !initialized {
		t.Fatalf("expected existing bitmap tick -120 to remain initialized, initialized=%v err=%v", initialized, err)
	}
}

func TestApplyBlockSkipsEventsWhenBlockAlreadyApplied(t *testing.T) {
	ctx := context.Background()
	key := marketv4.PoolKey{
		Currency0:   common.HexToAddress("0x0000000000000000000000000000000000000001"),
		Currency1:   common.HexToAddress("0x0000000000000000000000000000000000000002"),
		Fee:         500,
		TickSpacing: 60,
	}
	poolID, err := marketv4.ComputePoolID(key)
	if err != nil {
		t.Fatalf("compute pool id: %v", err)
	}

	pool := marketv4.NewPool(poolID, key)
	pool.State = market.PoolState{
		SqrtPriceX96: big.NewInt(100),
		Tick:         0,
		Liquidity:    big.NewInt(1000),
	}
	pool.LastBlockNumber = 9

	poolRepo := &bootstrapV4PoolRepo{pool: pool}
	checkpoints := newMemoryV4CheckpointRepo()
	service := NewBlockApplyService(
		poolRepo,
		checkpoints,
		nil,
		NewReadinessService(),
		nil,
	)

	liquidityDelta := big.NewInt(500)
	event := marketv4.NewModifyLiquidityEvent(
		marketv4.EventMeta{
			PoolID:      poolID,
			BlockNumber: 10,
			TxIndex:     1,
			LogIndex:    1,
		},
		common.Address{},
		-120,
		120,
		liquidityDelta,
		common.Hash{},
	)
	req := ApplyBlockRequest{
		BlockNumber:  10,
		BlockHash:    common.HexToHash("0x10"),
		Events:       []marketv4.PoolEvent{event},
		TrackedPools: []marketv4.PoolID{poolID},
	}

	if _, err := service.ApplyBlock(ctx, req); err != nil {
		t.Fatalf("first apply block: %v", err)
	}
	if _, err := service.ApplyBlock(ctx, req); err != nil {
		t.Fatalf("retry apply block: %v", err)
	}

	synced, err := poolRepo.Get(ctx, poolID)
	if err != nil {
		t.Fatalf("load synced pool: %v", err)
	}
	if synced.State.Liquidity.Cmp(big.NewInt(1500)) != 0 {
		t.Fatalf("expected liquidity to be applied once, got %s", synced.State.Liquidity)
	}
	lower, ok := synced.Ticks.Get(-120)
	if !ok || lower.LiquidityGross.Cmp(liquidityDelta) != 0 {
		t.Fatalf("expected lower tick liquidity once, got tick=%+v ok=%v", lower, ok)
	}
	checkpoint, err := checkpoints.Get(ctx, poolID)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if checkpoint == nil || checkpoint.BlockNumber != 10 {
		t.Fatalf("expected checkpoint at block 10, got %+v", checkpoint)
	}
}
