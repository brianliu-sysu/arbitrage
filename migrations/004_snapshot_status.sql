-- +goose Up
-- pool_states 快照生命周期状态（由 cmd/snapshot 写入，arbitrage 只加载 READY）
ALTER TABLE pool_states
    ADD COLUMN IF NOT EXISTS snapshot_status TEXT NOT NULL DEFAULT 'INITIALIZING';

CREATE INDEX IF NOT EXISTS idx_pool_states_snapshot_status
    ON pool_states (chain_name, snapshot_status);

-- 已有完整快照的行标记为 READY，便于平滑升级
UPDATE pool_states
SET snapshot_status = 'READY'
WHERE snapshot_status = 'INITIALIZING'
  AND block_number > 0
  AND tick_data IS NOT NULL
  AND tick_data::text <> '{}';

-- +goose Down
DROP INDEX IF EXISTS idx_pool_states_snapshot_status;
ALTER TABLE pool_states DROP COLUMN IF EXISTS snapshot_status;
