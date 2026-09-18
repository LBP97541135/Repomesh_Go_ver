# B00 与 B01 施工验收

更新于 2026-09-12。用户要求建立分批 TODO plan 并开始开发，每组验证。本轮完成 B00 工程基线与 B01 PostgreSQL 基础。业务功能仍按[施工表](../../plan/IMPLEMENTATION-PLAN.md)接续。

## 本轮交付

- Web 二进制提供 db check 和 db migrate。普通启动兼容现有骨架。
- PostgreSQL 连接实际 Ping，命令有总超时，错误不输出连接串。
- 内嵌有序迁移清单，记录版本、名称和 SHA-256。首条迁移只建 public.repomesh_schema_migrations。
- READ COMMITTED 迁移事务先取数据库事务锁，再读历史；SQL 与历史一起提交。失败或取消后以独立限时 context 回滚。
- 明确拒绝历史缺号、较新版本、名称和校验和变化。迁移 SQL 固定 LF。
- 验证脚本提供 B00 和 B01。B01 自建隔离 PostgreSQL，运行真实数据库和二进制场景，再停止并清理自己启动的实例。

实现依据与两种方案的比较见[design.md](design.md)。开发命令与操作限制见[数据库说明](../../current/database-development.md)。

## 分组结果

| 组 | 结果 | 证据 |
| --- | --- | --- |
| B00 工具与工程基线 | VERIFIED，Go 1.26.4，Node 22.22.1，npm 11.13.0，六项工程检查通过 | [原始检查](b00-checks.json)、[实际入口探针](b00-runtime.json) |
| B00 可重复验证脚本 | VERIFIED，工程命令、三个二进制和 HTTP 探针通过 | [脚本结果](b00-script-verification-r2/checks.json) |
| B01 脚本拒绝漏跑数据库测试 | VERIFIED，旧代码缺少 PostgreSQL 用例时，脚本按预期输出 NOT_VERIFIED；清理成功 | [反向检查](b01-negative-test-gate-r3/checks.json) |
| B01 真实 PostgreSQL | VERIFIED，17.11，12 组数据库测试及 19 个含子用例的通过事件，无数据库用例跳过 | [完整验证](b01-verification/checks.json)、[数据库日志](b01-verification/postgres-output.txt) |
| B01 二进制与持久性 | VERIFIED，首次、重复迁移、PostgreSQL 实际重启后核查、历史篡改拒绝、错误退出、HTTP 响应和构建 HTML 一致 | [完整验证](b01-verification/checks.json) |
| B01 独立代码复核 | PASS，由 gpt-5.6-sol 检查实际文件，9 个集成文件与实现工作区字节一致 | [独立复核记录](review.md) |
| B01 配套打包 | VERIFIED，三个版本一致、10 个产物哈希全部匹配、包内 db 帮助可用 | [打包检查](release-verification.json) |

12 组数据库用例覆盖空库只读核查与重复保存、20 个并发迁移、SQL 失败、历史记录插入失败、多文件原子升级、各类历史不匹配、SQL 取消、锁等待取消、锁等待超时、错误密码、数据库隔离，以及 COMMIT 时约束失败。Go JSON 中无测试文件的包级 skip 不属于数据库用例跳过。

## 实际修正与验证过程

B00 最初的输出采集遇到 GBK 解码错误。改为 UTF-8 后重跑前端构建并保存完整输出。验证脚本首次命令发现取得多个 node.exe 路径，选择 PATH 中第一个后，B00 全部通过。

PostgreSQL 首次启动把 Windows 参数中的单引号当作套接字目录名。改为在新实例配置中设置空套接字目录。随后发现后台进程继承了 pg_ctl 输出管道，父命令结束后输出管道仍未关闭。启动步骤改为不使用输出捕获管道，数据库日志独立保存。已有进程已停止。

独立脚本复核要求负向场景同时核对失败原因，避免把连接错误算成历史篡改被拒绝。脚本已增加诊断断言，首页也比对实际构建的 index.html 及其 SHA-256。修正后的 B01 全流程通过。

失败尝试保留在 b00-script-verification、b01-negative-test-gate 和 b01-negative-test-gate-r2，未改写为成功。本轮结论以表中链接的已完成结果为准。

## 边界与剩余工作

本批只有数据库运维基础，没有业务表、OAuth、项目 API、模型调用、Issue 或 AgentTeams 运行接入。页面仍是骨架，readyz 仍返回 503。原有候选采用状态保持，C05、C06 和 P9 在对应业务批次处理。

PostgreSQL 17.11 的 Windows 二进制来自官方页面指向的 EDB 下载，来源与本地 SHA-256 见[下载记录](postgres-source.json)。该哈希记录下载所得字节，不是独立签名校验。其他 PostgreSQL 大版本及系统未做本轮运行验证。便携二进制保留在被 Git 忽略的 bin 下，不随产品发布。

明确的 COMMIT 约束失败已覆盖。网络在 COMMIT 回执处断开导致的不确定性未做确定性故障注入。CLI 的 pending 分支已有实现，多迁移待应用状态通过数据库包测试，因为当前产品清单只有一条迁移。

没有重跑历史上游实验、连接真实 GitHub 或模型、提交现有工作区、推送或创建 PR。打包只生成本地产物，不部署。后续从 B02 认证与仓库发现开始，逐组回填证据。

决策轨迹见[decisions.tsv](decisions.tsv)，工作区开始状态见[baseline.json](baseline.json)。本轮严格验证的是连接与迁移行为，不据此宣布业务持久化或完整产品完成。
