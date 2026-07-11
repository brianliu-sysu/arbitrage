package arbitrage

import (
	"context"
	"errors"
)

// ErrOpportunityNotFound is returned when an opportunity id is unknown.
var ErrOpportunityNotFound = errors.New("opportunity not found")

type OpportunityRepository interface {
	Save(ctx context.Context, opportunity *Opportunity) error
	Get(ctx context.Context, id string) (*Opportunity, error)
	List(ctx context.Context, limit int) ([]*Opportunity, error)
	Delete(ctx context.Context, id string) error
}
