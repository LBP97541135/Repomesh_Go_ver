-- 0047: 参与权探测缓存(actor × repository_id)。
--
-- 背景(2026-09-20 线上实测):项目目录从 2 个仓涨到 49 个,创建 issue 的 options
-- 端点每次都串行重探 GitHub 参与权,49 × ~300ms 直接打爆请求的 15s 预算,整个
-- 表单 503 AUTHORIZATION_UNCONFIRMED。
--
-- 这张表是那些探测结果的落点:按 actor × repository_id 存 status + observed_at,
-- 5 分钟 TTL 内复用,过期/未验的仓并发现验后回填。缓存写丢只代价一次重探,
-- 绝不影响正确性;提交路径(CheckIssueObservation / CheckProjectObservation 的
-- 60s 新鲜窗口)不走缓存,仍现验。
--
-- 只存 actor 级的参与权结论,**不存 App(部署级)能力** —— 把它一起缓存会把
-- "App 已失去该仓授权"盖成 allowed,界面显示可选、建 issue 时才硬失败。App
-- 能力由 observeProjectRepositories 每次现探。主线能整体缓存是因为它把判定换成
-- 了 actor 级的 UserWorkCapability(OAuth App 分支的改动),LBP 不是那个模型。
--
-- 表结构(对齐主线 0033 + 0034 两次迁移的最终形态,省得以后为同一张表再发一版):
--   observed_at  —— 该次探测的时间,TTL 判定的唯一依据
--   external_id / display_name —— 命中缓存时重建观测用,不再回打 GitHub
--   host/owner/name —— 仓定位(LBP 的 RepositoryLocator 带这三个字段)
--
-- 版本号说明:起草时是 0045,与 catmem 的 0045_observation_facts 撞号,
-- 顺延为 0047(0046 已被 0046_planning_run_context 占用)。
CREATE TABLE IF NOT EXISTS repomesh_access.participation_observations (
  actor         text NOT NULL,
  repository_id text NOT NULL,
  status        text NOT NULL,
  external_id   bigint,
  display_name  text NOT NULL DEFAULT '',
  observed_at   timestamptz NOT NULL,
  host          text NOT NULL DEFAULT 'github.com',
  owner         text NOT NULL DEFAULT '',
  name          text NOT NULL DEFAULT '',
  PRIMARY KEY (actor, repository_id)
);
