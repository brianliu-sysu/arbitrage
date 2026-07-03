package arbitrage

import "context"

type OpportunityRepository interface {
	Save(ctx context.Context, opportunity *Opportunity) error
	List(ctx context.Context, limit int) ([]*Opportunity, error)
	Delete(ctx context.Context, id string) error
}
