package arbitrageapp

import (
	"context"
	"fmt"

	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	"go.uber.org/zap"
)

// OpportunityPublisher delivers discovered opportunities to downstream consumers.
type OpportunityPublisher interface {
	Publish(ctx context.Context, opportunity *domainarb.Opportunity) error
}

// PublishService publishes opportunities through configured publishers.
type PublishService struct {
	publishers []OpportunityPublisher
}

func NewPublishService(publishers ...OpportunityPublisher) *PublishService {
	copied := make([]OpportunityPublisher, len(publishers))
	copy(copied, publishers)
	return &PublishService{publishers: copied}
}

// Publish sends all opportunities to every configured publisher.
func (s *PublishService) Publish(ctx context.Context, opportunities []*domainarb.Opportunity) error {
	for _, opportunity := range opportunities {
		if opportunity == nil {
			continue
		}
		if err := s.PublishOne(ctx, opportunity); err != nil {
			return err
		}
	}
	return nil
}

// PublishOne sends a single opportunity to every configured publisher.
func (s *PublishService) PublishOne(ctx context.Context, opportunity *domainarb.Opportunity) error {
	if opportunity == nil {
		return nil
	}
	for _, publisher := range s.publishers {
		if publisher == nil {
			continue
		}
		if err := publisher.Publish(ctx, opportunity); err != nil {
			return fmt.Errorf("publish opportunity %s: %w", opportunity.ID, err)
		}
	}
	return nil
}

// RepositoryPublisher persists opportunities through OpportunityRepository.
type RepositoryPublisher struct {
	repo domainarb.OpportunityRepository
}

func NewRepositoryPublisher(repo domainarb.OpportunityRepository) *RepositoryPublisher {
	return &RepositoryPublisher{repo: repo}
}

func (p *RepositoryPublisher) Publish(ctx context.Context, opportunity *domainarb.Opportunity) error {
	if p.repo == nil {
		return fmt.Errorf("opportunity repository is nil")
	}
	if err := opportunity.EnsurePayload(); err != nil {
		return fmt.Errorf("encode opportunity payload: %w", err)
	}
	return p.repo.Save(ctx, opportunity)
}

// LogPublisher writes opportunities to the application logger.
type LogPublisher struct {
	logger *zap.Logger
}

func NewLogPublisher(logger *zap.Logger) *LogPublisher {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &LogPublisher{logger: logger}
}

func (p *LogPublisher) Publish(_ context.Context, opportunity *domainarb.Opportunity) error {
	p.logger.Info("arbitrage opportunity",
		zap.String("id", opportunity.ID),
		zap.String("strategy", opportunity.StrategyID),
		zap.Uint64("block", opportunity.BlockNumber),
		zap.String("AmountIn", opportunity.AmountIn.String()),
		zap.String("AmountOut", opportunity.AmountOut.String()),
		zap.String("net_profit", opportunity.NetProfit.String()),
		zap.Int("route_hops", opportunity.Route.Len()),
	)
	return nil
}
