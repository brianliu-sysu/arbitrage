package arbitrage

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/ethereum/go-ethereum/common"
)

// TriangleConfig controls in-memory 3-hop arbitrage scanning.
type TriangleConfig struct {
	BaseTokens       []common.Address
	AmountCandidates []*big.Int
	MinProfitBps     float64
	MaxResults       int
}

// TriangleHop is one directed swap in a triangle path.
type TriangleHop struct {
	Pool     common.Address
	TokenIn  common.Address
	TokenOut common.Address
}

// TrianglePath is a fixed 3-hop path A -> B -> C -> A.
type TrianglePath struct {
	BaseToken common.Address
	Hops      [3]TriangleHop
}

// TriangleOpportunity is a simulated triangular arbitrage candidate.
type TriangleOpportunity struct {
	Path        TrianglePath
	AmountIn    *big.Int
	AmountOut   *big.Int
	Profit      *big.Int
	ProfitBps   float64
	BlockNumber uint64
	DetectedAt  time.Time
}

type triangleQuoteFunc func(poolAddr common.Address, amountIn *big.Int, tokenIn common.Address) (*big.Int, error)

type directedEdge struct {
	Pool     common.Address
	TokenIn  common.Address
	TokenOut common.Address
}

// TriangleScanner scans the current pool cache for profitable 3-hop cycles.
type TriangleScanner struct {
	cache   *pool.Cache
	cfg     TriangleConfig
	quoteFn triangleQuoteFunc
}

// NewTriangleScanner creates a scanner using pool.State exact-input simulation.
func NewTriangleScanner(cache *pool.Cache, cfg TriangleConfig) *TriangleScanner {
	s := &TriangleScanner{cache: cache, cfg: normalizeTriangleConfig(cfg)}
	s.quoteFn = s.quotePool
	return s
}

func newTriangleScannerWithQuoteFunc(cache *pool.Cache, cfg TriangleConfig, quoteFn triangleQuoteFunc) *TriangleScanner {
	s := &TriangleScanner{cache: cache, cfg: normalizeTriangleConfig(cfg), quoteFn: quoteFn}
	if s.quoteFn == nil {
		s.quoteFn = s.quotePool
	}
	return s
}

func normalizeTriangleConfig(cfg TriangleConfig) TriangleConfig {
	if cfg.MaxResults <= 0 {
		cfg.MaxResults = 20
	}
	amounts := make([]*big.Int, 0, len(cfg.AmountCandidates))
	for _, amount := range cfg.AmountCandidates {
		if amount != nil && amount.Sign() > 0 {
			amounts = append(amounts, new(big.Int).Set(amount))
		}
	}
	cfg.AmountCandidates = amounts
	return cfg
}

// Scan enumerates base-token 3-hop cycles and returns profitable opportunities.
func (s *TriangleScanner) Scan() []TriangleOpportunity {
	if s == nil || s.cache == nil || len(s.cfg.BaseTokens) == 0 || len(s.cfg.AmountCandidates) == 0 {
		return nil
	}

	adj := s.buildAdjacency()
	if len(adj) == 0 {
		return nil
	}

	now := time.Now()
	var out []TriangleOpportunity
	for _, base := range s.cfg.BaseTokens {
		for _, h1 := range adj[base] {
			if h1.TokenOut == base {
				continue
			}
			for _, h2 := range adj[h1.TokenOut] {
				if h2.Pool == h1.Pool || h2.TokenOut == base {
					continue
				}
				for _, h3 := range adj[h2.TokenOut] {
					if h3.Pool == h1.Pool || h3.Pool == h2.Pool || h3.TokenOut != base {
						continue
					}
					path := TrianglePath{
						BaseToken: base,
						Hops: [3]TriangleHop{
							{Pool: h1.Pool, TokenIn: h1.TokenIn, TokenOut: h1.TokenOut},
							{Pool: h2.Pool, TokenIn: h2.TokenIn, TokenOut: h2.TokenOut},
							{Pool: h3.Pool, TokenIn: h3.TokenIn, TokenOut: h3.TokenOut},
						},
					}
					for _, amount := range s.cfg.AmountCandidates {
						if opp, ok := s.simulatePath(path, amount, now); ok {
							out = append(out, opp)
						}
					}
				}
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].ProfitBps == out[j].ProfitBps {
			return out[i].Profit.Cmp(out[j].Profit) > 0
		}
		return out[i].ProfitBps > out[j].ProfitBps
	})
	if len(out) > s.cfg.MaxResults {
		out = out[:s.cfg.MaxResults]
	}
	return out
}

func (s *TriangleScanner) buildAdjacency() map[common.Address][]directedEdge {
	adj := make(map[common.Address][]directedEdge)
	s.cache.Range(func(addr common.Address, st *pool.State) bool {
		if st == nil || st.Token0 == (common.Address{}) || st.Token1 == (common.Address{}) {
			return true
		}
		if st.Loading() || st.PendingLen() > 0 {
			return true
		}
		adj[st.Token0] = append(adj[st.Token0], directedEdge{Pool: addr, TokenIn: st.Token0, TokenOut: st.Token1})
		adj[st.Token1] = append(adj[st.Token1], directedEdge{Pool: addr, TokenIn: st.Token1, TokenOut: st.Token0})
		return true
	})
	return adj
}

func (s *TriangleScanner) simulatePath(path TrianglePath, amountIn *big.Int, detectedAt time.Time) (TriangleOpportunity, bool) {
	current := new(big.Int).Set(amountIn)
	for _, hop := range path.Hops {
		next, err := s.quoteFn(hop.Pool, current, hop.TokenIn)
		if err != nil || next == nil || next.Sign() <= 0 {
			return TriangleOpportunity{}, false
		}
		current = next
	}
	profit := new(big.Int).Sub(current, amountIn)
	if profit.Sign() <= 0 {
		return TriangleOpportunity{}, false
	}
	profitBps := calcProfitBps(profit, amountIn)
	if profitBps+1e-9 < s.cfg.MinProfitBps {
		return TriangleOpportunity{}, false
	}
	return TriangleOpportunity{
		Path:        path,
		AmountIn:    new(big.Int).Set(amountIn),
		AmountOut:   new(big.Int).Set(current),
		Profit:      profit,
		ProfitBps:   profitBps,
		BlockNumber: s.pathBlockNumber(path),
		DetectedAt:  detectedAt,
	}, true
}

func (s *TriangleScanner) quotePool(poolAddr common.Address, amountIn *big.Int, tokenIn common.Address) (*big.Int, error) {
	st, ok := s.cache.Get(poolAddr)
	if !ok {
		return nil, fmt.Errorf("pool %s not found", poolAddr.Hex())
	}
	return st.QuoteExactInput(amountIn, tokenIn)
}

func (s *TriangleScanner) pathBlockNumber(path TrianglePath) uint64 {
	var block uint64
	for _, hop := range path.Hops {
		if st, ok := s.cache.Get(hop.Pool); ok && st.BlockNumber > block {
			block = st.BlockNumber
		}
	}
	return block
}

func calcProfitBps(profit, amountIn *big.Int) float64 {
	if amountIn == nil || amountIn.Sign() <= 0 || profit == nil {
		return 0
	}
	r := new(big.Rat).SetFrac(profit, amountIn)
	f, _ := r.Float64()
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f * 10000
}
