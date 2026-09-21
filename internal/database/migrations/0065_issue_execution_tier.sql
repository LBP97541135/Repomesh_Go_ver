-- 0065: 建单时的监管强度从**两档**（自动托管 / 人工参与审计）改成**三档**
--      （全自动 / 半自动 / 人工审核），半自动由用户自选人工卡点。
--
-- 用户 2026-09-21 原话："把自动托管/人工参与审计调整一下，换成三档，全自动，半自动，
-- 人工审核，半自动模式下，我们可以自行调整人工审核点"。
--
-- 为什么不能只改界面：现在这一格是 `hitl_mode`（'ai' | 'hitl'），而自动托管循环
-- **就是按它停门的**（discovery_auto.go 的 `WHERE COALESCE(i.hitl_mode,'hitl')='ai'`）。
-- 只把按钮画成三档、后端仍只有两值，中间的"半自动"就会悄悄退化成"全自动"——
-- 那正是这套系统反复出过的那类假功能。所以档位与卡点都必须落成**服务端事实**。
--
-- 档位的域不变量照抄既有的项目监管策略（internal/humancontrol/policy.go，迁移 0040），
-- 不另立一套语义：
--   · auto              —— 卡点必须为空（带卡点的 auto 是自相矛盾）；
--   · supervised（半自动）—— 至少一个卡点；
--   · manual_controlled —— 六个卡点全要。
-- 卡点取值同样是那六个：repository_scope / specification / execution /
-- validation / delivery / exception_escalation。
--
-- 与 hitl_mode 的关系（**不是并列的两套，而是派生**）：
--   auto / supervised        → hitl_mode = 'ai'  （发现链自动推进）
--   manual_controlled        → hitl_mode = 'hitl'（发现链的门等真人）
-- 保留 hitl_mode 是因为自动托管循环与既有读面都在用它；改由 execution_mode 派生，
-- 是为了让"半自动"能被自动托管**按卡点**停住，而不是整条链都停下来等人。
ALTER TABLE repomesh_issues.issues
    -- 默认值必须**本身是一对合法组合**：形状约束要求 manual_controlled 正好六个卡点，
    -- 而卡点列的默认只能是空数组 —— 两者凑一起就是"自相矛盾的默认值"，任何裸插入
    -- （测试夹具、以及将来任何不显式给档位的写路径）都会当场违反 CHECK。
    -- 所以默认取 auto + 空卡点（这是唯一允许空数组的形状）。
    --
    -- 这**不是**把默认强度放松成"全自动"：真正建单的那条路（internal/issues 的
    -- parseNewInput）永远显式给档位，缺字段时按 hitlMode 派生成 manual_controlled
    -- （最保守）—— 默认值在这里只是"让不经过应用层的插入也合法"的兜底。
    ADD COLUMN IF NOT EXISTS execution_mode text NOT NULL DEFAULT 'auto',
    ADD COLUMN IF NOT EXISTS required_checkpoints jsonb NOT NULL DEFAULT '[]'::jsonb;

-- ⚠️ 回填 UPDATE 刻意放在**本文件最后**，见文末的说明 —— 顺序反了部署会失败。
ALTER TABLE repomesh_issues.issues
    DROP CONSTRAINT IF EXISTS issues_execution_mode_check;
ALTER TABLE repomesh_issues.issues
    ADD CONSTRAINT issues_execution_mode_check
    CHECK (execution_mode IN ('auto', 'supervised', 'manual_controlled'));

-- 形状约束写进存储层：三档不是"严一点/松一点"，是**三种形状**。
-- 只靠应用层校验的话，任何一条绕过应用的写路径都能造出"auto 带卡点"这种自相矛盾的行。
ALTER TABLE repomesh_issues.issues
    DROP CONSTRAINT IF EXISTS issues_checkpoint_shape_check;
ALTER TABLE repomesh_issues.issues
    ADD CONSTRAINT issues_checkpoint_shape_check CHECK (
        jsonb_typeof(required_checkpoints) = 'array'
        AND (execution_mode <> 'auto' OR jsonb_array_length(required_checkpoints) = 0)
        AND (execution_mode <> 'supervised' OR jsonb_array_length(required_checkpoints) >= 1)
        AND (execution_mode <> 'manual_controlled' OR jsonb_array_length(required_checkpoints) = 6)
    );

-- 回填：存量行按各自的 hitl_mode 如实翻译成对应档位，**不改变任何一条既有需求的
-- 实际行为**：
--   · hitl_mode='ai'（自动托管）  → auto + 空卡点，语义与今天一致；
--   · hitl_mode='hitl'（门等真人）→ manual_controlled + 六个卡点，语义与今天一致。
--
-- ⚠️ 这一段必须放在**所有 ALTER TABLE 之后**，否则线上会直接部署失败。
--
-- 2026-09-21 实测踩到（部署 job 在 0.038 秒内失败）：
--     ERROR: cannot ALTER TABLE "issues" because it has pending trigger events   (SQLSTATE 55006)
-- 原因是迁移跑在**一个事务**里：先 UPDATE 了 issues，该表上有延迟约束触发器，
-- 于是这个事务里留下了"待处理的触发器事件"；紧接着再对同一张表 ALTER，
-- Postgres 直接拒绝（55006）。把 UPDATE 挪到末尾就没有这个问题 ——
-- 事务提交时事件自然处理掉，后面不再有 ALTER。
--
-- 别把这段"挪回上面"图整齐：整齐的代价是每次部署都失败，而且失败发生在
-- **迁移步**（服务照常跑旧版本、回滚也救不了这一条），排查起来要翻 PG 日志才看得到真因。
UPDATE repomesh_issues.issues
SET execution_mode = CASE WHEN COALESCE(hitl_mode, 'hitl') = 'ai' THEN 'auto' ELSE 'manual_controlled' END,
    required_checkpoints = CASE
        WHEN COALESCE(hitl_mode, 'hitl') = 'ai' THEN '[]'::jsonb
        ELSE '["repository_scope","specification","execution","validation","delivery","exception_escalation"]'::jsonb
    END
WHERE required_checkpoints = '[]'::jsonb
  AND COALESCE(hitl_mode, 'hitl') <> 'ai';
