-- 指令进度与防重复执行（SQLite 版）

ALTER TABLE commands ADD COLUMN last_stage    TEXT NOT NULL DEFAULT '';
ALTER TABLE commands ADD COLUMN last_stage_at TEXT;

-- 历史数据回填：优先取 result.stage，其次取 task_hosts.stage。
UPDATE commands SET
    last_stage = COALESCE(
        NULLIF(json_extract(result, '$.stage'), ''),
        NULLIF((SELECT stage FROM task_hosts WHERE task_hosts.id = commands.task_host_id), ''),
        '')
WHERE EXISTS (SELECT 1 FROM task_hosts WHERE task_hosts.id = commands.task_host_id)
  AND last_stage = '';

-- 所有历史指令都回填。
UPDATE commands
SET last_stage_at = COALESCE(finished_at, started_at, dispatched_at, created_at)
WHERE last_stage_at IS NULL;

-- 清理历史重复活动指令：同一 task_host 只保留 id 最小的一条，其余取消。
WITH ranked AS (
    SELECT id, row_number() OVER (PARTITION BY task_host_id ORDER BY id) AS rn
    FROM commands
    WHERE task_host_id IS NOT NULL
      AND status IN ('pending','dispatched','accepted','running')
)
UPDATE commands
SET status='canceled', finished_at=CURRENT_TIMESTAMP,
    log=COALESCE(log,'')||'[migration] duplicate active command canceled'||char(10)
WHERE id IN (SELECT id FROM ranked WHERE rn > 1);

CREATE UNIQUE INDEX IF NOT EXISTS uq_commands_task_host_active
    ON commands(task_host_id)
    WHERE task_host_id IS NOT NULL
      AND status IN ('pending','dispatched','accepted','running');

CREATE INDEX IF NOT EXISTS idx_commands_active_lease
    ON commands(lease_expire_at)
    WHERE status IN ('dispatched','accepted','running');

CREATE INDEX IF NOT EXISTS idx_commands_active_stage
    ON commands(last_stage, last_stage_at)
    WHERE status IN ('dispatched','accepted','running');

CREATE INDEX IF NOT EXISTS idx_commands_task_host_id
    ON commands(task_host_id, id DESC)
    WHERE task_host_id IS NOT NULL;
