package asset

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

type TokenRepository interface {
	Save(ctx context.Context, token *Token) error
	Get(ctx context.Context, address common.Address) (*Token, error)
	GetMany(ctx context.Context, addresses []common.Address) (map[common.Address]*Token, error)
}
