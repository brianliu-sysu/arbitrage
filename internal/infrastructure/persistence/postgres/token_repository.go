package postgres

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

// TokenRepository persists ERC20 token metadata in PostgreSQL.
type TokenRepository struct {
	db *DB
}

func NewTokenRepository(db *DB) *TokenRepository {
	return &TokenRepository{db: db}
}

func (r *TokenRepository) Save(ctx context.Context, token *asset.Token) error {
	if token == nil {
		return nil
	}
	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO tokens (address, symbol, decimals, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (address) DO UPDATE SET
			symbol = EXCLUDED.symbol,
			decimals = EXCLUDED.decimals,
			updated_at = NOW()
	`,
		codec.AddressToBytes(token.Address),
		token.Symbol,
		int16(token.Decimal),
	)
	if err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	return nil
}

func (r *TokenRepository) Get(ctx context.Context, address common.Address) (*asset.Token, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT symbol, decimals
		FROM tokens
		WHERE address = $1
	`, codec.AddressToBytes(address))

	var symbol string
	var decimals int16
	if err := row.Scan(&symbol, &decimals); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan token: %w", err)
	}
	return &asset.Token{
		Address: address,
		Symbol:  symbol,
		Decimal: uint8(decimals),
	}, nil
}

func (r *TokenRepository) GetMany(ctx context.Context, addresses []common.Address) (map[common.Address]*asset.Token, error) {
	if len(addresses) == 0 {
		return map[common.Address]*asset.Token{}, nil
	}

	raw := make([][]byte, len(addresses))
	for i, address := range addresses {
		raw[i] = codec.AddressToBytes(address)
	}

	rows, err := r.db.pool.Query(ctx, `
		SELECT address, symbol, decimals
		FROM tokens
		WHERE address = ANY($1)
	`, raw)
	if err != nil {
		return nil, fmt.Errorf("query tokens: %w", err)
	}
	defer rows.Close()

	out := make(map[common.Address]*asset.Token, len(addresses))
	for rows.Next() {
		var address []byte
		var symbol string
		var decimals int16
		if err := rows.Scan(&address, &symbol, &decimals); err != nil {
			return nil, fmt.Errorf("scan token row: %w", err)
		}
		addr := codec.BytesToAddress(address)
		out[addr] = &asset.Token{
			Address: addr,
			Symbol:  symbol,
			Decimal: uint8(decimals),
		}
	}
	return out, rows.Err()
}
