package arbitrage

import (
	"encoding/json"
	"fmt"
	"math/big"

	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

type opportunityPayload struct {
	ID          string              `json:"id"`
	StrategyID  string              `json:"strategyId,omitempty"`
	Status      OpportunityStatus   `json:"status,omitempty"`
	PoolRef     string              `json:"poolRef,omitempty"`
	BlockNumber uint64              `json:"blockNumber"`
	Route       opportunityRoute    `json:"route,omitempty"`
	AmountIn    string              `json:"amountIn,omitempty"`
	AmountOut   string              `json:"amountOut,omitempty"`
	GrossProfit string              `json:"grossProfit,omitempty"`
	GasCost     string              `json:"gasCost,omitempty"`
	NetProfit   string              `json:"netProfit,omitempty"`
}

type opportunityRoute struct {
	TokenIn  string                   `json:"tokenIn"`
	TokenOut string                   `json:"tokenOut"`
	Hops     []opportunityRouteHop    `json:"hops,omitempty"`
}

type opportunityRouteHop struct {
	Version       string `json:"version"`
	PoolAddress   string `json:"poolAddress,omitempty"`
	PoolPancakeV3 string `json:"poolPancakeV3,omitempty"`
	PoolID        string `json:"poolId,omitempty"`
	TokenIn       string `json:"tokenIn"`
	TokenOut      string `json:"tokenOut"`
}

// EnsurePayload serializes the opportunity when payload is empty.
func (o *Opportunity) EnsurePayload() error {
	if o == nil {
		return fmt.Errorf("opportunity is nil")
	}
	if len(o.Payload) > 0 {
		return nil
	}
	payload, err := encodeOpportunityPayload(o)
	if err != nil {
		return err
	}
	o.Payload = payload
	return nil
}

func encodeOpportunityPayload(o *Opportunity) ([]byte, error) {
	payload := opportunityPayload{
		ID:          o.ID,
		StrategyID:  o.StrategyID,
		Status:      o.Status,
		PoolRef:     o.PoolRef.Key(),
		BlockNumber: o.BlockNumber,
		Route:       encodeOpportunityRoute(o.Route),
	}
	if o.AmountIn != nil {
		payload.AmountIn = o.AmountIn.String()
	}
	if o.AmountOut != nil {
		payload.AmountOut = o.AmountOut.String()
	}
	if o.GrossProfit != nil {
		payload.GrossProfit = o.GrossProfit.String()
	}
	if o.GasCost != nil {
		payload.GasCost = o.GasCost.String()
	}
	if o.NetProfit != nil {
		payload.NetProfit = o.NetProfit.String()
	}
	return json.Marshal(payload)
}

func encodeOpportunityRoute(route quoteunified.Route) opportunityRoute {
	encoded := opportunityRoute{
		TokenIn:  route.TokenIn.Hex(),
		TokenOut: route.TokenOut.Hex(),
		Hops:     make([]opportunityRouteHop, 0, len(route.Hops)),
	}
	for _, hop := range route.Hops {
		item := opportunityRouteHop{
			Version:  hop.Version.String(),
			TokenIn:  hop.TokenIn.Hex(),
			TokenOut: hop.TokenOut.Hex(),
		}
		switch hop.Version {
		case quoteunified.PoolVersionV3:
			item.PoolAddress = hop.PoolV3.Hex()
		case quoteunified.PoolVersionPancakeV3:
			item.PoolPancakeV3 = hop.PoolPancakeV3.Hex()
		case quoteunified.PoolVersionV4:
			item.PoolID = hop.PoolV4.String()
		}
		encoded.Hops = append(encoded.Hops, item)
	}
	return encoded
}

// ApplyPayload decodes persisted payload fields onto the opportunity.
func (o *Opportunity) ApplyPayload() error {
	if o == nil || len(o.Payload) == 0 {
		return nil
	}
	var payload opportunityPayload
	if err := json.Unmarshal(o.Payload, &payload); err != nil {
		return fmt.Errorf("decode opportunity payload: %w", err)
	}
	if o.StrategyID == "" {
		o.StrategyID = payload.StrategyID
	}
	if o.Status == "" {
		o.Status = payload.Status
	}
	if o.BlockNumber == 0 {
		o.BlockNumber = payload.BlockNumber
	}
	if o.AmountIn == nil && payload.AmountIn != "" {
		o.AmountIn = parsePayloadBigInt(payload.AmountIn)
	}
	if o.AmountOut == nil && payload.AmountOut != "" {
		o.AmountOut = parsePayloadBigInt(payload.AmountOut)
	}
	if o.GrossProfit == nil && payload.GrossProfit != "" {
		o.GrossProfit = parsePayloadBigInt(payload.GrossProfit)
	}
	if o.GasCost == nil && payload.GasCost != "" {
		o.GasCost = parsePayloadBigInt(payload.GasCost)
	}
	if o.NetProfit == nil && payload.NetProfit != "" {
		o.NetProfit = parsePayloadBigInt(payload.NetProfit)
	}
	if o.Route.Len() == 0 && len(payload.Route.Hops) > 0 {
		o.Route = decodeOpportunityRoute(payload.Route)
	}
	if o.PoolRef.Key() == "" && payload.PoolRef != "" {
		if ref, err := poolRefFromKey(payload.PoolRef); err == nil {
			o.PoolRef = ref
			o.PoolAddress = ref.PrimaryAddress()
		}
	}
	return nil
}

func decodeOpportunityRoute(route opportunityRoute) quoteunified.Route {
	decoded := quoteunified.Route{
		TokenIn:  common.HexToAddress(route.TokenIn),
		TokenOut: common.HexToAddress(route.TokenOut),
		Hops:     make([]quoteunified.RouteHop, 0, len(route.Hops)),
	}
	for _, hop := range route.Hops {
		item := quoteunified.RouteHop{
			TokenIn:  common.HexToAddress(hop.TokenIn),
			TokenOut: common.HexToAddress(hop.TokenOut),
		}
		switch hop.Version {
		case quoteunified.PoolVersionV3.String():
			item.Version = quoteunified.PoolVersionV3
			item.PoolV3 = common.HexToAddress(hop.PoolAddress)
		case quoteunified.PoolVersionPancakeV3.String():
			item.Version = quoteunified.PoolVersionPancakeV3
			item.PoolPancakeV3 = common.HexToAddress(hop.PoolPancakeV3)
		case quoteunified.PoolVersionV4.String():
			item.Version = quoteunified.PoolVersionV4
			item.PoolV4 = decodePoolID(hop.PoolID)
		case quoteunified.PoolVersionWrapWETH.String():
			item.Version = quoteunified.PoolVersionWrapWETH
		case quoteunified.PoolVersionUnwrapWETH.String():
			item.Version = quoteunified.PoolVersionUnwrapWETH
		}
		decoded.Hops = append(decoded.Hops, item)
	}
	return decoded
}

func decodePoolID(raw string) marketv4.PoolID {
	return marketv4.PoolID(common.HexToHash(raw))
}

func parsePayloadBigInt(raw string) *big.Int {
	value, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return big.NewInt(0)
	}
	return value
}
