-- 0045: 参与权探测缓存(actor × repository_id)。
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
-- 表结构一次性写成最终形态(对齐主线 0033_participation_cache.sql +
-- 0034_oauth_app_connections.sql 两次迁移,省得以后为同一张表再发一版):
--   observed_at  —— 该次探测的时间,TTL 判定的唯一依据
--   external_id / display_name —— 命中缓存时重建观测用,不再回打 GitHub
--   host/owner/name —— 仓定位(LBP 的 RepositoryLocator 带这三个字段)
--   can_push —— 预留列,对齐主线;LBP 暂无 push 能力概念,恒为 NULL
CREATE TABLE IF NOT EXISTS repomesh_access.participation_observations (
  actor         text NOT NULL,
  repository_id text NOT NULL,
  status        text NOT NULL,
  external_id   bigint,
  display_name  text NOT NULL DEFAULT '',
  observed_at   timestamptz NOT NULL,
  can_push      boolean,
  host          text NOT NULL DEFAULT 'github.com',
  owner         text NOT NULL DEFAULT '',
  name          text NOT NULL DEFAULT '',
  PRIMARY KEY (actor, repository_id)
);
