package arbitrage

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Opportunity is a persisted arbitrage opportunity discovered by the scanner.
type Opportunity struct {
	ID          string
	PoolAddress common.Address
	BlockNumber uint64
	Payload     []byte
	CreatedAt   time.Time
}
