# main 合并检查

整合基线：本地实现原基线 `28c3167`，远端 main `c0cd44a`，两者相差 70 个主线提交。独立工作树分支 `codex/local-observation-main` 只收录本次观测、评测、前端入口及配套说明。原工作区的 pstack／插件删除、环境脚本、本机认证及启动记录不进入本次提交。

## 兼容修正

- `ConsoleShell` 保留仓库团队页、Spec 页和 main 的设置深链，增加观测工作台直达。
- `SettingsPage` 保留 GitHub 重连和收编后的智能体／技能分类，再增加模型用途入口。
- `routes` 同时保留 agents／skills／local-cli 深链并支持 models。
- 发现历史补丁与 main 的 `ensureState`、Plan 读回及新增发现流程合并；模型／派工逻辑不被旧实现覆盖。
- main 已有 `0043_test_evidence.sql`、`0044_repository_teams.sql`。新增观测迁移改为 `0045_observation_facts.sql`，连续升级到 45；不修改任何主线已采用迁移的字节。
- 新 Bash 启动脚本指定 `eol=lf`，避免当前 CRLF 默认检出环境造成运行失败。

## 合并树验证

| 检查 | 结果 |
| --- | --- |
| `go build ./...`、`go vet ./...` | 通过 |
| `go test -count=1 -json ./...`，指定真实 PostgreSQL | 589 通过，0 失败，2 条件跳过；测试各自创建／清理隔离数据库，未迁移共享业务库 |
| observeui／observepipe／repomesh-observe 的 race 测试 | 通过 |
| 本地安装与进程保护脚本检查 | 4 项通过 |
| `frontend` 锁文件安装、构建、lint | 通过，保留既有 bundle 大小／动态导入提示 |
| `web` 锁文件安装、typecheck、35 项测试、build | 通过 |
| 浏览器导航及设置回归 | 三类模型用途、本地观测直达、旧 Trace 深链、main 的技能／智能体分类与 GitHub 重连按钮均通过 |
| 提交边界 | 本次实际 DeepSeek Key 未出现在提交文件；未纳入其他未提交插件／环境修改；无冲突标记 |

浏览器使用隔离静态预览验证合并后的构建产物；产品身份与只读接口为夹具，目标本地工作台为真实进程，不代替真实 OAuth 或供应商写入验收。未新增真实模型调用。原文档中指向被忽略的 `third_party/AgentTeams` 的五处源码引用仍要求另行准备锁定上游克隆，未为链接检查拉取或修改上游。

证据保存在本机 `$HOME/.local/state/repomesh-observe-local/merge-main/`，包括 Go 测试 JSON、前端检查、浏览器摘要和截图。迁移编号调整仅对本次尚未发布的新增迁移生效；使用首轮 0043 观测实验库时，应保留该实验历史并新建合并版本的隔离验证库，不手改迁移账本。

仓库现有 `push: main` 工作流会自动部署。推送使用普通快进，不改写远端历史；部署结果另按该工作流的实际结果报告。DSH 全链路接入和真实业务验收的既有范围不因合并完成而扩大。
