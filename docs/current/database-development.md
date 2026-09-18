# 使用与验证数据库基础

本页说明 PostgreSQL 连接和迁移工具。B01 建立迁移记录表，B02 新增秘密、认证和发现表；其他业务表在所属批次增加。实现选择见[方案记录](../development/2026-09-12-batch-01/design.md)，进度见[施工表](../plan/IMPLEMENTATION-PLAN.md)。

## 核查和迁移开发数据库

连接池将 PostgreSQL `timestamptz` 扫描结果统一为 UTC，使认证、会话和仓库观察的 JSON 时间满足浏览器契约。该设置不改变数据库时间点、主机时区或凭据期限；非 UTC 环境回归与修复证据见[UTC 修复记录](../development/2026-09-12-b026-utc-fix-01/README.md)。

1. 准备自己拥有的 PostgreSQL 开发数据库。迁移连接需要该库 public schema 的建表、CREATE SCHEMA 及迁移对象访问权限。
2. 在当前进程设置 `REPOMESH_DATABASE_URL`。连接串由操作者提供，程序不自动读取 .env。
3. 运行 `go run ./cmd/repomesh-web db check`。以当前 6 条迁移为例，空库输出 `schema status=missing current=0 target=6 pending=6`，二进制退出 1。
4. 运行 `go run ./cmd/repomesh-web db migrate --timeout 30s`。成功输出 `schema status=current current=6 target=6 pending=0`。
5. 再次运行 `db check`。核查必须完全匹配才能退出 0。新增迁移后，以上数字以所用二进制报告的 `target` 为准。

`db check --database-url <连接串>` 或 `db migrate --database-url <连接串>` 覆盖环境变量。优先使用环境变量，避免将凭据写入命令历史。显式空参数会覆盖环境变量并报配置错误。`db --help` 和各子命令 `--help` 不连接数据库，也不显示连接串。

`--timeout` 是连接与本次操作共用的总预算，默认 30 秒，必须为正。错误退出前清理事务另有最多 5 秒预算。无效参数或连接串退出 2，连接、迁移、历史核查失败退出 1。`go run` 的父进程另会显示子进程退出状态，自动化应直接调用构建后的二进制。

未配置认证时 Web 不连接数据库；提供认证部署配置后，Web 和 coordinator 连接数据库并要求迁移匹配，不执行隐式迁移。数据库命令不要求前端资源，也不启动 HTTP。`/readyz` 保持 503。

## 处理迁移失败

先保留程序输出，再运行 `db check`。命令输出不会包含连接串、驱动原始错误正文或历史记录中的任意名称。

- 缺少迁移时，使用与本次源码配套的二进制执行 `db migrate`。
- 名称、校验和、版本缺号或未知较新版本不匹配时，恢复正确的软件与迁移来源。不要修改已应用 SQL 或直接改迁移记录来消除错误。
- 连接失败或超时后，检查数据库实际可达性和账号配置，再用原二进制核查。
- 提交结果未确认时，先核查原库。不能把本地收到错误当成已经回滚的证明。

迁移仅支持 PostgreSQL 的事务型 SQL，全部待应用版本在一个短事务内提交。事务级 advisory lock 的固定键为 `0x5245504f4d455348`。版本记录与 SQL 同事务，等待锁结束后才读取已提交历史。只提供前进迁移，不提供 down、清库或任意 SQL 路径。

## 增加所属业务批次的迁移

在 `internal/database/migrations` 新增连续编号文件，编号接续已有最大版本；当前已到 `0006_complete_save_vault.sql`，下一条从 `0007` 开始，名称按实际业务填写。文件名由四位编号、下划线、小写名称及 .sql 组成。使用显式 schema 名称，事务外 DDL 不在当前工具范围内。先完成所属业务契约和对应的真实数据库约束测试。

已应用文件保持不变。SHA-256 按实际 SQL 文件字节计算，`.gitattributes` 固定这些文件使用 LF，保证 Windows 和 Linux 检出后的校验依据一致。迁移随二进制内嵌，不另发布一份可变 SQL 目录。本工具校验迁移历史，不把手工修改业务表后的数据库结构视为已自动审计。

## 运行本批验证

当前完整工程含 B02 认证测试，批次验证使用 Linux、PowerShell 7、Go、Node.js、npm 和 PostgreSQL 17。下方 Windows 命令仅保留为 B01 当时源码的历史运行方式。本批实际验证版本为 PostgreSQL 17.11。二进制可使用已有安装目录，或从[PostgreSQL 官方 Windows 页面](https://www.postgresql.org/download/windows/)指向的 EDB 压缩包取得。

```powershell
pwsh -NoProfile -File scripts/verify-batch.ps1 -Batch B01 -PostgresBin ./bin/dev-postgres17/pgsql/bin
```

脚本生成带随机密码、回环监听和独立数据目录的新实例。每个 Go 集成用例再创建独立数据库。脚本检查数据库用例实际运行且没有跳过，检查真实二进制输出，重启 PostgreSQL 并核查迁移持久性，然后停止并清理自己的实例。任何清理失败均使该次验证不通过，并保留目录供检查。

脚本把命令、版本、退出码、诊断和 HTTP 结果保存到新的证据目录。`-EvidenceDirectory` 可以指定新目录，已有目录拒绝覆盖。失败运行的记录保留，不改写为成功。Go 测试输出中的无测试包不等于跳过数据库用例。

已有专用测试实例也可通过 `REPOMESH_TEST_DATABASE_URL` 运行 `go test -count=1 ./internal/database`。该账号必须能创建与删除测试数据库。不要指向真实业务环境。普通单元测试在该变量未设置时明确跳过 PostgreSQL 用例，不能据此宣布本批完成。

B01 历史记录不覆盖业务。B02 的认证持久化与 HTTP 行为另见[本批结果](../development/2026-09-12-batch-02/README.md)；真实 OAuth、模型调用和上游运行接入尚未验收。明确的 COMMIT 约束失败已测试，网络在 COMMIT 回执处断开造成的不确定性未做确定性故障注入。
