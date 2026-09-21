-- 0059:选仓门(建项不选仓,spec 2026-09-20)带来的两处 schema 放宽。
--
-- 1) 建项聚合可以零仓提交。0017 的 creation_aggregate_complete 要求已提交操作的
--    issue **至少一行 issue_content_scope**,并把 issue/conversation 范围不小于
--    内容范围当硬约束。建项不再选仓后,范围由 ① 需求分析后的**选仓门**后置确认
--    (批量确认端点往 issue_repository_scope 与 issue_content_scope 追加),
--    因此内容范围允许为空;有 scope 时"不小于"的包含关系照旧成立(0 >= 0)。
--    不放宽的话空仓建项在提交时撞 23514('empty issue content scope for operation …'),
--    线上表现为「服务端暂时不可用(HTTP 500)」。
--
-- 2) planning_runs.step 允许 3。0048 的 CHECK (step IN (1,2,4,6)) 写于查漏步
--    (PlanningGapAudit)之前;选仓门被**人工**确认后,协调器要在 ③ 分档前派
--    step=3 的查漏 run,不加 3 会撞 23514(planning_runs_step_check)。
--
-- 两件事同属选仓门这一轮 schema 变更,合并在一个迁移里(迁移号连续,一次到位)。

-- 2) 查漏步(step=3)可派发。
ALTER TABLE repomesh_issues.planning_runs DROP CONSTRAINT IF EXISTS planning_runs_step_check;
ALTER TABLE repomesh_issues.planning_runs
    ADD CONSTRAINT planning_runs_step_check CHECK (step IN (1, 2, 3, 4, 6));

-- 1) 建项聚合:去掉"内容范围必须非空",其余完整性检查原样保留。
CREATE OR REPLACE FUNCTION repomesh_issues.creation_aggregate_complete() RETURNS trigger AS $$
DECLARE
    op record;
    n bigint;
    scope_count bigint;
BEGIN
    SELECT * INTO op FROM repomesh_issues.creation_operations
        WHERE project_id = NEW.project_id AND actor = NEW.actor
          AND entry = NEW.entry AND creation_id = NEW.creation_id;
    IF op.removed_at IS NOT NULL THEN RETURN NULL; END IF;
    IF op.issue_id IS NULL THEN RETURN NULL; END IF; -- uncommitted placeholder
    IF op.canonical_input IS NULL OR op.exact_input IS NULL OR op.receipt IS NULL THEN
        RAISE EXCEPTION 'creation_aggregate_complete: committed operation missing canonical input, exact input, or receipt'
            USING ERRCODE = 'check_violation';
    END IF;
    -- schema supports the aggregate
    IF op.schema_version IS DISTINCT FROM 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: unsupported schema version %', op.schema_version
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.issues
        WHERE project_id = op.project_id AND id = op.issue_id
          AND creation_operation_id = op.id
          AND main_changeset_id = op.main_changeset_id
          AND main_conversation_id = op.conversation_id
          AND initial_configuration_revision = op.initial_configuration_revision;
    IF n IS DISTINCT FROM 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: issue row missing or identity mismatch for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.changesets
        WHERE project_id = op.project_id AND issue_id = op.issue_id AND id = op.main_changeset_id AND kind = 'main';
    IF n IS DISTINCT FROM 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: main changeset missing for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.conversations
        WHERE project_id = op.project_id AND id = op.conversation_id
          AND created_by_operation_id = op.id
          AND title_origin_operation_id = op.id;
    IF n IS DISTINCT FROM 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: main conversation missing or wrong origin for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.page_sources
        WHERE project_id = op.project_id AND operation_id = op.id
          AND issue_id = op.issue_id AND conversation_id = op.conversation_id;
    IF n IS DISTINCT FROM 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: page source missing for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.conversation_cards
        WHERE project_id = op.project_id AND source_id = op.id AND operation_id = op.id;
    IF n IS DISTINCT FROM 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: conversation card missing for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    -- 内容范围可以为零:建项不选仓,范围由选仓门后置确认(spec 2026-09-20 §3.1)。
    SELECT count(*) INTO scope_count FROM repomesh_issues.issue_content_scope
        WHERE project_id = op.project_id AND issue_id = op.issue_id
          AND introduced_by_operation = op.id;
    SELECT count(*) INTO n FROM repomesh_issues.issue_repository_scope
        WHERE project_id = op.project_id AND issue_id = op.issue_id;
    IF n < scope_count THEN
        RAISE EXCEPTION 'creation_aggregate_complete: repository scope smaller than content scope for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.conversation_content_scope
        WHERE project_id = op.project_id AND conversation_id = op.conversation_id
          AND introduced_by_operation = op.id;
    IF n < scope_count THEN
        RAISE EXCEPTION 'creation_aggregate_complete: conversation scope smaller than issue scope for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.continuation_work
        WHERE project_id = op.project_id AND issue_id = op.issue_id
          AND cause_operation_id = op.id AND kind = 'issue_continue' AND state = 'blocked';
    IF n IS DISTINCT FROM 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: blocked continuation work missing for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*) INTO n FROM repomesh_issues.issue_events
        WHERE issue_id = op.issue_id;
    IF n < 1 THEN
        RAISE EXCEPTION 'creation_aggregate_complete: no creation event for operation %', op.id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
