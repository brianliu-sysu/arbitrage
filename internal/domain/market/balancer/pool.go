package balancer

import (
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

// Pool is the aggregate root for Balancer weighted and stable pool state.
type Pool struct {
	ID      PoolID
	Address common.Address
	Vault   common.Address
	Type    PoolType

	Tokens            []common.Address
	Balances          map[common.Address]*big.Int
	Weights           map[common.Address]*big.Int
	Amplification     *big.Int
	SwapFeePercentage *big.Int
	Status            market.PoolStatus
	LastBlockNumber   uint64
}

func NewPool(id PoolID, address, vault common.Address, poolType PoolType, tokens []common.Address) (*Pool, error) {
	if err := poolType.Validate(); err != nil {
		return nil, err
	}
	if len(tokens) < 2 {
		return nil, fmt.Errorf("balancer pool must have at least two tokens")
	}
	return &Pool{
		ID:                id,
		Address:           address,
		Vault:             vault,
		Type:              poolType,
		Tokens:            cloneAddresses(tokens),
		Balances:          newTokenIntMap(tokens),
		Weights:           make(map[common.Address]*big.Int),
		Amplification:     big.NewInt(0),
		SwapFeePercentage: big.NewInt(0),
		Status:            market.PoolStatusUnknown,
	}, nil
}

func (p *Pool) Clone() *Pool {
	if p == nil {
		return nil
	}
	return &Pool{
		ID:                p.ID,
		Address:           p.Address,
		Vault:             p.Vault,
		Type:              p.Type,
		Tokens:            cloneAddresses(p.Tokens),
		Balances:          cloneIntMap(p.Balances),
		Weights:           cloneIntMap(p.Weights),
		Amplification:     cloneInt(p.Amplification),
		SwapFeePercentage: cloneInt(p.SwapFeePercentage),
		Status:            p.Status,
		LastBlockNumber:   p.LastBlockNumber,
	}
}

func (p *Pool) Ref() market.PoolRef {
	return market.PoolRefFromBalancer(p.ID.Hash())
}

func (p *Pool) IsInitialized() bool {
	if p == nil || len(p.Tokens) < 2 || len(p.Balances) != len(p.Tokens) {
		return false
	}
	for _, token := range p.Tokens {
		balance := p.Balances[token]
		if balance == nil || balance.Sign() < 0 {
			return false
		}
	}
	switch p.Type {
	case PoolTypeWeighted:
		return len(p.Weights) == len(p.Tokens)
	case PoolTypeStable:
		return p.Amplification != nil && p.Amplification.Sign() > 0
	default:
		return false
	}
}

// Apply is the sole entry point for mutating Balancer pool state from chain events.
func (p *Pool) Apply(event PoolEvent) error {
	if event.Meta.PoolID != (PoolID{}) && event.Meta.PoolID != p.ID {
		return fmt.Errorf("event pool %s does not match pool %s", event.Meta.PoolID, p.ID)
	}
	if event.Meta.BlockNumber < p.LastBlockNumber {
		return nil
	}

	var err error
	switch event.Kind {
	case EventKindPoolBalanceChanged:
		err = p.applyPoolBalanceChanged(event)
	case EventKindSwap:
		err = p.applySwap(event)
	case EventKindSwapFeePercentageChanged:
		err = p.applySwapFeePercentageChanged(event)
	case EventKindAmplificationUpdated:
		err = p.applyAmplificationUpdated(event)
	default:
		return fmt.Errorf("unsupported event kind %d", event.Kind)
	}
	if err != nil {
		return err
	}

	if p.Status == market.PoolStatusUnknown || p.Status == market.PoolStatusBootstrapping {
		p.Status = market.PoolStatusSyncing
	}
	if event.Meta.BlockNumber > p.LastBlockNumber {
		p.LastBlockNumber = event.Meta.BlockNumber
	}
	return nil
}

func (p *Pool) applyPoolBalanceChanged(event PoolEvent) error {
	payload := event.PoolBalanceChanged
	if payload == nil {
		return fmt.Errorf("pool balance changed event payload is nil")
	}
	if len(payload.Tokens) != len(payload.Deltas) {
		return fmt.Errorf("pool balance changed has %d tokens and %d deltas", len(payload.Tokens), len(payload.Deltas))
	}
	for i, token := range payload.Tokens {
		if err := p.addBalanceDelta(token, payload.Deltas[i]); err != nil {
			return err
		}
	}
	return nil
}

func (p *Pool) applySwap(event PoolEvent) error {
	payload := event.Swap
	if payload == nil {
		return fmt.Errorf("swap event payload is nil")
	}
	if payload.AmountIn == nil || payload.AmountIn.Sign() < 0 {
		return fmt.Errorf("swap amountIn must be non-negative")
	}
	if payload.AmountOut == nil || payload.AmountOut.Sign() < 0 {
		return fmt.Errorf("swap amountOut must be non-negative")
	}
	if err := p.addBalanceDelta(payload.TokenIn, payload.AmountIn); err != nil {
		return err
	}
	return p.addBalanceDelta(payload.TokenOut, new(big.Int).Neg(payload.AmountOut))
}

func (p *Pool) applySwapFeePercentageChanged(event PoolEvent) error {
	payload := event.SwapFeePercentageChanged
	if payload == nil || payload.SwapFeePercentage == nil {
		return fmt.Errorf("swap fee percentage changed event payload is nil")
	}
	if payload.SwapFeePercentage.Sign() < 0 {
		return fmt.Errorf("swap fee percentage must be non-negative")
	}
	p.SwapFeePercentage = cloneInt(payload.SwapFeePercentage)
	return nil
}

func (p *Pool) applyAmplificationUpdated(event PoolEvent) error {
	if p.Type != PoolTypeStable {
		return fmt.Errorf("amplification updates are only supported for stable pools")
	}
	payload := event.AmplificationUpdated
	if payload == nil || payload.Amplification == nil {
		return fmt.Errorf("amplification updated event payload is nil")
	}
	if payload.Amplification.Sign() <= 0 {
		return fmt.Errorf("amplification must be positive")
	}
	p.Amplification = cloneInt(payload.Amplification)
	return nil
}

func (p *Pool) addBalanceDelta(token common.Address, delta *big.Int) error {
	if delta == nil {
		return fmt.Errorf("balance delta for token %s is nil", token.Hex())
	}
	current, ok := p.Balances[token]
	if !ok {
		return fmt.Errorf("token %s is not part of pool %s", token.Hex(), p.ID)
	}
	next := new(big.Int).Add(current, delta)
	if next.Sign() < 0 {
		return fmt.Errorf("token %s balance would become negative", token.Hex())
	}
	p.Balances[token] = next
	return nil
}

func cloneInt(v *big.Int) *big.Int {
	if v == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(v)
}

func cloneInts(values []*big.Int) []*big.Int {
	out := make([]*big.Int, len(values))
	for i, value := range values {
		out[i] = cloneInt(value)
	}
	return out
}

func cloneAddresses(values []common.Address) []common.Address {
	out := make([]common.Address, len(values))
	copy(out, values)
	return out
}

func newTokenIntMap(tokens []common.Address) map[common.Address]*big.Int {
	out := make(map[common.Address]*big.Int, len(tokens))
	for _, token := range tokens {
		out[token] = big.NewInt(0)
	}
	return out
}

func cloneIntMap(values map[common.Address]*big.Int) map[common.Address]*big.Int {
	out := make(map[common.Address]*big.Int, len(values))
	for token, value := range values {
		out[token] = cloneInt(value)
	}
	return out
}
