package api

import (
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// 请求/响应类型
// ---------------------------------------------------------------------------

type quoteRequest struct {
	AmountIn string `json:"amountIn" binding:"required"`
	TokenIn  string `json:"tokenIn" binding:"required"`
}

type crossQuoteRequest struct {
	AmountIn string `json:"amountIn" binding:"required"`
	TokenIn  string `json:"tokenIn" binding:"required"`
	TokenOut string `json:"tokenOut" binding:"required"`
}

type crossQuoteResponse struct {
	AmountIn  string     `json:"amountIn"`
	AmountOut string     `json:"amountOut"`
	Chain     string     `json:"chain"`
	TokenIn   string     `json:"tokenIn"`
	TokenOut  string     `json:"tokenOut"`
	Hops      int        `json:"hops"`
	Path      []quoteHop `json:"path"`
}

type quoteHop struct {
	Pool     string `json:"pool"`
	TokenIn  string `json:"tokenIn"`
	TokenOut string `json:"tokenOut"`
}

type triangleOpportunityResponse struct {
	Chain       string     `json:"chain"`
	BaseToken   string     `json:"baseToken"`
	AmountIn    string     `json:"amountIn"`
	AmountOut   string     `json:"amountOut"`
	Profit      string     `json:"profit"`
	ProfitBps   float64    `json:"profitBps"`
	BlockNumber uint64     `json:"blockNumber"`
	DetectedAt  string     `json:"detectedAt"`
	Path        []quoteHop `json:"path"`
}

type poolInfo struct {
	Chain        string  `json:"chain,omitempty"`
	Address      string  `json:"address"`
	Token0       string  `json:"token0"`
	Token1       string  `json:"token1"`
	Token0Symbol string  `json:"token0Symbol,omitempty"`
	Token1Symbol string  `json:"token1Symbol,omitempty"`
	Fee          uint32  `json:"fee"`
	Tick         int32   `json:"tick"`
	Price0In1    float64 `json:"price0In1"`
	Price1In0    float64 `json:"price1In0"`
	Liquidity    string  `json:"liquidity"`
	SqrtPriceX96 string  `json:"sqrtPriceX96"`
	BlockNumber  uint64  `json:"blockNumber"`
}

type priceResponse struct {
	Chain     string  `json:"chain,omitempty"`
	Address   string  `json:"address"`
	Price0In1 float64 `json:"price0In1"`
	Price1In0 float64 `json:"price1In0"`
	Tick      int32   `json:"tick"`
}

type quoteResponse struct {
	Chain     string `json:"chain,omitempty"`
	AmountIn  string `json:"amountIn"`
	AmountOut string `json:"amountOut"`
	TokenIn   string `json:"tokenIn"`
	TokenOut  string `json:"tokenOut"`
	Pool      string `json:"pool"`
}

// ---------------------------------------------------------------------------
// 处理器
// ---------------------------------------------------------------------------

func (s *Server) handleHealth(c *gin.Context) {
	pools := s.svc.GetAllPoolInfo()
	chains := make(map[string]int)
	totalPools := 0
	totalTicks := 0
	for _, m := range pools {
		chain, _ := m["chain"].(string)
		chains[chain]++
		totalPools++
		tickCount, _ := m["tick"].(float64)
		if tickCount > 0 {
			totalTicks++
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"chains":      len(chains),
		"pools":       totalPools,
		"chainDetail": chains,
	})
}

func (s *Server) handleListPools(c *gin.Context) {
	chain := c.Param("chain")
	raw := s.svc.GetAllPoolInfo()
	pools := make([]poolInfo, 0, 0)
	for _, m := range raw {
		chainMatch, _ := m["chain"].(string)
		if chainMatch != chain {
			continue
		}
		pools = append(pools, mapToPoolInfo(m))
	}
	c.JSON(http.StatusOK, gin.H{"pools": pools})
}

func (s *Server) handleGetPool(c *gin.Context) {
	chain := c.Param("chain")
	addr := c.Param("address")
	poolAddr := common.HexToAddress(addr)

	info := s.findPool(chain, poolAddr)
	if info == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pool not found", "chain": chain, "address": addr})
		return
	}

	c.JSON(http.StatusOK, info)
}

func (s *Server) handleGetPrice(c *gin.Context) {
	chain := c.Param("chain")
	addr := c.Param("address")
	poolAddr := common.HexToAddress(addr)

	price0, price1, tick, ok := s.svc.GetPrice(chain, poolAddr)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "pool not found", "chain": chain, "address": addr})
		return
	}

	c.JSON(http.StatusOK, priceResponse{
		Chain:     chain,
		Address:   addr,
		Price0In1: price0,
		Price1In0: price1,
		Tick:      tick,
	})
}

func (s *Server) handleQuote(c *gin.Context) {
	chain := c.Param("chain")
	addr := c.Param("address")
	poolAddr := common.HexToAddress(addr)

	info := s.findPool(chain, poolAddr)
	if info == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pool not found", "chain": chain, "address": addr})
		return
	}

	var req quoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	amountIn, ok := new(big.Int).SetString(req.AmountIn, 10)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amountIn: " + req.AmountIn})
		return
	}

	tokenInAddr := common.HexToAddress(req.TokenIn)

	amountOut, err := s.svc.QuoteExactInput(chain, poolAddr, amountIn, tokenInAddr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var tokenOut string
	if req.TokenIn == info.Token0 {
		tokenOut = info.Token1
	} else {
		tokenOut = info.Token0
	}

	c.JSON(http.StatusOK, quoteResponse{
		Chain:     chain,
		AmountIn:  req.AmountIn,
		AmountOut: amountOut.String(),
		TokenIn:   req.TokenIn,
		TokenOut:  tokenOut,
		Pool:      addr,
	})
}

func (s *Server) handleCrossQuote(c *gin.Context) {
	chain := c.Param("chain")

	var req crossQuoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	amountIn, ok := new(big.Int).SetString(req.AmountIn, 10)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amountIn: " + req.AmountIn})
		return
	}

	tokenInAddr := common.HexToAddress(req.TokenIn)
	tokenOutAddr := common.HexToAddress(req.TokenOut)

	result, err := s.svc.CrossQuote(chain, amountIn, tokenInAddr, tokenOutAddr)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "chain": chain})
		return
	}

	path := make([]quoteHop, len(result.Hops))
	for i, h := range result.Hops {
		path[i] = quoteHop{
			Pool:     h.Pool.Hex(),
			TokenIn:  h.TokenIn.Hex(),
			TokenOut: h.TokenOut.Hex(),
		}
	}

	c.JSON(http.StatusOK, crossQuoteResponse{
		Chain:     chain,
		AmountIn:  req.AmountIn,
		AmountOut: result.AmountOut.String(),
		TokenIn:   req.TokenIn,
		TokenOut:  req.TokenOut,
		Hops:      len(path),
		Path:      path,
	})
}

func (s *Server) handleTriangleOpportunities(c *gin.Context) {
	chain := c.Param("chain")
	opps, err := s.svc.TriangleOpportunities(chain)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "chain": chain})
		return
	}
	resp := make([]triangleOpportunityResponse, 0, len(opps))
	for _, opp := range opps {
		path := make([]quoteHop, 0, len(opp.Path.Hops))
		for _, h := range opp.Path.Hops {
			path = append(path, quoteHop{
				Pool:     h.Pool.Hex(),
				TokenIn:  h.TokenIn.Hex(),
				TokenOut: h.TokenOut.Hex(),
			})
		}
		resp = append(resp, triangleOpportunityResponse{
			Chain:       chain,
			BaseToken:   opp.Path.BaseToken.Hex(),
			AmountIn:    bigString(opp.AmountIn),
			AmountOut:   bigString(opp.AmountOut),
			Profit:      bigString(opp.Profit),
			ProfitBps:   opp.ProfitBps,
			BlockNumber: opp.BlockNumber,
			DetectedAt:  opp.DetectedAt.Format(time.RFC3339Nano),
			Path:        path,
		})
	}
	c.JSON(http.StatusOK, gin.H{"opportunities": resp})
}

// ---------------------------------------------------------------------------
// 内部工具
// ---------------------------------------------------------------------------

func (s *Server) findPool(chain string, poolAddr common.Address) *poolInfo {
	addrLower := strings.ToLower(poolAddr.Hex())
	for _, m := range s.svc.GetAllPoolInfo() {

		chainMatch, _ := m["chain"].(string)
		addr, _ := m["address"].(string)
		if chainMatch == chain {
			// 大小写不敏感比较（EIP-55 checksum vs lowercase）
			if strings.EqualFold(addr, addrLower) || strings.ToLower(addr) == addrLower {
				info := mapToPoolInfo(m)
				return &info
			}
		}
	}
	return nil
}

func mapToPoolInfo(m map[string]interface{}) poolInfo {
	return poolInfo{
		Chain:        strVal(m, "chain"),
		Address:      strVal(m, "address"),
		Token0:       strVal(m, "token0"),
		Token1:       strVal(m, "token1"),
		Token0Symbol: strVal(m, "token0Symbol"),
		Token1Symbol: strVal(m, "token1Symbol"),
		Fee:          uint32Val(m, "fee"),
		Tick:         int32(int64Val(m, "tick")),
		Price0In1:    float64Val(m, "price0In1"),
		Price1In0:    float64Val(m, "price1In0"),
		Liquidity:    strVal(m, "liquidity"),
		SqrtPriceX96: strVal(m, "sqrtPriceX96"),
		BlockNumber:  uint64(int64Val(m, "blockNumber")),
	}
}

func strVal(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func int64Val(m map[string]interface{}, key string) int64 {
	switch v := m[key].(type) {
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint64:
		return int64(v)
	case uint32:
		return int64(v)
	case float64:
		return int64(v)
	}
	return 0
}

func uint32Val(m map[string]interface{}, key string) uint32 {
	switch v := m[key].(type) {
	case int:
		return uint32(v)
	case int32:
		return uint32(v)
	case int64:
		return uint32(v)
	case uint32:
		return v
	case float64:
		return uint32(v)
	}
	return 0
}

func float64Val(m map[string]interface{}, key string) float64 {
	v, _ := m[key].(float64)
	return v
}

func bigString(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}
