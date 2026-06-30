-- +goose Up
-- 链级同步进度：LastProcessedBlock、Reorg checkpoint
CREATE TABLE IF NOT EXISTS chain_sync_state (
    chain_name             TEXT PRIMARY KEY,
    last_processed_block   BIGINT NOT NULL DEFAULT 0,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS chain_sync_state;
