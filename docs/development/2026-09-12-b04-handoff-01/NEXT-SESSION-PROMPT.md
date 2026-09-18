# RepoMesh 下一任务：审查 B04 精确采用包，再实施配置与模型秘密管理

准备状态：`DESIGN_PREPARED_NOT_ADOPTED`。这份 Prompt 授权下一会话先审查并收口 B04 的精确范围与接口，再按采用结果实现；它不把现有候选整体自动采用。

使用 `$pstack:poteto-mode`。工作目录为 `/home/xubohan/projects/Repomesh_Go_ver`。先确认 Linux、实际 cwd、Git 分支/HEAD/完整含未跟踪文件的状态，保存新任务非秘密基线。桌面 SSH 与项目绑定已核验，无需重新配置。

## 真实状态与保护

B00/B01 已 VERIFIED。B02 仍 IN_PROGRESS，外部账号验收 PAUSED_BY_USER；历史 LIVE-05 FAIL、LIVE-08 NOT_RUN、RESTORE_IN_PROGRESS 保留，未来跨账号项 DEFERRED_BY_USER。不执行账号操作，不恢复第二账号实验，不索取第二账号；未来若另获真实账号验收授权只使用主账号 A（catbobyman）。本批可用本地合成 actor 做隔离回归，不能称真实 GitHub 跨账号验收。

B03 已集成主目录，末轮本地后端、前端、真实 PostgreSQL、浏览器和配套发布检查 PASS；本 Prompt 编制时最终独立复核仍在进行。先读主目录 `docs/development/2026-09-12-b03-integration-01/` 中实际最新最终复核与汇总：只有明确 PASS、无开放 P0/P1/P2 且覆盖最终源码/产物，才按 `INTEGRATED_LOCAL_VERIFIED` 接手。文件缺失、未完成或结论不符时保留验证待定，先处理该具体缺口；不得预先称 B03 或 B02 VERIFIED。`businessReady=false`，模型请求和运行未接入。

保留全部既有修改，不 reset、stash、stage、commit 或 push。不覆盖 `.codex/config.toml`、历史证据、旧失败及任何已有发布包，不运行 `docs/development/2026-09-12-batch-02/verify-record.py`。保留 `/home/xubohan/projects/Repomesh_B03` worktree、其证据与旧包；主目录源码是下一实现基线，不再次整体合并旧 worktree。

不读取或输出认证配置/包装根/数据库密码/token/Key 正文，不扫描进程环境。不要清理共享服务、容器、数据库或浏览器 profile。B02 专用数据库 repomesh_b026、socket `/home/xubohan/.local/state/repomesh-b026`、55432 与 r3 Web/coordinator 不用于 B04 迁移或测试。不按旧 PID 操作；必要时只核实际非秘密进程路径/版本。测试使用新建且本任务拥有的隔离 PostgreSQL、端口、浏览器上下文，清理仅限这些资源。旧 r1/r2/r3 和 B03 两处发布包不覆盖；新发布使用唯一新版本标签。摘要/哈希匹配不冒充可复现构建或源码到二进制的密码学来源证明。

## 必须完整阅读

1. 根 `AGENTS.md`、`README.md`，`docs/current/README.md`、`HANDOFF.md`、`IMPLEMENTATION-PLAN.md`、`DEVELOPMENT-START.md`。
2. `docs/development/2026-09-12-b04-handoff-01/HANDOFF.md`；B03 integration-01 的 README、checks、最新最终独立复核/验证汇总与其引用的实际末轮结果。较早失败保留，只据最终结果及实际源码判定。
3. `docs/current/first-batch-complete-review.md`、`model-key-save-design.md`、`model-settings-browser-api-draft.md`、`model-connection-settings-design.md`、`model-project-apply-design.md`、`backend-model-operations-draft.md`、`backend-first-batch-sources-draft.md`、`first-batch-recovery-design.md`。
4. `docs/current/first-batch-browser-api-contract.md`、`backend-first-batch-persistence.md`、`project-configuration-design.md`、`repository-picker-design.md`、`b02-authentication-adoption.md`、`authentication-development.md`、`HANDOFF-PAGE-API-DESIGN.md`、`HANDOFF-BACKEND-DESIGN.md`，以及原型导航指向的模型页面采用基线。
5. worktree `/home/xubohan/projects/Repomesh_B03/docs/development/2026-09-12-b04-design-01/` 的 `backend-candidate-a.md`、`backend-interfaces-a.go.txt`、`http-ui-candidate-b.md`、`frontend-interfaces-b.ts.txt`，及已存在的 `backend-interfaces-final.go.txt`、`frontend-interfaces-final.ts.txt` 和任何后续 synthesis。文件名 final 不等于采用；已读版本仍 PROPOSED_NOT_ADOPTED。
6. 真实源码 `internal/secrets`、`internal/access`、`internal/projects`、`internal/web`、`internal/database`、`internal/jsoninput`、测试数据库设施，Web 入口与发布脚本；前端 api/values/routes/session/projectApi/projectRecovery/projectRequests、项目创建与设置/配置选择器及对应测试。

按具体章节采用范围和替代关系裁决。不要因为 B02 采用 S01/S02 就采用全部模型/预算/运行来源，不把原型内存成功算实现或验收。

## 先采用再实现：模型与文件归属

规划、编排、架构、函数接口声明必须 GPT-6 Astra。实现必须 `gpt-5.6-sol`（工具显示名 gpt-5.6 sol）。独立复核使用未承担主要实现的其他模型。先核工具实际模型，不以文本角色名冒充模型路由。每个实现单元先由 Astra 声明 Go/TS 公共及必要私有函数、DTO、锁序、模块归属；Sol 填函数体，新增 helper 先提交具体签名申请，由 Astra 批准后实现，不事后声称全部遵守前置流程。

新建唯一 B04 实施证据目录，记录使用到的授权、精确采用章节/明确替代、设计决定、文件唯一编辑归属、接口声明、验收矩阵及独立设计复核。Astra 需实读并解决两个候选与中断 final 草稿的差异：
- A 的 MAC + 比较 Key + InputVault，与 final 草稿的受控加密完整输入单 vault；精确比较、禁用原业务 Key 后的独立目的权限、结果清理/墓碑保留。
- import 单例锁与 owner 锁次序、旧结果重放先于新输入资格/默认解析、全局死锁关系。
- S03/S04 精确裁减 schema、单 owner 与原 allowedOwnerIds 的替代、执行模板仅登记、不可用条件、default pinned_version 与现有 resolver 兼容。
- DTO 的 displayName 非空输出、fieldErrors 固定数组、稳定 requestId，以及服务端/浏览器 provider_save Destination。
- 秘密 Prepare/InsertPrepared 的事务边界与根包装独立预扣；不得持业务锁后再次独立 reserveWrap 导致自阻塞。

推荐六个模型 HTTP 端点沿原模型字段稿前六项：Provider 列表、当前详情、指定不可变版本、保存、原 save 查询、原 save close。采用后在唯一字段稿记录 B04 精确范围，不另造第二 HTTP Schema。部署导入只为受控本地 CLI，不增加浏览器配置导入后台；命令和 schema 须先由 Astra 定稿。若发现需要扩大用户范围才可解决的设计分歧，说明具体分歧，继续独立准备；不得默认引入 B05+。

## B04 实现边界及不可退化条件

仅模型与执行配置来源、不可变版本、模型秘密保存与原操作安全终结。排除模型真实请求/测试/费用/预算数值编辑/预算账本扩展、专用模型应用、Issue、P9、运行准备、Manager/Worker、AgentTeams、账号操作。

Provider owner 私有，完整 1..50 模型快照与稳定行/modelProfile 身份映射。Key 默认 keep，显式 replace 生成新版本，即使正文相同；Key 1..8192 UTF-8 字节原样，不 trim、不回显、不保存裸猜测哈希。地址格式合法不代表允许出站，保存不能探测网络。项目引用固定旧模型/秘密版本，首个模型不自动变默认。原 B03 PATCH 显式 configuration 会重解析两项，不能包装成仅换模型且保持 execution 原固定版本。

保存作用域 `(actor, provider_save, saveId)`，UUID 原键；新建 201、更新及重放 200。配置、密文/可用性、全量模型映射、目录版本、head 和原不可变回执同事务，原成功回执不因后续修改而改变。新旧输入冲突不得覆盖赢家。根包装预扣允许保守增长，业务回滚不得留半套记录。

close 路径和 Idempotency-Key 都为原 saveId，正文严格 {}；与 save 同唯一槽互斥。已保存回原 committed，已拒绝回原 rejected，空槽持久 closed_without_save 阻止合法迟到 save，返回固定 closedAt；不撤销成功保存。closed/removed 优先新修订及秘密比较；当前认证/授权与信封边界仍成立。close 不需要原 Key/Provider 输入或解密成功。GET404 仍未知，close 503/丢响应继续同键查询或同 close。只有持久 rejected/closed_without_save 才能回非秘密草稿、重录必要 Key、显式新键；410 不证明未保存、不复活原键。

浏览器沿供应商分栏/移动顶部选择与模型小弹窗。保存摘要不显示 Key；发出前展示复制原操作链接，即使存储禁用。Key 发送后立即清内存表单，不等响应，不可进入 sessionStorage/localStorage/日志/URL/原始 HAR。非秘密草稿 sessionStorage 与最小 local 索引按 actor/kind/key 隔离。未知页只查、明确 close、稍后处理；关闭弹窗不 close。401/logout/换 actor 清全部敏感派生状态和输入，保留最小索引不授权。Abort 之外必须 actor/session/route/query/request generation 比较，真正晚到 201/200/404 不得覆写新状态。原 actor 未知时不能把当前另一 actor 空槽 close 当成原操作已终结。

执行来源导入版本和显式默认绑定与回执原子提交，同 importId 原输入稳定重放，异内容及 id/version 异内容整笔拒绝。默认变化只影响以后显式解析；旧项目不变。不制造假模板、预算、运行 Ready 或假默认填空；登记元数据与实际可执行性分开。保持 businessReady=false。

## 必须实际运行并留证的验收矩阵

| 组 | 必需事实 |
| --- | --- |
| 并发/冲突 | 同键同输入 20 并发只一套 Provider/模型/secret/profile/receipt；异 Key/参数 409，不改赢家；旧成功键优先旧修订重放 |
| 事务 | 每持久阶段故障全回滚；根包装预扣不退款；所有原结果时间/版本稳定 |
| save/close | save 赢、close 赢、save 回滚、双 close/20 并发 close、持锁超时、closed 后迟到合法 save；根不可用仍可关闭空槽 |
| 恢复 | 提交后丢 HTTP、GET 先于提交、请求未到、实际 Web 双进程/服务重启后原键查询；close 丢回执；404/410/403/503 不混终态 |
| 秘密 | 错 root/AAD/owner/purpose 拒绝；根退役与准备提交竞争；业务 Key 禁用不绕资格；原输入比较和清理墓碑安全；诊断/缓存无正文 |
| 来源 | 真实 PostgreSQL 导入同键 20 并发、异输入、相同 version 异内容、无 owner/未知引用、各阶段回滚、CLI 丢回执/重启原 importId 恢复 |
| 固定版本/B03 | 空目录→保存可选模型；execution 导入与默认固定版本；profile head 更新不暗换默认或旧项目；仅显式配置重绑定；受限仓资料修复仍通过 |
| HTTP | 六端点请求/响应、当前 owner、Origin/CSRF、字段与Unicode/字节上限、分页/过期/跨查询cursor、错误优先级、原结果链接/安全Destination |
| 浏览器 | 桌面/移动、空/加载/错误、草稿/摘要/Key keep/replace、原 save 恢复与 close 确认、存储禁止、刷新/跨标签/back/forward、真实 session401清理、合成换 actor、不能取消的晚到保存201/200和close200、旧404晚到终态 |
| 配套发布 | 三入口版本、前端资源、空库迁移/重复迁移/升级/历史漂移拒绝、发布二进制原操作恢复、未配置入口与businessReady=false，旧包原字节保护 |

实现变化后运行 `go build ./...`、`go test ./...`、`go vet ./...`、适当全量 race、`npm --prefix web ci`、前端 test/typecheck/build；真实 PostgreSQL 用例不得 skip 算 PASS。发布使用 `scripts/build.ps1 -Version <唯一新版本>` 并执行新包验证。测试方案和故障接口须先由 Astra 声明，Sol 实现；为文档准备不重跑历史实验。

每轮失败新目录保留，修复后重跑受影响验收；最终非主要实现模型独立审查实际最终源码、命令结果、SQL/HTTP/browser/release证据和边界，关闭所有 P0/P1/P2 才按证据提升本地状态。更新 README 使用说明、HANDOFF、IMPLEMENTATION-PLAN 及唯一采用文档，明确设计采用、实现、LOCAL_VERIFIED、真实外部验收与整批 VERIFIED 的区别。B02 暂停不会被 B04 本地通过改为完成。

最终汇报列实际改动、采用范围、模型分工与任何流程偏差、命令/退出码/测试数量及跳过、证据目录、新包路径、独立复核结论、未完成限制与 B02/B03/B04 真实状态。不自动提交或推送。
