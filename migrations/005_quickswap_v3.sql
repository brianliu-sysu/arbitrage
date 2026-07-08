-- QuickSwap V3 pool state tables.

-- QuickSwap V3 sync state tables.

CREATE TABLE IF NOT EXISTS quickswap_pools (
    address BYTEA PRIMARY KEY,
    token0 BYTEA NOT NULL,
    token1 BYTEA NOT NULL,
    fee INTEGER NOT NULL,
    tick_spacing INTEGER NOT NULL,
    status TEXT NOT NULL,
    last_block_number BIGINT NOT NULL DEFAULT 0,
    sqrt_price_x96 NUMERIC NOT NULL DEFAULT 0,
    tick INTEGER NOT NULL DEFAULT 0,
    liquidity NUMERIC NOT NULL DEFAULT 0,
    fee_growth_global0_x128 NUMERIC NOT NULL DEFAULT 0,
    fee_growth_global1_x128 NUMERIC NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS quickswap_pool_ticks (
    pool_address BYTEA NOT NULL REFERENCES quickswap_pools(address) ON DELETE CASCADE,
    tick_index INTEGER NOT NULL,
    liquidity_gross NUMERIC NOT NULL,
    liquidity_net NUMERIC NOT NULL,
    PRIMARY KEY (pool_address, tick_index)
);

CREATE TABLE IF NOT EXISTS quickswap_pool_tick_bitmap (
    pool_address BYTEA NOT NULL REFERENCES quickswap_pools(address) ON DELETE CASCADE,
    word_pos SMALLINT NOT NULL,
    word_value NUMERIC NOT NULL,
    PRIMARY KEY (pool_address, word_pos)
);

CREATE TABLE IF NOT EXISTS quickswap_checkpoints (
    pool_address BYTEA PRIMARY KEY REFERENCES quickswap_pools(address) ON DELETE CASCADE,
    block_number BIGINT NOT NULL,
    block_hash BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS quickswap_snapshots (
    pool_address BYTEA NOT NULL,
    block_number BIGINT NOT NULL,
    sqrt_price_x96 NUMERIC NOT NULL,
    tick INTEGER NOT NULL,
    liquidity NUMERIC NOT NULL,
    fee_growth_global0_x128 NUMERIC NOT NULL DEFAULT 0,
    fee_growth_global1_x128 NUMERIC NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (pool_address, block_number)
);

CREATE TABLE IF NOT EXISTS quickswap_snapshot_ticks (
    pool_address BYTEA NOT NULL,
    block_number BIGINT NOT NULL,
    tick_index INTEGER NOT NULL,
    liquidity_gross NUMERIC NOT NULL,
    liquidity_net NUMERIC NOT NULL,
    PRIMARY KEY (pool_address, block_number, tick_index),
    FOREIGN KEY (pool_address, block_number) REFERENCES quickswap_snapshots(pool_address, block_number) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS quickswap_snapshot_tick_bitmap (
    pool_address BYTEA NOT NULL,
    block_number BIGINT NOT NULL,
    word_pos SMALLINT NOT NULL,
    word_value NUMERIC NOT NULL,
    PRIMARY KEY (pool_address, block_number, word_pos),
    FOREIGN KEY (pool_address, block_number) REFERENCES quickswap_snapshots(pool_address, block_number) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_quickswap_snapshots_pool_block ON quickswap_snapshots(pool_address, block_number DESC);


