-- 0053: issue 级的"人工参与 / 自动托管"模式落到服务端。
--
-- 2026-09-20 用户裁定：物化（⑤）与分档审批（③）是**人审门**，自动托管只是"处理员
-- 代行"，不能反过来把"人审"这件事整个吃掉。而此前这个模式只存在浏览器的
-- sessionStorage（WorkbenchPage 的 hitlKey），Go 侧 grep hitl 零命中 —— 于是
-- 协调器的自动托管循环**无条件**代行 ③ 与 ⑤，人工参与模式下也一样：界面上选了
-- "人工参与"，物化却从来不找审核台。
--
-- 现在把它变成服务端事实：issue 建项时写入，读面随 issue 详情返回，协调器按它停门。
-- 默认 hitl（门等真人）—— 最保守的缺省，不替任何人做主。

SET LOCAL search_path = public, pg_catalog;

ALTER TABLE repomesh_issues.issues
    ADD COLUMN IF NOT EXISTS hitl_mode text NOT NULL DEFAULT 'hitl';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'issues_hitl_mode_check'
    ) THEN
        ALTER TABLE repomesh_issues.issues
            ADD CONSTRAINT issues_hitl_mode_check CHECK (hitl_mode IN ('ai', 'hitl'));
    END IF;
END $$;
