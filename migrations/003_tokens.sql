-- ERC20 token metadata cache.

CREATE TABLE IF NOT EXISTS tokens (
    address BYTEA PRIMARY KEY,
    symbol TEXT NOT NULL DEFAULT '',
    decimals SMALLINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
