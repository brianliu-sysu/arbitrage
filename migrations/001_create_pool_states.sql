-- +goose Up
CREATE TABLE IF NOT EXISTS pool_states (
    pool_address    TEXT PRIMARY KEY,
    block_number    BIGINT NOT NULL,
    tick            INTEGER NOT NULL,
    sqrt_price_x96  TEXT NOT NULL,
    liquidity       TEXT NOT NULL,
    price0_in_1     DOUBLE PRECISION NOT NULL DEFAULT 0,
    tick_data       JSONB NOT NULL DEFAULT '{}',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_pool_states_updated ON pool_states (updated_at);

-- +goose Down
DROP TABLE IF EXISTS pool_states;
