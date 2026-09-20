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
