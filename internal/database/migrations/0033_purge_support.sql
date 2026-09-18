-- 0033: purge（已归档 issue 的硬删除）放行。
-- 0017 的 scope_content_immutable 与 immutable_provider_fact 对 DELETE 一律
-- 拒绝，Purge 的清扫因此永远失败。删除是已归档议题的合规终点，但放行必须
-- 只发生在明确授权的清扫事务里：触发函数检查事务级自定义 GUC
-- repomesh.purge_mode（Purge 事务内 SET LOCAL 置 on），任何其它路径的
-- DELETE 仍然被拒，不可变语义对常规写入不变。
CREATE OR REPLACE FUNCTION repomesh_issues.scope_content_immutable() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' AND current_setting('repomesh.purge_mode', true) = 'on' THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'scope_content_immutable: issue and conversation content scope rows are append-only'
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION repomesh_issues.immutable_provider_fact() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' AND current_setting('repomesh.purge_mode', true) = 'on' THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'immutable_provider_fact: issues rows can only be tombstoned via removed_at, never deleted'
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;
