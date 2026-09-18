# B01 数据库基础实现方案

本批交付 PostgreSQL 连接、显式迁移、版本核查和隔离验证。普通 Web 启动仍提供现有骨架页面，业务 ready 保持 503。业务表随后续用例增加。

## 当前链路与接入位置

main 解析 addr、assets、version，建立信号 context 后调用 internal/web.Run。Run 先验证 index.html，再监听并提供两个探针及静态资源。数据库命令由同一入口分派，数据库包集中负责连接、历史和迁移事务，不修改 HTTP 行为。

[开发指南](../../plan/DEVELOPMENT-START.md)把数据库连接、迁移和可重复验证列为第 0 步。[持久化设计](../../current/backend-first-batch-persistence.md)采用 PostgreSQL 短事务和 READ COMMITTED，但逻辑记录不是已冻结 DDL。本批据此选择具体工程实现。

## 方案比较与选择

| 标准 | A，pgx 加内嵌迁移 | B，pgx 加外部 Atlas |
| --- | --- | --- |
| 当前范围 | 同一二进制，普通骨架启动保持兼容 | 新增工具和运行设置，提议普通启动依赖数据库 |
| 迁移事实 | 私有有序清单，版本、名称、SHA-256 精确比较 | Atlas 管理目录校验及版本事实，另核包装命令行为 |
| 事务 | 取得事务级 advisory lock 后读历史，SQL 和记录同事务 | 外部工具负责事务，包装层另管进程和错误 |
| 发布 | go:embed 随二进制固定 | 额外固定、打包及校验 Atlas 和迁移文件 |
| 代价 | 维护仅支持 PostgreSQL、事务型 SQL、只前进的迁移器 | 更多工具能力，同时增加安装和维护范围 |

候选由 gpt-5.6-sol 和 gpt-5.6-terra 分别提供。独立交叉评审 gpt-5.6-luna 推荐 A。主代理核对入口和设计后采用 A，吸收 B 的确定命名、明确核查输出和操作说明。评审中的 checksum32 明确为 SHA-256 的 32 字节，不是 32 位。

## 调用和结构

现有 Web 二进制增加 db check 和 db migrate。URL 来自 REPOMESH_DATABASE_URL，可用数据库子命令参数覆盖。数据库命令有总超时，不依赖前端资源。普通启动及 --version 保持原行为，不自动迁移。

internal/database 内部持有 pgxpool，Open 必须实际 Ping。Check 检查一致的历史快照。Migrate 取得事务锁后读取历史，校验并应用全部缺少的版本。私有清单保存连续正整数版本、文件名、SQL 和计算所得的 SHA-256。首条迁移只建立 public.repomesh_schema_migrations。

历史必须是编译清单的精确前缀。缺号、未知较新版本、名称和内容变化均拒绝。DDL 和历史同事务，失败或取消时使用独立且有上限的 context 回滚。URL 和驱动中可能携带的连接信息不写日志。无任意 SQL 文件路径、降级或非事务迁移开关。

驱动固定 github.com/jackc/pgx/v5 v5.11.0，已用 Go module 代理核实。实际连接核验遵循[pgxpool 文档](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool)，创建池本身不证明已建立连接。验证使用 PostgreSQL 17.11，不声明其他大版本已验证。

## 验证与隔离

B00 六项检查、探针和三入口行为通过。最初一次前端输出采集遇到 GBK 解码错误，改为 UTF-8 后重跑构建并保存完整输出。

Docker 引擎未启动。使用[官方 Windows 页面](https://www.postgresql.org/download/windows/)指向的 EDB 压缩包，解压到被忽略的 bin。下载来源及 SHA-256 见 [postgres-source.json](postgres-source.json)。脚本建立临时数据目录，只监听回环地址，使用随机凭据，结束时停止自己启动的实例。集成用例各建独立数据库，只清理自己生成的名字。

必须覆盖缺少 schema、首次与重复迁移、20 个并发迁移、连接重建、失败 DDL 回滚、锁等待取消、SQL 中取消、版本和校验和不匹配。真实二进制验证子命令、错误退出、现有 HTTP 探针及静态资源。

## 分工和原则

主代理维护计划、验证脚本、实例和集成。实现代理独占临时 worktree 的 Go、SQL 及测试。实现代理结束后才复制列明文件到主工作区。非实现者独立检查最终产物。

Foundational Thinking 将共同依赖放在首批。Model the Domain 用不可变清单表示迁移历史。Sequence Work into Verifiable Units 要求 B00 通过后才进入 B01。Separate Before Serializing Shared State 用 worktree 隔离写入。Prove It Works 要求真实 PostgreSQL 和二进制证据。

认证、秘密、模型、Issue 和 P9 的候选状态保持。共享工作区的既有改动不整体暂存或提交，本批不创建或合并 PR。
