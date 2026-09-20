# RepoMesh —— 多仓协同交付控制台

把一句需求变成**跨仓库的真实交付**：agent 团队自己规划、自己拆任务、自己改代码、
自己跑测试、自己开 PR；人在关键的门上说了算，全程留痕、可回放、可审计。

> 部署与排障看 [`docs/操作手册.md`](docs/操作手册.md)。本文件只讲"是什么、怎么跑起来"。

## 一、它解决什么

一个需求往往要改好几个仓库（API 改了、客户端要跟着改、数据库要迁移）。
人工做这件事的成本在**协调**上：谁该改、谁先改、契约怎么对齐、改完谁验。
RepoMesh 把这套协调搬进一个控制台，并且**每一步都留下可查的证据**。

## 二、三个进程，各管一段

```
浏览器 ──► repomesh-web ──► PostgreSQL（唯一事实源）
                ▲                    ▲
                │                    │
        repomesh-coordinator ────────┘   决定"下一步派谁"
                │
        repomesh-host-executor ─────────► 起 agent 进程 / 铸 token / 交付记账
```

| 进程 | 职责 | 不做什么 |
|---|---|---|
| `repomesh-web` | 所有 HTTP 读面与写面 | 不跑 agent、不碰主机 |
| `repomesh-coordinator` | DAG 调度、规划派发、自动托管发现链、收产物 | 不直接起进程 |
| `repomesh-host-executor` | 唯一的主机执行面：起 agent、隔离工作区、交付记账 | 不决定业务状态 |

**设计约束**：三者都不在内存里存业务状态 —— 重启后按库里的事实继续跑。

## 三、一次交付长什么样

```
需求 → ① 需求分析 → ② 候选评分 → ③ 分档审批 → ④ 生成计划 → ⑤ 物化任务
                                                              ↓
                        各仓 DAG 任务 → 开发 agent → 测试 agent → 交付（commit/push/PR）
                                                              ↓
                                           交付清单（版本集合 / 迁移 / 数据基线 / 证据）
```

- **人审门**（③ 分档审批、⑤ 物化确认）在 `人工参与` 模式下**必须等人**；
  `自动托管` 模式下由协调器代行，日志里看得见每一步。
- **agent 只能"提"规格变更**：产物落审核台，人批之后才升版并触发重排 —— 不允许 agent 自己改生效。

## 三·五、控制台里看什么（按你关心的事找入口）

| 你想知道 | 去哪看 |
|---|---|
| 这条需求现在跑到哪一步 | 工作台左栏：①-⑤ 五步态；**计划 DAG 常驻在「Manager · 主脑」上面**，节点带执行态着色 |
| worker 到底干了什么 | 点任务行 → 右栏「worker 工作内容」：agent 的 stdout/stderr 尾部、exit code、PR 链接（读不到就说"没有这份记录"，不假装它没干活） |
| 测试组在测什么、排到哪了 | 点「测试组」→「测试排期」：计划要跑的每一轮（单点验收/仓库集成/跨仓回归）与"已通过/待跑"（状态只按真实记录判，没记录就是待跑） |
| 生成计划/物化为什么没动 | 人工参与模式下这两步是**人工门**，右栏与聊天流里都有按钮；等的是你，不是系统 |
| Manager/Leader/执行 的交接 | 右栏「跨仓职责 · 授权 · 冲突」：案例时间线（谁、什么角色、何时、做了什么）+ 六个动作入口 |
| 数据库变更验过没有 | 右栏「数据库分支验证」：从**业务数据基线库**开分支、逐条迁移留结果、跑完回收；证据里写清是哪个 provider |
| 这次交付包含哪些版本 | 交付阶段 →「交付版本清单」：各仓 PR/提交、迁移原文、数据基线、验证结论、测试证据 |
| 线上跑的是哪个版本 | 设置 → 平台 →「部署」：控制台版本（构建期）与服务端版本（运行时）分开显示 |
| 某个能力为什么是空的 | 界面按三态说话：**没配** / **没跑** / **取不到** 分开说，不合成一句"无数据" |

## 四、快速开始（本地）

```bash
# 1. 依赖：Go 1.26+、PostgreSQL 16、Node 20+（前端）
export REPOMESH_DATABASE_URL='postgres://user:pass@127.0.0.1:5432/repomesh?sslmode=disable'

# 2. 建表
go run ./cmd/repomesh-web db migrate

# 3. 起三个进程（三个终端）
go run ./cmd/repomesh-web
go run ./cmd/repomesh-coordinator
go run ./cmd/repomesh-host-executor

# 4. 前端
cd frontend && npm install && npm run dev     # 开发；生产用 npx vite build 后由 web 托管
```

**必配环境变量**（不配则相关能力如实报"未配置"，不假装能用）：
`REPOMESH_DATABASE_URL`、`AGENTTEAMS_CONTROLLER_URL/_TOKEN`（智能体 runtime）、
GitHub App 凭据（仓库读写）。

### 本地观测与评估

独立管理工具提供 Trace、计量、Jev rubric 评分、DeepSeek 聚类／归因、数据集及人工复核：

```bash
go run ./cmd/repomesh-observe serve --archive /absolute/private/observation/archive --addr 127.0.0.1:19091
```

工作台默认进入「任务地图」：选择一次 Trial 或 OTLP Trace 后，先看任务故事线、真实性能和证据链，再逐层打开完整 Span、事件、Links、验收检查、模型评分与原始 JSON。原有九个完整数据页仍保留在左侧导航。

在主控制台「设置 → 模型与 API」直接输入 Jev／DeepSeek 的模型与 API Key，保存后可测试连接、读取可用模型；也可使用独立工作台的设置页。凭据写入工作台实际使用的私有配置，页面不回读明文。
Web 默认连接 `http://127.0.0.1:18090`；若工作台按上例运行在 19091，在启动 Web 前设置 `REPOMESH_OBSERVE_WORKBENCH_URL=http://127.0.0.1:19091`。维护本机共享观测配置需要管理员会话，平台其他依赖未就绪时模型设置仍可访问。
观测、评估调度和数据集留在本机，无需云空间。
自动评分默认关闭。发现链计量可通过 `REPOMESH_OBSERVE_ARCHIVE` 与 `REPOMESH_OBSERVE_SOURCE_ID` 在下次启动时启用。
完整配置、接口及未接入边界见[本地观测工作台](docs/current/local-observation-workbench.md)。

## 可选：Jev 辅助测试与代码审查

选择项目后，在「设置 → 模型与 API → 测试与代码审查 · TypeSafe / Jev」保存 Key，并打开统一开关「启用 Jev 辅助测试与代码审查」。Key 由 Web 的既有密钥库加密保存；需要已配置认证运行时和包装根。保存不会发出模型请求；「检查连接」会运行一次合成样本推理。

部署本功能需配套更新三个二进制并应用迁移 `0058_typesafe_verification.sql`（`go run ./cmd/repomesh-web db migrate`）。在 host-executor 环境中设置 `REPOMESH_TYPESAFE_BROKER_URL` 为 Web 的 origin，例如 `https://repomesh.example`；同机开发可使用实际监听的 loopback HTTP 地址。该地址不含 `/api` 路径，HTTPS 证书须受执行器信任。不需要把 Jev Key 设置到执行器或 coding agent 环境。

开启后，测试 run 获得证据核对工具；新派发任务另有一次仓库负责人的辅助代码审查，检查独立副本中的候选提交。关闭后停止新 Jev 调用，保留历史记录。工作台测试区、任务验收区及审核阶段分别展示辅助判断；Jev 不自动批准任务或合并。Skill 固定随产品发布；仓库内的 Codex 可直接使用 `.agents/skills/typesafe-ai`。

行为与接口见 [Spec](docs/current/typesafe-verification-spec.md)，本地验证与真实 Codex/Jev 证据见[验证记录](docs/development/2026-09-20-typesafe-verification/README.md)。既有跨仓集成仍有未绑定候选提交组合的限制，辅助判断不表示整条交付链已验收。

## 五、目录导航

| 路径 | 内容 |
|---|---|
| `cmd/repomesh-web` | HTTP 面 + 进程装配（所有接线都在这） |
| `cmd/repomesh-coordinator` | 调度与自动托管循环 |
| `cmd/repomesh-host-executor` | 主机执行面（agent 启动、交付脚本、证据读回） |
| `internal/web` | 路由层（每个域一个文件，端点集中在域文件里） |
| `internal/{projects,issues,discovery,execution,tasks,...}` | 各域服务与存储 |
| `internal/database/migrations` | SQL 迁移（**编号全局唯一**） |
| `frontend/src` | React 控制台（`api/` 一域一文件） |
| `docs/操作手册.md` | 部署、运维、事故处置、验收清单 |
| `docs/current/` | as-built 设计文档（接口总册等） |

## 六、开发约定（踩过坑总结）

1. **改完必须**：`go build ./...`、带 `REPOMESH_TEST_DATABASE_URL` 跑相关 `go test`、
   前端 `npx tsc --noEmit` + `npx vite build`。
2. **迁移编号**取 `origin/main` 最大号 +1，绝不重号（重号会让部署静默失败）。
3. **行尾**与 HEAD 一致：提交前用 `git show HEAD:<file>` 比对，别整文件归一。
4. **不编数据**：读不到就留空 / `unconfigured` / `false`，失败原因如实上屏。
5. **agent 工作区**：每个任务一棵**自己的** worktree（`<attempt>/repo`），
   共享基础克隆只用于 `fetch`。
6. **交付闸门**：提交前清构建产物；`git add -A` 后**真没改动就明确失败**，不许开空 PR。
