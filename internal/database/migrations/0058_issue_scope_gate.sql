-- 0058:选仓门状态列(建项不选仓,spec 2026-09-20 §2)。
-- {state: pending|resolved, decided_by: manual|ai|timeout, suggested:[...],
--  deadline_at, resolved_at}。deadline_at 只在 ai 模式置(开门时刻+10 分钟);
-- hitl 模式无截止,门无限等待。空缺 = 老 issue,视为 resolved(跳过门)。
--
-- 必须是新列,不能塞现有 jsonb(candidates 等):旧二进制的 discovery.save()
-- 整行重写会无声抹掉子键(reopen.go 等整块赋值路径同理)。门写入只走
-- 单列 CAS UPDATE(internal/discovery/gate.go),绝不进 save()。
ALTER TABLE repomesh_issues.issue_discoveries
    ADD COLUMN IF NOT EXISTS scope_gate jsonb;
