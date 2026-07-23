package arbitrage

import (
	"encoding/json"
	"fmt"
	"math/big"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

type opportunityPayload struct {
	ID                string                 `json:"id"`
	StrategyID        string                 `json:"strategyId,omitempty"`
	Status            OpportunityStatus      `json:"status,omitempty"`
	PoolRef           string                 `json:"poolRef,omitempty"`
	BlockNumber       uint64                 `json:"blockNumber"`
	Route             opportunityRoute       `json:"route,omitempty"`
	AmountIn          string                 `json:"amountIn,omitempty"`
	AmountOut         string                 `json:"amountOut,omitempty"`
	GrossProfit       string                 `json:"grossProfit,omitempty"`
	GasCost           string                 `json:"gasCost,omitempty"`
	BuilderPaymentWei string                 `json:"builderPaymentWei,omitempty"`
	FlashLoan         *opportunityFlash      `json:"flashLoan,omitempty"`
	NetProfit         string                 `json:"netProfit,omitempty"`
	QuoteSteps        []opportunityQuoteStep `json:"quoteSteps,omitempty"`
}

type opportunityFlash struct {
	Protocol     string `json:"protocol,omitempty"`
	PoolRef      string `json:"poolRef,omitempty"`
	Amount       string `json:"amount,omitempty"`
	Fee          string `json:"fee,omitempty"`
	FeePPM       string `json:"feePpm,omitempty"`
	BorrowToken0 bool   `json:"borrowToken0,omitempty"`
}

type opportunityRoute struct {
	TokenIn  string                `json:"tokenIn"`
	TokenOut string                `json:"tokenOut"`
	Hops     []opportunityRouteHop `json:"hops,omitempty"`
}

type opportunityRouteHop struct {
	Version         string `json:"version"`
	PoolAddress     string `json:"poolAddress,omitempty"`
	PoolPancakeV3   string `json:"poolPancakeV3,omitempty"`
	PoolQuickSwapV3 string `json:"poolQuickSwapV3,omitempty"`
	PoolID          string `json:"poolId,omitempty"`
	TokenIn         string `json:"tokenIn"`
	TokenOut        string `json:"tokenOut"`
}

type opportunityQuoteStep struct {
	Index     int    `json:"index"`
	Version   string `json:"version,omitempty"`
	TokenIn   string `json:"tokenIn"`
	TokenOut  string `json:"tokenOut"`
	AmountIn  string `json:"amountIn"`
	AmountOut string `json:"amountOut"`
	FeeAmount string `json:"feeAmount,omitempty"`
}

// EnsurePayload serializes the opportunity when payload is empty, and keeps the
// persisted status field aligned with Opportunity.Status when payload already exists.
// Extra payload keys (for example execution) are preserved.
func (o *Opportunity) EnsurePayload() error {
	if o == nil {
		return fmt.Errorf("opportunity is nil")
	}
	if len(o.Payload) == 0 {
		payload, err := encodeOpportunityPayload(o)
		if err != nil {
			return err
		}
		o.Payload = payload
		return nil
	}
	return o.syncPayloadStatus()
}

// SetStatus updates the opportunity status and keeps the encoded payload in sync.
func (o *Opportunity) SetStatus(status OpportunityStatus) error {
	if o == nil {
		return fmt.Errorf("opportunity is nil")
	}
	o.Status = status
	return o.EnsurePayload()
}

func (o *Opportunity) syncPayloadStatus() error {
	var payload map[string]any
	if err := json.Unmarshal(o.Payload, &payload); err != nil {
		return fmt.Errorf("decode opportunity payload: %w", err)
	}
	if payload == nil {
		payload = make(map[string]any)
	}
	current, _ := payload["status"].(string)
	if current == string(o.Status) {
		return nil
	}
	payload["status"] = string(o.Status)
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode opportunity payload: %w", err)
	}
	o.Payload = raw
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
	if o.BuilderPaymentWei != nil {
		payload.BuilderPaymentWei = o.BuilderPaymentWei.String()
	}
	payload.FlashLoan = encodeOpportunityFlash(o.FlashLoan)
	if o.NetProfit != nil {
		payload.NetProfit = o.NetProfit.String()
	}
	payload.QuoteSteps = encodeOpportunityQuoteSteps(o.QuoteSteps)
	return json.Marshal(payload)
}

func encodeOpportunityFlash(flash FlashLoanQuote) *opportunityFlash {
	if flash.Protocol == "" {
		return nil
	}
	payload := &opportunityFlash{
		Protocol:     string(flash.Protocol),
		PoolRef:      flash.PoolRef.Key(),
		BorrowToken0: flash.BorrowToken0,
	}
	if flash.Amount != nil {
		payload.Amount = flash.Amount.String()
	}
	if flash.Fee != nil {
		payload.Fee = flash.Fee.String()
	}
	if flash.FeePPM != nil {
		payload.FeePPM = flash.FeePPM.String()
	}
	return payload
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
		case quoteunified.PoolVersionQuickSwapV3:
			item.PoolQuickSwapV3 = hop.PoolQuickSwapV3.Hex()
		case quoteunified.PoolVersionV4:
			item.PoolID = hop.PoolV4.String()
		case quoteunified.PoolVersionBalancer:
			item.PoolID = hop.PoolBalancer.String()
		}
		encoded.Hops = append(encoded.Hops, item)
	}
	return encoded
}

func encodeOpportunityQuoteSteps(steps []OpportunityQuoteStep) []opportunityQuoteStep {
	if len(steps) == 0 {
		return nil
	}
	encoded := make([]opportunityQuoteStep, 0, len(steps))
	for _, step := range steps {
		encoded = append(encoded, opportunityQuoteStep{
			Index:     step.Index,
			Version:   step.Version,
			TokenIn:   step.TokenIn.Hex(),
			TokenOut:  step.TokenOut.Hex(),
			AmountIn:  bigIntString(step.AmountIn),
			AmountOut: bigIntString(step.AmountOut),
			FeeAmount: bigIntString(step.FeeAmount),
		})
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
	if o.BuilderPaymentWei == nil && payload.BuilderPaymentWei != "" {
		o.BuilderPaymentWei = parsePayloadBigInt(payload.BuilderPaymentWei)
	}
	if o.FlashLoan.Protocol == "" && payload.FlashLoan != nil && payload.FlashLoan.Protocol != "" {
		o.FlashLoan = decodeOpportunityFlash(*payload.FlashLoan)
	}
	if o.NetProfit == nil && payload.NetProfit != "" {
		o.NetProfit = parsePayloadBigInt(payload.NetProfit)
	}
	if len(o.QuoteSteps) == 0 && len(payload.QuoteSteps) > 0 {
		o.QuoteSteps = decodeOpportunityQuoteSteps(payload.QuoteSteps)
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

func decodeOpportunityQuoteSteps(steps []opportunityQuoteStep) []OpportunityQuoteStep {
	if len(steps) == 0 {
		return nil
	}
	decoded := make([]OpportunityQuoteStep, 0, len(steps))
	for _, step := range steps {
		decoded = append(decoded, OpportunityQuoteStep{
			Index:     step.Index,
			Version:   step.Version,
			TokenIn:   common.HexToAddress(step.TokenIn),
			TokenOut:  common.HexToAddress(step.TokenOut),
			AmountIn:  parsePayloadBigInt(step.AmountIn),
			AmountOut: parsePayloadBigInt(step.AmountOut),
			FeeAmount: parsePayloadBigInt(step.FeeAmount),
		})
	}
	return decoded
}

func bigIntString(value *big.Int) string {
	if value == nil {
		return ""
	}
	return value.String()
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
		case quoteunified.PoolVersionQuickSwapV3.String():
			item.Version = quoteunified.PoolVersionQuickSwapV3
			item.PoolQuickSwapV3 = common.HexToAddress(hop.PoolQuickSwapV3)
		case quoteunified.PoolVersionV4.String():
			item.Version = quoteunified.PoolVersionV4
			item.PoolV4 = decodePoolID(hop.PoolID)
		case quoteunified.PoolVersionBalancer.String():
			item.Version = quoteunified.PoolVersionBalancer
			item.PoolBalancer = decodeBalancerPoolID(hop.PoolID)
		case quoteunified.PoolVersionWrapWETH.String():
			item.Version = quoteunified.PoolVersionWrapWETH
		case quoteunified.PoolVersionUnwrapWETH.String():
			item.Version = quoteunified.PoolVersionUnwrapWETH
		}
		decoded.Hops = append(decoded.Hops, item)
	}
	return decoded
}

func decodeOpportunityFlash(payload opportunityFlash) FlashLoanQuote {
	return FlashLoanQuote{
		Protocol:     FlashLoanProtocol(payload.Protocol),
		PoolRef:      decodeOptionalPoolRef(payload.PoolRef),
		Amount:       parsePayloadBigInt(payload.Amount),
		Fee:          parsePayloadBigInt(payload.Fee),
		FeePPM:       parsePayloadBigInt(payload.FeePPM),
		BorrowToken0: payload.BorrowToken0,
	}
}

func decodeOptionalPoolRef(raw string) PoolRef {
	if raw == "" {
		return PoolRef{}
	}
	ref, err := poolRefFromKey(raw)
	if err != nil {
		return PoolRef{}
	}
	return ref
}

func decodePoolID(raw string) marketv4.PoolID {
	return marketv4.PoolID(common.HexToHash(raw))
}

func decodeBalancerPoolID(raw string) marketbalancer.PoolID {
	return marketbalancer.PoolID(common.HexToHash(raw))
}

func parsePayloadBigInt(raw string) *big.Int {
	value, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return big.NewInt(0)
	}
	return value
}
