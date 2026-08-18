-- 生成任务表（docs/04 §1.4 命名规范）
CREATE TABLE IF NOT EXISTS generation_tasks (
    id              BIGINT PRIMARY KEY,
    user_id         TEXT NOT NULL,
    request_id      TEXT NOT NULL,
    task_type       TEXT NOT NULL,
    model_key       TEXT NOT NULL,
    prompt          TEXT NOT NULL DEFAULT '',
    ref_asset_urls  JSONB NOT NULL DEFAULT '[]',
    params          JSONB NOT NULL DEFAULT '{}',
    canvas_ctx      JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'PENDING',
    progress        INT NOT NULL DEFAULT 0,
    outputs         JSONB NOT NULL DEFAULT '[]',
    error_reason    TEXT NOT NULL DEFAULT '',
    provider_job_id TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_generation_tasks_user_request
    ON generation_tasks (user_id, request_id);
CREATE INDEX IF NOT EXISTS idx_generation_tasks_user_id_created_at
    ON generation_tasks (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_generation_tasks_status
    ON generation_tasks (status);
