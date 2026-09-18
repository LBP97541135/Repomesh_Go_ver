# RepoMesh 下一开发任务 Prompt：完成 B02.6 并进入 B03

> 历史接手提示。B03、B04 的已授权实现均已完成本地集成验收；本文保留编制时的指令，不作为新的任务授权。当前入口见[计划导航](../../plan/README.md)和[当前交接](../../current/HANDOFF.md)。原始字节见[归档说明](README.md)。

以下内容可直接复制到新的 Codex 任务。

---

[$pstack:poteto-mode](/home/xubohan/.codex/plugins/cache/pstack-claude/pstack/0.9.28/skills/poteto-mode/SKILL.md) 接手 RepoMesh 开发，先完成 B02.6 真实 GitHub 验收，再按已采用契约实施 B03 项目管理批次。

工作目录：

`/home/xubohan/projects/Repomesh_Go_ver`

先确认实际运行在 Linux、工作目录正确并读取 Git 状态。保留全部既有修改；不要重置、暂存、提交或推送，不覆盖 `.codex/config.toml`、历史证据目录或任何已有发布包。Codex 桌面 SSH 连接和项目绑定已经核验，无需重新配置。

## 必须完整阅读

1. `AGENTS.md`、`README.md`
2. `docs/current/README.md`
3. `docs/current/HANDOFF.md`
4. `docs/current/DEVELOPMENT-START.md`
5. `docs/current/IMPLEMENTATION-PLAN.md`
6. `docs/current/b02-authentication-adoption.md`
7. `docs/current/authentication-development.md`
8. `docs/current/b02-github-live-acceptance.md`
9. `docs/development/2026-09-12-b026-live-03/README.md`
10. `docs/development/2026-09-12-b026-refresh-01/README.md`
11. `docs/development/2026-09-12-b026-refresh-02/` 下的 README、baseline、continuity、processes、observer、post-refresh、复核文件和当时已经生成的全部结果
12. `docs/development/2026-09-12-b026-second-account-01/` 下全部文件
13. `docs/current/first-batch-browser-api-contract.md`
14. `docs/current/backend-first-batch-persistence.md`
15. `docs/current/project-configuration-design.md`
16. `docs/current/repository-picker-design.md`
17. `docs/current/first-batch-recovery-design.md`
18. `docs/current/HANDOFF-PAGE-API-DESIGN.md`
19. `docs/current/HANDOFF-BACKEND-DESIGN.md`

以具体章节的采用范围和替代关系为准。设计完成、实现完成、本地验证、真实外部验收和整批 VERIFIED 必须分别表述。

## 当前状态

- B00、B01 已 VERIFIED。
- B02.1 至 B02.5 已 LOCAL_VERIFIED；B02 整体仍 IN_PROGRESS。
- r3 发布包为 `dist/repomesh-0.2.0-b026-cookie-20260912-r3/`。旧 r1、r2、历史失败和旧证据必须保留。
- 专用 HTTPS origin 为 `https://repomesh.bohanxu.me:8443`；auth 配置绝对路径为 `/home/xubohan/.config/repomesh/auth.json`。不要读取或输出秘密正文。
- 专用 PostgreSQL 为 `repomesh_b026`，Unix socket 目录 `/home/xubohan/.local/state/repomesh-b026`，端口 55432。连接秘密只留在服务器受控环境。
- r3 Web 和 coordinator 已运行。接手时必须核对实际 PID 和版本，不按本文旧 PID 猜测，也不要清理这些共享进程或数据库。
- 账号 A 为 `catbobyman`；第二账号 B 为 `bohanxu111`，用户已确认完成邮箱验证。
- 当前自然刷新基线来自 reconnect `cbfce23e-631b-4d97-a66a-97b25e0d60bb`：actor `16198e14-7249-4cc2-a15e-57cd8b568874`、epoch 5、revision `fb6d2db3-5069-483f-9c79-5efb16b58921`、session generation 5。
- refresh_due_at 为 `2026-09-13T00:58:45.991534Z`，即 2026-09-12 17:58:45.991534 PDT；access_expires_at 为 17:59:15.991534 PDT。接手时读取实际结果，不因为时间已过就假定刷新成功。
- Linux observer 原 PID 为 293161，Windows keeper 原 PID 为 52940。PID 可能变化，必须通过进程和证据文件核实。
- 第二账号验收目录已准备并通过独立计划复核，但 LIVE-05 与 LIVE-08 用户读权撤回仍是 NOT_RUN。准备完成不能算真实通过。
- B03 保持 TODO；B02.6 完整通过和独立复核之前不得实施 B03 产品代码。

## 第一阶段：完成 B02.6

先检查 `docs/development/2026-09-12-b026-refresh-02/observations.jsonl`、`outcome.json`、`confirmed-refresh.json`、`browser-result.json`、观察器进程及浏览器 keeper 输出。不要重复启动观察器，不要重新登录或重连来制造新 epoch。

LIVE-09 只有在真实前后只读快照证明同一 actor、原 session、无新增登录／重连、epoch 恰好增加 1、revision 与 credential_committed_at 前进、access/refresh 两项期限前进、提交不早于自然刷新窗口且连接回到 connected/idle 时才能 PASS。不得修改数据库期限、模拟时间、手动重放 token 或用新登录代替刷新。

LIVE-10 必须使用刷新后的同一有效会话完成新的真实私仓发现、桌面与移动视口、实际注销、session 401、敏感页面清理及浏览器 back/forward。只保存裁剪后的白名单事件，不保存 Cookie、code、state、csrfToken、authorizationUrl、token、原始 HAR 或数据库秘密。

刷新与 LIVE-10 通过并独立复核后，按 `docs/development/2026-09-12-b026-second-account-01/PLAN.md` 执行：

1. 临时将专用 GitHub App 设为 public；账号 B 建立一个 A 不可见的专用私仓，并以 selected-repositories 安装该 App，只选择该仓库。
2. 账号 A 邀请 B 成为 A 持有的专用私仓协作者，账号 B 接受。
3. B 完成真实 RepoMesh 登录和 App 授权，确认 A-owned 私仓初始读权与 App 能力 allowed。
4. A 发起 RepoMesh 重连，在 GitHub 账号选择器中选择 B；必须得到 ACCOUNT_MISMATCH，A 的 actor、连接、session 不得被替换，A 的完整分页结果不得包含 B-only 私仓稳定 ID或名称。
5. B 保持同一 RepoMesh 会话；记录 A-owned 私仓 allowed 观察时间，A 移除 B 协作者，等待距 allowed 观察超过 60 秒后重查，确认不再披露旧私仓名称。分别保存 allowed、移除、重查时间和实际秒差。
6. 无论成功、失败或中途阻塞，都恢复 App 为 private，移除 B 的临时 App 安装，保持 B 对 A 私仓的协作者权限已移除，并核对 A 安装范围与 metadata read、contents write、pull requests write 权限。

GitHub 账号密码、二次验证、账号选择、授权同意、安装确认、邀请接受和必要审批由账号持有人亲自完成。需要这些动作时直接打开准确页面并用一句话说明要点击什么；不要索取或显示密码、secret、PEM、包装根、数据库密码或 token。

每次真实验收写入新的证据目录，保留失败记录。完成后由未执行主要操作的模型独立复核全部 B02.6 证据。只有 LIVE-01 至 LIVE-10 的必需范围完整通过且没有开放 P0/P1/P2，才能把 B02.6 和 B02 标为 VERIFIED。

## 第二阶段：实施 B03 项目管理

B02 验证完成后，先在新的 B03 证据目录记录采用范围、设计决定、文件归属和验收矩阵。规划、编排、架构及函数接口声明使用 GPT-6 Astra；实现使用 gpt-5.6 sol；独立复核使用未承担主要实现的其他模型。

B03 只实现：待配置项目创建、项目读取与列表、资料编辑、明确增仓、固定配置引用和原操作恢复。不要实现模型 Key 管理、预算数值编辑、Issue 创建、运行准备、Manager/Worker、P9 或 AgentTeams 接入，也不要因 B02 采用而自动采用后续候选。

浏览器字段与错误码唯一来源为 `first-batch-browser-api-contract.md`：

- `POST /api/projects`，UUID `Idempotency-Key`；首次 201，同键同输入重放 200。
- `GET /api/project-creations/{projectCreationId}`；原回执的 revision 与时间保持不变。
- `PATCH /api/projects/{projectId}`，需要 `expectedProjectRevision` 和 UUID `Idempotency-Key`。
- `GET /api/projects/{projectId}/updates/{updateId}`；先重放已提交操作，再判断新修订冲突。
- 项目 GET、项目列表、项目仓库范围及对应页面状态严格沿现有契约实现；不要复制第二套 HTTP Schema。
- 浏览器恢复页分别为 `/project-creations/{projectCreationId}` 和 `/projects/{projectId}/updates/{updateId}`；恢复记录按 actor、operation kind、key 和可选 projectId 隔离。

持久化必须保证：

- 项目、owner、完整仓库范围、配置引用／固定版本、规范化输入、不可变回执在同一事务提交。
- 创建作用域为 `(actor, project_create, projectCreationId)`；更新作用域为 `(projectId, actor, project_update, updateId)`，与 Issue 操作键严格隔离。
- 创建前完整核实全部所选仓库读权；未知返回 503，不得部分落项目。App、模型或环境暂不可用可以保存待配置项目。
- 更新只把 `repositoryIdsToAdd` 作为明确增量；不能用客户端可见列表替换完整范围。任一新增仓库失败时整次更新回滚。
- 修改资料或修复配置不要求恢复已有受限仓库的读权。
- 更新省略 configuration 时保持原固定配置；只有显式 configuration 提交才重新解析。无实际变化可以成功并保存稳定回执，但不得强制生成新 projectRevision。
- 旧成功回执在后续项目修改后仍返回原 revision 和时间；当前状态另走项目 GET。
- 5xx、断网或丢响应时保持原键并查询原操作；404 只表示当前无可见提交，不能自动换键重建。

至少执行并记录以下真实 PostgreSQL、HTTP 和浏览器验收：

1. 同键同输入 20 并发只提交一个项目／操作，其他为稳定重放或先未知后查到同一结果。
2. 同键不同输入冲突，不覆盖赢家正文。
3. 每个事务阶段故障回滚，不留下半套项目、范围、配置或回执。
4. 提交后丢弃 HTTP 回执、查询先于提交、服务重启后仍可用原键恢复唯一结果。
5. 旧 expectedProjectRevision 的新更新返回冲突；已提交旧 updateId 的重放优先且回执稳定。
6. 无实际变化的更新不增加 revision，但保存可重放回执。
7. 新增仓库任一 denied/unknown 时整个更新回滚，原范围不变。
8. 原范围部分受限时，资料编辑与配置修复仍可成功，不泄露受限仓库名称。
9. 默认配置或 secret 版本变化不暗换项目固定版本；只有显式配置提交重新绑定。
10. 浏览器验证创建、列表、编辑、明确增仓、未知结果恢复、401/失权清理、晚到响应隔离、空/加载/错误状态和原操作链接。

为实际改动运行有针对性的测试；涉及前后端、入口、迁移或发布链路时执行完整 Go build/test/vet、前端 ci/typecheck/build、真实 PostgreSQL 用例、race 和配套发布验证。不要为纯文档准备重跑历史实验。

## 操作限制

- 不运行 `docs/development/2026-09-12-batch-02/verify-record.py`；它会覆盖历史 final-source.json 和 final-checks.json。
- 不覆盖 r1/r2/r3 包，不改写旧失败，不把清单匹配声称为可复现构建或源码到二进制的密码学来源证明。
- 不清理共享服务、容器、数据库或浏览器 profile。
- 不输出秘密正文；只使用非秘密 HTTPS origin、路径、稳定 ID、状态码、错误码、布尔标志和必要时间。
- 已授权的常规实现、修复和验证直接推进。只在账号持有人必须完成 GitHub 交互时停下。
- 每批独立复核、保存实际结果、更新 HANDOFF 和 IMPLEMENTATION-PLAN。明确区分实现完成、LOCAL_VERIFIED、准备完成、真实外部验收和 VERIFIED。
- 不自动提交或推送。

最终汇报列出实际改动、验证命令与结果、证据目录、独立复核结论、未完成限制，以及 B02/B03 的真实状态。

---
