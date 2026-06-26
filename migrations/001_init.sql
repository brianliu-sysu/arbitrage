-- +goose Up
-- ============================================================
-- 核心状态表：保存每个池子的最新运行时状态
-- ============================================================
CREATE TABLE IF NOT EXISTS pool_states (
    pool_address    TEXT NOT NULL,
    chain_name      TEXT NOT NULL DEFAULT '',
    block_number    BIGINT NOT NULL,
    tick            INTEGER NOT NULL,
    sqrt_price_x96  TEXT NOT NULL,
    liquidity       TEXT NOT NULL,
    price0_in_1     DOUBLE PRECISION NOT NULL DEFAULT 0,
    token0_symbol   TEXT NOT NULL DEFAULT '',
    token1_symbol   TEXT NOT NULL DEFAULT '',
    fee             INTEGER NOT NULL DEFAULT 0,
    tick_data       JSONB NOT NULL DEFAULT '{}',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (pool_address, chain_name)
);
CREATE INDEX IF NOT EXISTS idx_pool_states_updated ON pool_states (updated_at);

-- ============================================================
-- 历史记录表：按时间记录每次价格/流动性变动（用于回溯分析）
-- ============================================================
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
DROP TABLE IF EXISTS pool_states;
