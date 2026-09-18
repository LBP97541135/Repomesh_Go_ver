-- 0029: 任务树读面的展示列(下发任务树 · 左树)。
-- batch_no: 计划内批次号(计划 execution_batches 的序);物化写入端未填时为 NULL。
-- conversation_id: 该任务协作房间的会话 id(消息流读面定位键)。
-- leader_label / worker_label: 执行者显示名。真实指派走 assignee_agent_id +
-- 花名册 join;这两列是编组装时写入的显示快照,读面直接透出。
ALTER TABLE public.tasks ADD COLUMN IF NOT EXISTS batch_no integer;
ALTER TABLE public.tasks ADD COLUMN IF NOT EXISTS conversation_id text;
ALTER TABLE public.tasks ADD COLUMN IF NOT EXISTS leader_label text;
ALTER TABLE public.tasks ADD COLUMN IF NOT EXISTS worker_label text;
