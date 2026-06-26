-- +goose Up
CREATE TABLE IF NOT EXISTS token_metadata (
    chain_name      TEXT NOT NULL,
    token_address   TEXT NOT NULL,
    symbol          TEXT NOT NULL DEFAULT '',
    decimals        INTEGER NOT NULL DEFAULT 18,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_name, token_address)
);

CREATE INDEX IF NOT EXISTS idx_token_metadata_updated_at
    ON token_metadata (updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS token_metadata;
