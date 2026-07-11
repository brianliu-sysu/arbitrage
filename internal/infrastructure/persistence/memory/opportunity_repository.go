package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
)

// OpportunityRepository is an in-memory OpportunityRepository.
type OpportunityRepository struct {
	mu            sync.RWMutex
	opportunities map[string]*arbitrage.Opportunity
}

func NewOpportunityRepository() *OpportunityRepository {
	return &OpportunityRepository{opportunities: make(map[string]*arbitrage.Opportunity)}
}

func (r *OpportunityRepository) Save(_ context.Context, opportunity *arbitrage.Opportunity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copyOpportunity := *opportunity
	payload := make([]byte, len(opportunity.Payload))
	copy(payload, opportunity.Payload)
	copyOpportunity.Payload = payload
	r.opportunities[opportunity.ID] = &copyOpportunity
	return nil
}

func (r *OpportunityRepository) Get(_ context.Context, id string) (*arbitrage.Opportunity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	opportunity, ok := r.opportunities[id]
	if !ok {
		return nil, arbitrage.ErrOpportunityNotFound
	}
	copyOpportunity := *opportunity
	payload := make([]byte, len(opportunity.Payload))
	copy(payload, opportunity.Payload)
	copyOpportunity.Payload = payload
	return &copyOpportunity, nil
}

func (r *OpportunityRepository) List(_ context.Context, limit int) ([]*arbitrage.Opportunity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]*arbitrage.Opportunity, 0, len(r.opportunities))
	for _, opportunity := range r.opportunities {
		copyOpportunity := *opportunity
		payload := make([]byte, len(opportunity.Payload))
		copy(payload, opportunity.Payload)
		copyOpportunity.Payload = payload
		items = append(items, &copyOpportunity)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *OpportunityRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.opportunities, id)
	return nil
}
