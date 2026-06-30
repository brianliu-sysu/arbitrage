package pool

import (
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// Cache 持有所有链上池子的运行时 State，供 Quote / Arbitrage / BlockProcessor 共享访问。
type Cache struct {
	pools sync.Map // common.Address -> *State
}

// NewCache 创建空池子缓存。
func NewCache() *Cache {
	return &Cache{}
}

// Get 按地址获取池子状态。
func (c *Cache) Get(addr common.Address) (*State, bool) {
	v, ok := c.pools.Load(addr)
	if !ok {
		return nil, false
	}
	return v.(*State), true
}

// Set 注册或替换池子状态。
func (c *Cache) Set(addr common.Address, state *State) {
	if state == nil {
		c.pools.Delete(addr)
		return
	}
	c.pools.Store(addr, state)
}

// Delete 移除池子。
func (c *Cache) Delete(addr common.Address) {
	c.pools.Delete(addr)
}

// Len 返回当前缓存池子数量。
func (c *Cache) Len() int {
	n := 0
	c.pools.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

// Range 遍历所有池子，fn 返回 false 时停止。
func (c *Cache) Range(fn func(addr common.Address, state *State) bool) {
	c.pools.Range(func(key, value any) bool {
		return fn(key.(common.Address), value.(*State))
	})
}

// Addresses 返回所有已注册池子地址。
func (c *Cache) Addresses() []common.Address {
	addrs := make([]common.Address, 0, c.Len())
	c.Range(func(addr common.Address, _ *State) bool {
		addrs = append(addrs, addr)
		return true
	})
	return addrs
}

// MustGet 获取池子，不存在时 panic（测试/内部使用）。
func (c *Cache) MustGet(addr common.Address) *State {
	s, ok := c.Get(addr)
	if !ok {
		panic(fmt.Sprintf("pool %s not in cache", addr.Hex()))
	}
	return s
}
