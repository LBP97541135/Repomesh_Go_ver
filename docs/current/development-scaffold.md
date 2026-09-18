# RepoMesh 基础工程与开发说明

阶段：工程基础与认证实现。施工范围和验收见[分批 TODO plan](../plan/IMPLEMENTATION-PLAN.md)。B02 配置与状态见[认证说明](authentication-development.md)，真实 GitHub 尚未验收；后续业务与上游集成仍未实现。

## 实际目录与依赖方向

```text
go.mod                           唯一产品 Go module
cmd/
  repomesh-web/main.go            Web 配置、信号、启动及 db 子命令
  repomesh-coordinator/main.go    认证后台工作循环与配置
  repomesh-host-executor/main.go  版本与未实现诊断
internal/
  buildinfo/version.go           三入口共用发布版本
  database/                      PostgreSQL 连接、迁移及测试
  access/                        账号、会话、连接和持久发现
  secrets/                       信封加密、根与可用性
  github/                        固定 GitHub 协议适配
  web/server.go                  HTTP 认证入口、诊断及静态文件
  web/server_test.go             真实路由完成度边界检查
web/
  src/main.tsx                   登录、结果与仓库页面路由
  src/style.css                  页面样式
  package.json/package-lock.json 前端依赖和命令
  tsconfig.json/vite.config.ts   类型与构建配置
configs/repomesh.env.example     已实现的 Web 参数示例
scripts/build.ps1               配套发布构建，不执行部署
scripts/verify-batch.ps1        各批工程及独立数据库验证
docs/current/scaffold-*.md      本阶段阅读和复核记录
validation/go.mod              历史实验的嵌套 module 隔离标记
third_party/README.md           独立上游源码位置及重建方式
third_party/agentteams-source.json 上游来源与确定提交
third_party/go.mod              独立上游源码的编译隔离标记
third_party/AgentTeams/         用户后续要求的新克隆，父仓库忽略
```

产品导入路径暂用本地名称 `repomesh.local/repomesh`，不假定远端代码托管地址。产品只有根目录一个 Go module；`validation` 和 `third_party` 的嵌套 module 仅隔离实验与上游源码，不属于产品工程。

后续源码整理授权：用户要求移除旧独立 AgentTeams 目录，重新从官方 main 克隆到 RepoMesh 中。实际位置为 `third_party/AgentTeams/`，来源与备份说明见[上游说明](../../third_party/README.md)。该操作不改变产品架构，不更新历史验证源码，也不表示已实现 AgentTeams Adapter、构建镜像或运行接入。

当前依赖：Web 入口调用 `internal/web` 和 `internal/database`，后者使用 pgx；三个入口都依赖 `internal/buildinfo`，入口之间不互相导入。`web/src` 依赖 React，Vite 将静态资源输出到 `web/dist`，Go Web 在启动时从配置目录读取。前端不导入 Go 内部类型，通过认证 API 核验会话、授权结果和仓库发现。迁移包含版本记录与 B02 认证领域表，没有公共 `pkg` 或通用插件框架。

后续按照 ADR-0010／0013 在真实用例出现时增加领域模块：入口调用用例，领域规则不依赖 HTTP DTO 或上游 DTO；具体基础设施接入集中管理。跨模块原子事务按已采用创建契约和 ADR-0016 设计，不能因目录拆分变成多个提交。目录名称和示例不冻结未定表结构、MCP Schema、进程间消息或恢复算法。

## 三类进程及配套发布

| 进程 | 已采用的最终职责 | 本轮实际行为 |
| --- | --- | --- |
| Web | 页面、访问核验、用户输入持久化、查询和 SSE。 | 提供页面与诊断；配置认证后提供登录、会话和发现 API，db 子命令负责显式迁移。未实现其他业务与 SSE。 |
| 后台协调 | 持久待办、计划/资源核验、采集、恢复；Graph 在此进程内。 | 配置认证后运行身份核实、刷新、发现续扫与秘密维护；无配置以 1 退出，`--version` 以 0 退出。 |
| 受限主机执行 | 已登记环境操作、容器/挂载/网络/限额/停止核查/回收。 | 默认打印未实现并以 1 退出；`--version` 以 0 退出；没有监听或命令执行能力。 |

未来三者可以在同一服务器分别启动，并从同一版本的软件包发布。Web 和 Agent 不持有 Docker socket；受限进程不提供任意宿主命令接口。进程拆分本身不证明权限隔离、可靠恢复或高可用，当前也未实现它们。

`scripts/build.ps1` 一次安装锁文件依赖、检查前端类型、构建页面并生成同版本的三份 Go 二进制。它给三个入口注入同一 `internal/buildinfo.Version`，将前端、前端锁文件、配置和启动说明一起打包到 `dist/repomesh-<Version>/`，最后写入包含目标平台、阶段和各产物 SHA-256 的 `release.json`。已有目录拒绝覆盖；中途失败的目录不代表完成发布，只有脚本成功并存在最终清单才是完整包。无部署、上传、容器启动或数据库初始化步骤。

发布构建使用 `-buildvcs=false`，以显式版本和产物清单标识包，避免将本工作树中历史未提交文档状态误作版本证明。清单不是源码提交证明，也不保证逐字节可复现；当前没有自动选定发布版本、签名或升级协议。必须整体交付，不能把不同构建的 Web、协调、执行二进制与静态资源混用。

## 本地启动与配置

完整命令见[根 README](../../README.md)，均从仓库根目录执行。当前 Web 默认 `127.0.0.1:8080`、资源目录 `web/dist`；覆盖顺序是 `--addr/--assets`、`REPOMESH_WEB_ADDR/REPOMESH_WEB_ASSETS`、默认值。示例文件不自动加载。数据库子命令读取 `REPOMESH_DATABASE_URL`。设置 `REPOMESH_AUTH_CONFIG` 后 Web 和 coordinator 也依赖该数据库；认证配置见专项说明。AgentTeams 和 Python 配置尚未接入。数据库命令见[专项说明](database-development.md)。

| 路径/场景 | 实际结果 | 限定含义 |
| --- | --- | --- |
| `GET /`、`/login`、`/auth/result/{UUID}` | 200，React 页面 | 账号与结果通过 API 核验，不从静态页推断已登录。 |
| 认证 API 未配置 | 503 | 无法完成登录核验。 |
| `GET /healthz` | 200，版本、`status=scaffold`、`businessReady=false` | 仅 Web HTTP 存活。 |
| `GET /readyz` | 503，`status=not_implemented` | 业务不可用，不能作为完整产品启动成功。 |
| 未实现的 `/api` 路径 | 404，骨架 `not_implemented` 提示 | 不是业务错误契约实现，不制造 Issue 成功回执。 |
| 未知页面或资源 | 404 | 不回退为假成功 HTML，不开放目录列表。 |
| 缺少 `index.html` / 监听失败 | 启动错误、非零退出 | 错误消息指向前端构建或监听问题。 |
| Ctrl+C / SIGTERM | Web 最多等待 5 秒关闭 | 只关闭本地 HTTP，未涉及任务恢复。 |

开发模式 `npm --prefix web run dev` 仅运行 Vite 前端，不代理或模拟认证 API；Go 静态服务需先执行前端 build。

本机 Git 可能因仓库所有者不同拒绝 Go 的 VCS 元数据查询。检查时可在**当前 PowerShell 进程**追加信任配置后运行原命令，不必改变全局 Git 设置：

```powershell
$configCount = if ($env:GIT_CONFIG_COUNT) { [int]$env:GIT_CONFIG_COUNT } else { 0 }
Set-Item -Path "Env:GIT_CONFIG_KEY_$configCount" -Value 'safe.directory'
Set-Item -Path "Env:GIT_CONFIG_VALUE_$configCount" -Value ((Get-Location).Path.Replace('\', '/'))
$env:GIT_CONFIG_COUNT = [string]($configCount + 1)
go build ./...
go test ./...
go vet ./...
```

## 未实现与扩展约束

- Graph 按 ADR-0014／0015 是后台进程内模块，随后端发布；将来复用上游仓内有限 DAG，RepoMesh 管跨仓依赖、结果采纳和 Loop。本轮不建 Graph 包、网络服务或 Go DAG 引擎。
- ADR-0020 允许额外的受控 Python 分析子进程。Go 管身份、权限、固定只读材料、持久作业、结果及可选建项来源；主机执行进程管理已登记分析作业的启动、限额、停止核查与回收。分析先于 Issue，也不触发 AgentTeams 准备；失败可手动选仓。版本化 JSON 是已采用方向，具体 Schema 未编制。本轮没有 Python 包、进程、安装依赖、接口、按钮或通用插件平台。
- Skill 全部暂缓，不创建目录、接口、功能开关或占位页面。React Flow 为已选方向，但本轮没有图页面，不添加尚未使用的依赖。
- PostgreSQL 连接、迁移记录表和事务迁移工具已实现。B02 已增加认证和发现表，运行待办与对象存储尚未实现；队列领取、消息顺序、MCP 可信身份和运行恢复按对应专题继续细化。
- Issue、计划、调度与 AgentTeams 尚无业务实现；GitHub 当前限于认证与发现适配；未安装或启动上游真实服务，未重跑旧实验，未清理共享容器或卷。

`validation/go.mod` 使 Go 根目录 `./...` 停在实验边界。它不是能独立编译实验的工程：里面的 Go 文件仍须按原脚本放入锁定上游上下文，当前不要对其运行 `go test` 或 `go mod tidy`。不改动原 README、报告、evidence 和 scripts；“没有产品源码”是旧审计当时事实，新增骨架不补齐第二轮 `remaining-preconditions.md` 或 `resumed-blocked-audit.md` 中的业务缺口。

阅读覆盖见[文件清单](../archive/2026-09-12-development-preparation/docs/current/scaffold-source-inventory.md)、[ADR 审计](../archive/2026-09-12-development-preparation/docs/current/scaffold-adr-review.md)、[AgentTeams 证据追溯](scaffold-agentteams-evidence.md)、[页面/接口/后端审计](../archive/2026-09-12-development-preparation/docs/current/scaffold-page-backend-review.md)。检查和独立复核见[验收记录](scaffold-verification.md)。
