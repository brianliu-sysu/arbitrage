-- +goose Up
CREATE TABLE IF NOT EXISTS pool_states_history (
    id              BIGSERIAL PRIMARY KEY,
    pool_address    TEXT NOT NULL,
    block_number    BIGINT NOT NULL,
    tick            INTEGER NOT NULL,
    sqrt_price_x96  TEXT NOT NULL,
    liquidity       TEXT NOT NULL,
    price0_in_1     DOUBLE PRECISION NOT NULL DEFAULT 0,
    recorded_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_history_pool_time
    ON pool_states_history (pool_address, recorded_at DESC);

-- +goose Down
DROP TABLE IF EXISTS pool_states_history;
