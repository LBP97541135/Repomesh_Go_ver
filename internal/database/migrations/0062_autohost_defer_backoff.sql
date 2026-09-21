-- 0062: 自动托管的「反复被拒」退避要**落库**，否则每次进程重启就归零。
--
-- 为什么必须落库（2026-09-21 线上实测）：
--   协调器的自动托管循环里，`attempts` 是**进程内存**的计数。此前一条 issue 的分档把
--   全部仓库判成「排除」，发现链如实拒绝（no repositories selected），而错误分支只有
--   固定 10 秒退避 —— 它每 10 秒撞一次墙、永远不停。
--   我给错误分支补了「连续 3 次 → 退到 5 分钟」的熔断（与成功分支对称），但**计数仍在
--   内存里**：线上实测 11:16:14 熔断触发 → 11:16:24 coordinator 重启 → 11:16:35 新进程
--   又从零开始数。而今天为了推修复重启了很多次，等于熔断一直没真正生效。
--
--   所以把「连续被拒次数」与「退避到什么时候」写进库：进程重启后退避仍然有效，
--   该 issue 在到期前**根本不会被 pendingIssues 取出来**（查询里加了这一条），
--   既不刷屏、也不占自动托管的轮次。
--
-- 为什么放这两列而不是新开一张表：
--   · 这是 issue 发现链的**推进状态**，与 analysis/candidates/approval 同属一行；
--   · 一个 issue 只有一份，不存在"多份退避记录"；开新表就要 join，收益为零。
--
-- 复位规则：一旦该 issue 的某一步**推进成功**（done/working 的成功分支），
-- 计数归零、退避清空 —— 有人补了仓库、或判据变了，它应当立刻恢复被处理。
ALTER TABLE repomesh_issues.issue_discoveries
    ADD COLUMN IF NOT EXISTS autohost_defer_count integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS autohost_deferred_until timestamptz;

-- 取待处理 issue 时按这一列过滤，给它一个索引（pendingIssues 每 3 秒跑一次）。
CREATE INDEX IF NOT EXISTS issue_discoveries_autohost_deferred_until_idx
    ON repomesh_issues.issue_discoveries (autohost_deferred_until);
