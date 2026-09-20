-- 0059: Manager 双向桥的投递台账（设计 §9-②3，manager-bridge.md）。
-- 一张表兼两用：② 的"已送达"标记（human→room）与 ③ 的回声去重（room→human，
-- matrix_event_id 唯一）。status: pending / sent / received / skipped。
CREATE TABLE IF NOT EXISTS repomesh_messages.bridge_deliveries (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    direction        text NOT NULL CHECK (direction IN ('human','manager')),
    issue_id         text NOT NULL,
    conversation_id  text NOT NULL,
    submission_id    text,          -- human 方向 = message_submissions.id；manager 方向 NULL
    matrix_event_id  text,          -- sent 后回填；manager 方向即去重键
    actor            text NOT NULL DEFAULT '',   -- 谁说的话（展示用前缀）
    body             text NOT NULL,
    status           text NOT NULL DEFAULT 'pending',
    attempts         int  NOT NULL DEFAULT 0,
    last_error       text,
    created_at       timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at       timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT bridge_deliveries_event UNIQUE (matrix_event_id)
);
CREATE INDEX IF NOT EXISTS bridge_deliveries_pending ON repomesh_messages.bridge_deliveries (direction, status, created_at);
