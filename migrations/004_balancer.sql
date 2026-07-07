CREATE TABLE IF NOT EXISTS balancer_pools (
    pool_id BYTEA PRIMARY KEY,
    address BYTEA NOT NULL,
    vault BYTEA NOT NULL,
    pool_type TEXT NOT NULL,
    tokens JSONB NOT NULL,
    balances JSONB NOT NULL,
    weights JSONB NOT NULL,
    amplification NUMERIC(78,0) NOT NULL DEFAULT 0,
    swap_fee_percentage NUMERIC(78,0) NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'unknown',
    last_block_number BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (pool_type IN ('weighted', 'stable'))
);

CREATE TABLE IF NOT EXISTS balancer_snapshots (
    pool_id BYTEA NOT NULL,
    block_number BIGINT NOT NULL,
    tokens JSONB NOT NULL,
    balances JSONB NOT NULL,
    weights JSONB NOT NULL,
    amplification NUMERIC(78,0) NOT NULL DEFAULT 0,
    swap_fee_percentage NUMERIC(78,0) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (pool_id, block_number)
);

CREATE TABLE IF NOT EXISTS balancer_checkpoints (
    pool_id BYTEA PRIMARY KEY,
    block_number BIGINT NOT NULL,
    block_hash BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

