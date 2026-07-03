package syncapp

import (
	"context"
	"fmt"
)

// HealthReport aggregates dependency health probe results.
type HealthReport struct {
	Healthy bool
	Checks  map[string]error
}

// HealthService checks external dependencies used by sync.
type HealthService struct {
	probes []HealthProbe
}

func NewHealthService(probes ...HealthProbe) *HealthService {
	return &HealthService{probes: probes}
}

func (s *HealthService) Check(ctx context.Context) HealthReport {
	checks := make(map[string]error, len(s.probes))
	healthy := true
	for _, probe := range s.probes {
		err := probe.Ping(ctx)
		checks[probe.Name()] = err
		if err != nil {
			healthy = false
		}
	}
	return HealthReport{Healthy: healthy, Checks: checks}
}

func (s *HealthService) CheckOrError(ctx context.Context) error {
	report := s.Check(ctx)
	if report.Healthy {
		return nil
	}
	for name, err := range report.Checks {
		if err != nil {
			return fmt.Errorf("health check failed for %s: %w", name, err)
		}
	}
	return nil
}
