package service

import (
	"fmt"
	"math/big"
	"sync"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/quote"
	"github.com/ethereum/go-ethereum/common"
)

// MultiChainService 管理多条链的报价服务，每链一个 MultiPoolService。
type MultiChainService struct {
	mu     sync.RWMutex
	chains map[string]*MultiPoolService // chainName -> svc
	logger logx.Logger
}

// NewMultiChainService 创建多链报价服务。
func NewMultiChainService(logger logx.Logger) *MultiChainService {
	return &MultiChainService{
		chains: make(map[string]*MultiPoolService),
		logger: logger,
	}
}

// AddChain 注册一条链的多池子服务。
func (m *MultiChainService) AddChain(name string, svc *MultiPoolService) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.chains[name]; exists {
		return fmt.Errorf("chain %q already registered", name)
	}
	m.chains[name] = svc
	return nil
}

// GetChain 获取指定链的服务。
func (m *MultiChainService) GetChain(name string) (*MultiPoolService, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	svc, ok := m.chains[name]
	return svc, ok
}

// ChainNames 返回所有已注册的链名。
func (m *MultiChainService) ChainNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.chains))
	for name := range m.chains {
		names = append(names, name)
	}
	return names
}

// StopAll 停止所有链。
func (m *MultiChainService) StopAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for name, svc := range m.chains {
		svc.StopAll()
		m.logger.Info("stopped chain", "chain", name)
	}
	m.logger.Info("all chains stopped")
}

// SetOnPriceUpdateAll 为所有链设置价格更新回调。
func (m *MultiChainService) SetOnPriceUpdateAll(fn func(chain string, poolAddr common.Address, price0In1, price1In0 float64, tick int32)) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for name, svc := range m.chains {
		chainName := name
		svc.SetOnPriceUpdate(func(addr common.Address, price0In1, price1In0 float64, tick int32) {
			fn(chainName, addr, price0In1, price1In0, tick)
		})
	}
}

// ---- QuoteProvider-compatible delegation ----

// GetAllPoolInfoFlat 返回所有链的所有池子（含 chain 字段）。
func (m *MultiChainService) GetAllPoolInfoFlat() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []map[string]interface{}
	for name, svc := range m.chains {
		for _, info := range svc.GetAllPoolInfo() {
			info["chain"] = name
			result = append(result, info)
		}
	}
	return result
}

// GetAllPoolInfo 返回所有链的所有池子（按链分组）。
func (m *MultiChainService) GetAllPoolInfo() []map[string]interface{} {
	return m.GetAllPoolInfoFlat()
}

// GetPrice 获取指定链上指定池子的现货价格。
func (m *MultiChainService) GetPrice(chain string, poolAddr common.Address) (price0In1, price1In0 float64, tick int32, ok bool) {
	svc, ok := m.GetChain(chain)
	if !ok {
		return 0, 0, 0, false
	}
	return svc.GetPrice(poolAddr)
}

// QuoteExactInput 对指定链上指定池子执行报价。
func (m *MultiChainService) QuoteExactInput(chain string, poolAddr common.Address, amountIn *big.Int, tokenIn common.Address) (*big.Int, error) {
	svc, ok := m.GetChain(chain)
	if !ok {
		return nil, fmt.Errorf("chain %q not found", chain)
	}
	return svc.QuoteExactInput(poolAddr, amountIn, tokenIn)
}

// CrossQuote 在指定链上执行跨池报价。
func (m *MultiChainService) CrossQuote(chain string, amountIn *big.Int, tokenIn, tokenOut common.Address) (*quote.Result, error) {
	svc, ok := m.GetChain(chain)
	if !ok {
		return nil, fmt.Errorf("chain %q not found", chain)
	}
	return svc.CrossQuote(amountIn, tokenIn, tokenOut)
}
