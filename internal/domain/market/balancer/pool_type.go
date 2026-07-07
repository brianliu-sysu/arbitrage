package balancer

import "fmt"

// PoolType limits the Balancer pools supported by this sync path.
type PoolType string

const (
	PoolTypeWeighted PoolType = "weighted"
	PoolTypeStable   PoolType = "stable"
)

func (t PoolType) Validate() error {
	switch t {
	case PoolTypeWeighted, PoolTypeStable:
		return nil
	default:
		return fmt.Errorf("unsupported balancer pool type %q", t)
	}
}
