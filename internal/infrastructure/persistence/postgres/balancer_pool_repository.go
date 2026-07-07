package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

// BalancerPoolRepository persists Balancer pool aggregate state in PostgreSQL.
type BalancerPoolRepository struct {
	db *DB
}

func NewBalancerPoolRepository(db *DB) *BalancerPoolRepository {
	return &BalancerPoolRepository{db: db}
}

func (r *BalancerPoolRepository) Save(ctx context.Context, pool *marketbalancer.Pool) error {
	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO balancer_pools (
			pool_id, address, vault, pool_type, tokens, balances, weights,
			amplification, swap_fee_percentage, status, last_block_number, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())
		ON CONFLICT (pool_id) DO UPDATE SET
			address = EXCLUDED.address,
			vault = EXCLUDED.vault,
			pool_type = EXCLUDED.pool_type,
			tokens = EXCLUDED.tokens,
			balances = EXCLUDED.balances,
			weights = EXCLUDED.weights,
			amplification = EXCLUDED.amplification,
			swap_fee_percentage = EXCLUDED.swap_fee_percentage,
			status = EXCLUDED.status,
			last_block_number = EXCLUDED.last_block_number,
			updated_at = NOW()
	`,
		pool.ID.Hash().Bytes(),
		pool.Address.Bytes(),
		pool.Vault.Bytes(),
		string(pool.Type),
		mustJSON(balancerTokensToStrings(pool.Tokens)),
		mustJSON(balancerIntMapToStrings(pool.Balances)),
		mustJSON(balancerIntMapToStrings(pool.Weights)),
		pool.Amplification.String(),
		pool.SwapFeePercentage.String(),
		string(pool.Status),
		pool.LastBlockNumber,
	)
	if err != nil {
		return fmt.Errorf("upsert balancer pool: %w", err)
	}
	return nil
}

func (r *BalancerPoolRepository) Get(ctx context.Context, id marketbalancer.PoolID) (*marketbalancer.Pool, error) {
	row := r.db.pool.QueryRow(ctx, `
		SELECT address, vault, pool_type, tokens, balances, weights,
		       amplification, swap_fee_percentage, status, last_block_number
		FROM balancer_pools
		WHERE pool_id = $1
	`, id.Hash().Bytes())

	var (
		addressRaw, vaultRaw []byte
		poolTypeRaw          string
		tokensRaw            []byte
		balancesRaw          []byte
		weightsRaw           []byte
		amplificationRaw     string
		swapFeeRaw           string
		statusRaw            string
		lastBlock            uint64
	)
	if err := row.Scan(&addressRaw, &vaultRaw, &poolTypeRaw, &tokensRaw, &balancesRaw, &weightsRaw, &amplificationRaw, &swapFeeRaw, &statusRaw, &lastBlock); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan balancer pool: %w", err)
	}

	tokens, err := balancerTokensFromJSON(tokensRaw)
	if err != nil {
		return nil, err
	}
	pool, err := marketbalancer.NewPool(
		id,
		common.BytesToAddress(addressRaw),
		common.BytesToAddress(vaultRaw),
		marketbalancer.PoolType(poolTypeRaw),
		tokens,
	)
	if err != nil {
		return nil, err
	}
	pool.Balances, err = balancerIntMapFromJSON(balancesRaw)
	if err != nil {
		return nil, err
	}
	pool.Weights, err = balancerIntMapFromJSON(weightsRaw)
	if err != nil {
		return nil, err
	}
	pool.Amplification = parseNumericString(amplificationRaw)
	pool.SwapFeePercentage = parseNumericString(swapFeeRaw)
	pool.Status = market.PoolStatus(statusRaw)
	pool.LastBlockNumber = lastBlock
	return pool, nil
}

func (r *BalancerPoolRepository) Delete(ctx context.Context, id marketbalancer.PoolID) error {
	_, err := r.db.pool.Exec(ctx, `DELETE FROM balancer_pools WHERE pool_id = $1`, id.Hash().Bytes())
	if err != nil {
		return fmt.Errorf("delete balancer pool: %w", err)
	}
	return nil
}

func (r *BalancerPoolRepository) AdvanceSyncProgress(ctx context.Context, id marketbalancer.PoolID, blockNumber uint64) error {
	return r.AdvanceSyncProgressMany(ctx, []marketbalancer.PoolID{id}, blockNumber)
}

func (r *BalancerPoolRepository) AdvanceSyncProgressMany(ctx context.Context, ids []marketbalancer.PoolID, blockNumber uint64) error {
	if len(ids) == 0 {
		return nil
	}
	poolIDs := make([][]byte, len(ids))
	for i, id := range ids {
		poolIDs[i] = id.Hash().Bytes()
	}
	tag, err := r.db.pool.Exec(ctx, `
		UPDATE balancer_pools SET
			last_block_number = GREATEST(last_block_number, $2),
			status = CASE WHEN status = $3 THEN $4 ELSE status END,
			updated_at = NOW()
		WHERE pool_id = ANY($1)
	`,
		poolIDs,
		blockNumber,
		string(market.PoolStatusCatchingUp),
		string(market.PoolStatusSyncing),
	)
	if err != nil {
		return fmt.Errorf("advance balancer sync progress: %w", err)
	}
	if tag.RowsAffected() != int64(len(ids)) {
		return fmt.Errorf("expected to update %d balancer pools, updated %d", len(ids), tag.RowsAffected())
	}
	return nil
}

func mustJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}

func balancerTokensToStrings(tokens []common.Address) []string {
	out := make([]string, len(tokens))
	for i, token := range tokens {
		out[i] = token.Hex()
	}
	return out
}

func balancerTokensFromJSON(raw []byte) ([]common.Address, error) {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("decode balancer tokens: %w", err)
	}
	tokens := make([]common.Address, len(values))
	for i, value := range values {
		tokens[i] = common.HexToAddress(value)
	}
	return tokens, nil
}

func balancerIntMapToStrings(values map[common.Address]*big.Int) map[string]string {
	out := make(map[string]string, len(values))
	for token, value := range values {
		if value == nil {
			out[token.Hex()] = "0"
			continue
		}
		out[token.Hex()] = value.String()
	}
	return out
}

func balancerIntMapFromJSON(raw []byte) (map[common.Address]*big.Int, error) {
	var values map[string]string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("decode balancer int map: %w", err)
	}
	out := make(map[common.Address]*big.Int, len(values))
	for token, value := range values {
		out[common.HexToAddress(token)] = parseNumericString(value)
	}
	return out, nil
}
