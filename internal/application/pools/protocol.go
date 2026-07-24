package poolsapp

import (
	"context"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	"github.com/ethereum/go-ethereum/common"
)

type TokenMetadataResolver func(context.Context, ...common.Address) (map[common.Address]*asset.Token, error)

// ProtocolAdapter supplies pool metadata and diagnostics for one protocol.
type ProtocolAdapter interface {
	Type() string
	List(context.Context) ([]PoolInfo, error)
	Diagnostics(context.Context, DiagnosticsRequest, uint64, TokenMetadataResolver) (*DiagnosticsResponse, error)
	AppendMismatches(context.Context, uint64, TokenMetadataResolver, *[]DiagnosticsResponse) error
}

// ServiceDeps contains protocol-independent dependencies for the pools service.
type ServiceDeps struct {
	Protocols []ProtocolAdapter
	Tokens    TokenService
	Head      HeadBlockReader
}

type TokenService interface {
	Resolve(context.Context, []common.Address) (map[common.Address]*asset.Token, error)
}
