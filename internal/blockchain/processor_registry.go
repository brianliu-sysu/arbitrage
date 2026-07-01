package blockchain

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/brianliu-sysu/arbitrage/internal/pool/replay"
	"github.com/brianliu-sysu/arbitrage/internal/storage"
	"github.com/ethereum/go-ethereum/common"
)

// ProtocolID 标识 DEX 协议类型（多 DEX 扩展点）。
type ProtocolID string

const (
	ProtocolUniswapV3 ProtocolID = "uniswap_v3"
	// ProtocolUniswapV4 ProtocolID = "uniswap_v4"
	// ProtocolAerodrome  ProtocolID = "aerodrome"
)

// PoolAddressUpdater 跟踪池子地址列表的 Processor 接口。
type PoolAddressUpdater interface {
	SetPoolAddresses(addrs []common.Address)
}

// PoolBackfiller 为单个池子回填历史区块（动态加池时用，不推进链级游标）。
type PoolBackfiller interface {
	BackfillPool(ctx context.Context, addr common.Address, from, to uint64) error
	ChainLastProcessedBlock(ctx context.Context) (uint64, error)
}

// PoolLoader 完成池子加载 handoff：回填历史 + 消费缓冲事件。
type PoolLoader interface {
	PoolBackfiller
	FinishPoolLoading(ctx context.Context, addr common.Address) error
}

// ProcessorBuildParams 构建 BlockProcessor 所需依赖。
type ProcessorBuildParams struct {
	ChainName     string
	Protocol      ProtocolID
	Cache         *pool.Cache
	Fetcher       BlockLogFetcher
	PoolRepo      storage.PoolRepo
	SyncRepo      storage.SyncRepo
	Logger        logx.Logger
	OnPoolApplied func(chainName string, poolAddr common.Address, blockNumber uint64)
}

// ProcessorFactory 按协议创建 BlockProcessor。
type ProcessorFactory func(ProcessorBuildParams) (BlockProcessor, error)

// ProcessorRegistry 多 DEX BlockProcessor 注册表。
type ProcessorRegistry struct {
	factories map[ProtocolID]ProcessorFactory
}

// NewProcessorRegistry 创建注册表并注册内置协议。
func NewProcessorRegistry() *ProcessorRegistry {
	r := &ProcessorRegistry{factories: make(map[ProtocolID]ProcessorFactory)}
	r.Register(ProtocolUniswapV3, func(p ProcessorBuildParams) (BlockProcessor, error) {
		return NewUniswapV3BlockProcessor(
			p.ChainName, p.Cache, p.Fetcher, replay.NewDefaultApplier(), p.PoolRepo, p.SyncRepo, p.Logger, p.OnPoolApplied,
		), nil
	})
	return r
}

// Register 注册协议 Processor 工厂。
func (r *ProcessorRegistry) Register(id ProtocolID, factory ProcessorFactory) {
	r.factories[id] = factory
}

// Build 构建指定协议的 BlockProcessor。
func (r *ProcessorRegistry) Build(p ProcessorBuildParams) (BlockProcessor, error) {
	f, ok := r.factories[p.Protocol]
	if !ok {
		return nil, fmt.Errorf("unsupported protocol: %s", p.Protocol)
	}
	return f(p)
}

// BuildComposite 为一条链构建多协议 CompositeBlockProcessor。
func (r *ProcessorRegistry) BuildComposite(chainName string, protocols []ProtocolID, params ProcessorBuildParams) (BlockProcessor, error) {
	var processors []BlockProcessor
	for _, proto := range protocols {
		params.ChainName = chainName
		params.Protocol = proto
		p, err := r.Build(params)
		if err != nil {
			return nil, err
		}
		processors = append(processors, p)
	}
	return NewCompositeBlockProcessor(processors...), nil
}

// CompositeBlockProcessor 顺序执行多个 BlockProcessor（多 DEX 同链）。
type CompositeBlockProcessor struct {
	processors []BlockProcessor
}

// NewCompositeBlockProcessor 创建组合 Processor。
func NewCompositeBlockProcessor(processors ...BlockProcessor) *CompositeBlockProcessor {
	return &CompositeBlockProcessor{processors: processors}
}

// ProcessBlock 依次调用各 Processor。
func (c *CompositeBlockProcessor) ProcessBlock(ctx context.Context, block uint64) error {
	for _, p := range c.processors {
		if err := p.ProcessBlock(ctx, block); err != nil {
			return err
		}
	}
	return nil
}

// SetPoolAddresses 传播到支持更新的 Processor。
func (c *CompositeBlockProcessor) SetPoolAddresses(addrs []common.Address) {
	for _, p := range c.processors {
		if u, ok := p.(PoolAddressUpdater); ok {
			u.SetPoolAddresses(addrs)
		}
	}
}

// BackfillPool 委托给首个支持回填的 Processor。
func (c *CompositeBlockProcessor) BackfillPool(ctx context.Context, addr common.Address, from, to uint64) error {
	for _, p := range c.processors {
		if bf, ok := p.(PoolBackfiller); ok {
			return bf.BackfillPool(ctx, addr, from, to)
		}
	}
	return fmt.Errorf("no pool backfiller registered")
}

// ChainLastProcessedBlock 返回链级已扫块高度。
func (c *CompositeBlockProcessor) ChainLastProcessedBlock(ctx context.Context) (uint64, error) {
	for _, p := range c.processors {
		if bf, ok := p.(PoolBackfiller); ok {
			return bf.ChainLastProcessedBlock(ctx)
		}
	}
	return 0, fmt.Errorf("no pool backfiller registered")
}

// FinishPoolLoading 委托给首个支持加载完成的 Processor。
func (c *CompositeBlockProcessor) FinishPoolLoading(ctx context.Context, addr common.Address) error {
	for _, p := range c.processors {
		if loader, ok := p.(PoolLoader); ok {
			return loader.FinishPoolLoading(ctx, addr)
		}
	}
	return fmt.Errorf("no pool loader registered")
}
