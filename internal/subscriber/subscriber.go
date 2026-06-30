package subscriber

import "github.com/brianliu-sysu/arbitrage/internal/blockchain"

// Deprecated: use github.com/brianliu-sysu/arbitrage/internal/blockchain instead.

type (
	Subscriber     = blockchain.PoolClient
	EventHandler   = blockchain.EventHandler
	PoolStateRPC   = blockchain.PoolStateRPC
	PoolMetadata   = blockchain.PoolMetadata
	TokenMetadata  = blockchain.TokenMetadata
	TickData       = blockchain.TickData
)

var NewSubscriber = blockchain.NewSubscriber
