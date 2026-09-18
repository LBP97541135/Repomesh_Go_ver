-- 0032: cursors 的 kind 枚举补 'issues'。
-- issue 列表分页(B07)写 kind='issues' 的 cursor,0005 定下的 CHECK 枚举
-- 没包含它——一页装不下(条目数超过 limit)时 cursor INSERT 违反约束,
-- 整个列表端点 503。演示库 11 条 issue、limit=3 即必现。
ALTER TABLE repomesh_projects.cursors
    DROP CONSTRAINT cursors_kind_check;
ALTER TABLE repomesh_projects.cursors
    ADD CONSTRAINT cursors_kind_check
    CHECK (kind IN ('projects', 'repositories', 'model', 'execution', 'providers', 'issues'));
