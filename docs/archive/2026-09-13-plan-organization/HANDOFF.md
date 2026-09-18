# RepoMesh 当前交接

> 整理前的历史副本，来源为 `3ad89bd`。当前状态见[当前交接](../../current/HANDOFF.md)和[施工计划](../../plan/IMPLEMENTATION-PLAN.md)。旧指令不构成新任务授权。

更新：2026-09-13。施工进度见[分批 TODO plan](../../plan/IMPLEMENTATION-PLAN.md)。开始开发时先读[开发前阅读与行动指南](../../plan/DEVELOPMENT-START.md)，再按[现行索引](../../current/README.md)进入唯一专题。旧阶段进度、通信和接手 Prompt 已移入[本次归档](../2026-09-12-development-preparation/README.md)，不再作为当前任务指令。

## 当前完成度

最新接续入口：[B04 验收报告](../../development/2026-09-13-b04-acceptance-01/README.md)、[B04 采用记录](../../current/b04-model-sources-adoption.md)、[B04 收口 01](../../development/2026-09-13-b04-closeout-01/README.md)、[收口后修复 01](../../development/2026-09-13-b04-validation-stay-01/README.md)与[B04—B06 设计后交接](../../development/2026-09-13-b04-b06-handoff-01/HANDOFF.md)。用户原话“推进B04”仅授权 D01—D04 与 U04.1—U04.4。该授权范围已结束。B05／B06 仍待各自采用，不自动开工。

### 2026-09-13 B04 授权范围结束

用户确认可宣布 B04 结束并合入 `main`。结束范围只覆盖已采用的 D01—D04 与 U04.1—U04.4：`INTEGRATED_LOCAL_VERIFIED`，含收口后 P1／P2 修复。验收见 [B04 验收报告](../../development/2026-09-13-b04-acceptance-01/README.md)。`main` 为 `621592d`。不是整批 `VERIFIED`。没有真实模型请求。`businessReady=false`。进入 B05 仍须另作 D05／D06／C05／C06 采用。

### 2026-09-13 B04 收口后复核

收口合并后，另一次独立复核在已合并代码上发现一项 P1：校验未通过的保存仍跳到原操作页，丢失未秘密字段且看不到 `VALIDATION_FAILED`。另有三项 P2：`0005` 完整保存触发器含 `OR true`、拒绝回执无法清理 vault、锁序文档与实现不一致。本轮在 `cursor/b04-save-validation-stay-45b9` 修复这些问题；验证见 [B04 收口后修复 01](../../development/2026-09-13-b04-validation-stay-01/README.md)。不把收口当时的复核改写成当时已通过这些项。不启动 B05／B06。

### 2026-09-13 B04 收口

B04 为 `DESIGN_ADOPTED`、`IMPLEMENTED`、`INTEGRATED_LOCAL_VERIFIED`。完整工程检查、S01—S12 真实 PostgreSQL、B03 原回归、U04.4 夹具浏览器、配套发布 r2 与[独立复核](../../development/2026-09-13-b04-closeout-01/FINAL-INDEPENDENT-REVIEW.md)已完成。未采用 D05—D08。没有整批 `VERIFIED`。没有真实模型请求。`businessReady=false`。进入 B05 还要另作 D05／D06／C05／C06 采用。

### 2026-09-13 B04 实施

B04 曾为 `LOCAL_VERIFIED`（S01 到 S12 已有真实 PostgreSQL 证据）。该检查点已被上方收口替代，历史缺口记录不改写成当时已完整收口。

### 2026-09-13 B04—B06 设计交付

2026-09-13 B04—B06 前置设计已形成[本轮交付](../../development/2026-09-13-b04-b06-design-01/README.md)，含架构比较、事务/核心声明、C05/C06、P9及DB/CB验收映射；[独立复核](../../development/2026-09-13-b04-b06-design-01/REVIEW.md)已通过，无开放P0/P1/P2。新推荐仍待采用，未开始产品实现或运行验收。B02外部暂停、B03 INTEGRATED_LOCAL_VERIFIED及B04 DESIGN_PREPARED_NOT_ADOPTED保持；B05/B06实施仍TODO。B09仅本轮四项数据兼容问题的静态部分覆盖，完整G1/G2未完成。

后续实施前须明确采用交付中的D01—D08、S06及模型Key UI/协议范围，按U04/U05/U06单元落实；本轮交回后不自动实施或继续B07—B11。下方同日“分工记录”和09-12交接是历史检查点，不能覆盖本次设计进度。

### 2026-09-13 前置设计分工记录

用户要求将 Astra 可提前承担的架构、协议和函数声明工作列入必读及计划，并明确限制工作范围。现已新增[Astra 前置设计分工](../../plan/ASTRA-DESIGN-PREPARATION.md)，并接入[开发必读](../../plan/DEVELOPMENT-START.md)和[施工计划](../../plan/IMPLEMENTATION-PLAN.md)。B04、B05、B06、B09—B11 由 Astra 重点做前置设计，B07 做契约复核，B08 做验收设计，Sol 按收口设计实现；可提前程度和定稿依赖以指南为准。每项包含允许内容、排除项、交付终点及文件／操作边界，B09-G1 的提前兼容审查与完整 G1／G2 分开派工；完成指定单项后不自动启动相邻批次或实现。

本次仅文档记录，没有完成新的架构、接口声明或产品实现，没有采用具体候选。B03 仍为 INTEGRATED_LOCAL_VERIFIED，B04 仍为 DESIGN_PREPARED_NOT_ADOPTED，B02 外部验收继续暂停；下方历史验证与恢复记录保留。

### 2026-09-12 B03 主目录集成检查点

用户已明确允许合并 B03，并决定搁置账号验证、优先开发主要计划功能；后续真实账号验证只使用主账号 A。这一决定解除 B03 等待 B02 VERIFIED 的旧顺序门槛。B02 外部验收保持 `PAUSED_BY_USER` 和 `IN_PROGRESS`；未来跨账号 LIVE-05 与 LIVE-08-USER-READ 为 `DEFERRED_BY_USER`，不计为 PASS，也不再自动建立 second-account-03 重跑。下方 22:31 PDT 检查点中的 LIVE-05 `FAIL`、LIVE-08 `NOT_RUN` 和 `RESTORE_IN_PROGRESS` 均保留为真实历史，外部清理责任没有取消。

B03 已完成主目录集成，状态为 `INTEGRATED_LOCAL_VERIFIED`。[最终独立复核](../../development/2026-09-12-b03-integration-01/FINAL-INDEPENDENT-REVIEW.md)无开放 P0/P1/P2；配置分页的事务连接复用、提交中/未知结果的输入锁定、明确失权后的敏感页面清理共两项 P1、一项 P2 均已修复并验收。最终后端真实 PostgreSQL 73 通过、0 跳过，完整 Go build/test/vet/race 通过；前端 28/28、普通浏览器 25 项及空配置目录 1 项、配套发布均通过。新包为 `dist/repomesh-0.3.0-b03-integrated-20260912-r1/`，`businessReady=false`。本结论不表示外部 GitHub、部署或整批业务 VERIFIED。历史 worktree `/home/xubohan/projects/Repomesh_B03`、旧证据与发布包保持原位；最终证据和失败分类见[主目录集成 01](../../development/2026-09-12-b03-integration-01/README.md)。

B04 为 `DESIGN_PREPARED_NOT_ADOPTED`，本轮只准备[下会话交接](../../development/2026-09-12-b04-handoff-01/HANDOFF.md)和[可复制 Prompt](NEXT-TASK-B04-PROMPT.md)，无产品实现。下一会话先由 GPT-6 Astra 完成设计综合、函数接口声明与集中采用，再由 gpt-5.6-sol 实现，另由非主要实现模型独立复核；不得自动采用后续候选或发送真实付费模型请求。

### 2026-09-12 22:31 PDT 暂停检查点

以下保留当时事实与顺序记录；其中 B03 等待 B02 VERIFIED、主目录 NOT_RUN、必须重跑第二账号的指令，已被顶部最新用户授权与现行状态替代。历史 FAIL/NOT_RUN 与外部 RESTORE_IN_PROGRESS 不改写，也不表示恢复责任取消。

本检查点覆盖下文较早的运行状态，但保留其历史过程。LIVE-09、LIVE-10 已通过最终独立复核，不重跑。第二账号验收 02 已观察到 B 初始完整分页中 B-only 仓库存在，且 A-owned 仓库的 `userParticipation` 与 `appCapability` 均为 `allowed`；整轮 LIVE-05 仍为 FAIL，LIVE-08 为 NOT_RUN。失败时间线证明 A 同账号 reconnect 先 confirmed，12 秒后又出现独立 B login 并改变 A 浏览器上下文；这没有覆盖错误账号 reconnect，不能表述为 `ACCOUNT_MISMATCH` 产品缺陷。两个 RepoMesh 会话均已通过正常 UI 注销，logout 204，后续 session 401。当前为 RESTORE_IN_PROGRESS，冷启动接续以[暂停交接](../../development/2026-09-12-b026-second-account-02/PAUSE.md)为准。

外部现状：专用 App 仍为 PUBLIC，GitHub 因 B installation 仍存在而拒绝 Make private；B installation `161284386` 仍存在且只选择仓库稳定 ID `1368000734`；B 仍是 A-owned 仓库稳定 ID `1367444901` 的协作者；B OAuth grant 尚未撤销。A installation `161172403` 是否保持原范围与权限、以及 A 是否无法访问 B 仓库，均待恢复后的最终只读复核。GitHub profile A 的 CDP `9231`、Chrome PID `50296` 当前为 `catbobyman` / `137759882`；profile B 的 CDP `9230`、Chrome PID `45576` 当前为 `bohanxu111` / `328458192`；两个进程均存活，页面 target ID 不作为永久入口。r3 Web PID `287037`、coordinator PID `287038` 存活，固定 origin 为 `https://repomesh.bohanxu.me:8443`。非秘密 auth config 绝对路径为 `/home/xubohan/.config/repomesh/auth.json`；只把路径用于接续定位，不要读取或输出文件正文。

恢复顺序固定为：先由 B 卸载 installation `161284386` 并验证 absent；再由 A 将 App 改回 private 并验证；移除 B 协作者并验证无待邀请；由 B 撤销 OAuth grant；验证 A installation 的选择范围与三项权限不变，以及 A 无 B 仓库访问；更新恢复证据并做独立复核。完整恢复后必须新建 `2026-09-12-b026-second-account-03` 证据目录重跑 LIVE-05 与 LIVE-08，不覆盖本轮 02。B02 保持 IN_PROGRESS；B03 worktree 为 LOCAL_VERIFIED，主目录 integration NOT_RUN。当前未 commit、未 push，全部现有修改保留，旧 `verify-record.py` 未运行。

本轮新增授权允许 B02.6 等待期间先在 `/home/xubohan/projects/Repomesh_B03` 的 `codex/b03-project-management` worktree 开发 B03，B02.6 VERIFIED 后再集成验证。规划、编排、架构和函数接口声明使用 GPT-6 Astra，实现使用 gpt-5.6 sol。11:00 PDT 左右的[接手核对](../../development/2026-09-12-b026-pickup-01/README.md)确认 r3 服务、原 observer 和 keeper 存活，epoch 5 及原 session 保持有效，刷新结果尚未生成。B02 仍 IN_PROGRESS。2026-09-12 12:56 PDT 的实际只读复核确认 r3 Web PID 287037、coordinator PID 287038、observer PID 293161、Windows keeper PID 52940 存活；observations 共 180 条，最新 sampledAt 为 `2026-09-12T19:56:24Z`，actor 为 `16198e14-7249-4cc2-a15e-57cd8b568874`、epoch 5、revision 为 `fb6d2db3-5069-483f-9c79-5efb16b58921`、原 generation 5 有效，结果文件尚未生成。

B03 已在上述隔离 worktree 实现完成并达到 worktree LOCAL_VERIFIED：72 个真实 PostgreSQL 测试通过、0 跳过，完整 Go/race、HTTP、实际 Web 进程恢复、前端 28 项测试、普通与空配置目录实际浏览器及配套发布均通过；最终独立复核无开放 P0/P1/P2。证据位于 `/home/xubohan/projects/Repomesh_B03/docs/development/2026-09-12-b03-01/`，最终复核文件为其中的 `FINAL-INDEPENDENT-REVIEW.md`；新发布包位于 `/home/xubohan/projects/Repomesh_B03/dist/repomesh-0.3.0-b03-worktree-20260912-r1/`。本轮 B03 的 GitHub 使用受控替身，不是 B02 真实 GitHub 验收。主目录集成为 NOT_RUN，整批 B03 未 VERIFIED，等待 B02.6 完整 VERIFIED 后集成重验。下文此前 B03 TODO 和必须先等 B02 才实施的记录由此处顺序授权及当前状态替代。

已实现 PostgreSQL 显式迁移、认证秘密基础、GitHub App 登录／同账号重连、服务端会话、授权结果恢复与持久仓库发现。Web 配置认证后连接数据库，coordinator 负责身份核实、刷新、发现续扫及秘密维护；主机执行入口仍未实现，`/readyz` 保持 503。专用环境已运行 r3 Web 与 coordinator，真实登录、同账号重连及安装权限已有逐项通过证据，完整 B02.6 验收仍未通过。自然刷新正在等待实际到期窗口；第二账号已创建，相关两项等待本轮刷新结束后执行。项目、模型、Issue 和 AgentTeams 运行接入仍待后续批次。使用方式见[认证说明](../../current/authentication-development.md)，本地证据见[B02 记录](../../development/2026-09-12-batch-02/README.md)。[B01 记录](../../development/2026-09-12-batch-01/README.md)及旧[骨架验收](../../current/scaffold-verification.md)保留为历史基线。

会话与 Issue 分离、页面原子建项、首批项目／列表契约、最小消息与澄清契约均已有列明的采用范围。三进程权限、复用上游仓内 DAG、业务与待办同事务、资源原子预留的方向已采用；Skill 工程暂缓。以[ADR 索引](../../adr/README.md)的局部替代关系和各专题具体章节为准。

首批管理范围已经确定；认证、模型保存／测试／应用、配置来源、恢复及详情新增决定见[完整候选包](../../current/first-batch-complete-review.md)，仍未整体采用。已采用的 F01—F04 具体 UI、创建弹窗、关联控件、提交反馈、DAG 和 dock 布局保留；原型可点击不代表新协议已采用或实现。

## 最近审查与修订

[09-12 审查修订](../../current/design-readiness-revisions.md)补充了 Key 原操作安全终结、Issue 固定配置关联和认证返回候选，修复模型测试观察不一致，并澄清 Skill 验证证据范围。[修订检查](../../reviews/2026-09-12-design-fixes/README.md)是文档与内存原型证据，不是数据库或真实模型验收。

可开发已确定的独立管理单元；接收供以后执行的真实 Issue／待办前，须先确定并落实[配置绑定](../../current/issue-configuration-binding-design.md)。[五项执行协议门槛](../../current/execution-integration-gates.md)仍未全部完成，不能把管理闭环当成 Manager／Worker 工作闭环。

[项目契约检查](../../reviews/2026-09-12-project-contracts/README.md)已澄清无变更配置保存、同步旧认证注销步骤及当前设计入口。还须补齐项目预算／时限的只读摘要，以及模型应用预览中原模型的受限形状与历史定位；它们分别影响配置摘要和完整替换预览，不阻止已确定的独立项目管理单元开发。新增候选和 P9 的采用状态保持。

## 历史接手状态（现行状态以上方检查点为准）

用户已在 2026-09-12 的本次任务恢复开发。本次直接运行命令确认 WSL2 Linux，工作目录为 `/home/xubohan/projects/Repomesh_Go_ver`；桌面项目列表确认同一路径绑定 `remote-ssh-discovered:repomesh-wsl`。开发工具均使用 Linux 路径，当前 Git 为 `main`、HEAD 为 `ad9a49b2fe3b5fe743a65c838ded6cb022e79363`。已核对 41 个既有修改或未跟踪文件并保存其原字节，初始遗漏的一项另留补测记录。详见 [B02 接手记录](../../development/2026-09-12-b02-preflight/README.md)。

B00、B01 保持 VERIFIED。用户已明确回复“确认，继续”，采用 [B02 最小实施包](../../current/b02-authentication-adoption.md)，B02 保持 IN_PROGRESS，本地秘密、持久认证、GitHub 适配、发现续扫和页面已 LOCAL_VERIFIED，含完整工程检查、42组真实 PostgreSQL 用例、race、9项浏览器场景及独立复核。GitHub App 尚未配置，只阻止真实 OAuth 和页面集成验收。实际完成单元及证据见 [B02 记录](../../development/2026-09-12-batch-02/README.md)。

认证所需 S01、S02 已纳入 B02；coordinator 的持久发现续扫也随本批实现。模型、预算、执行及 P9 候选仍保留各自采用范围，不因认证采用而整体生效。

本轮再次接手已核对当前 69 项源码清单、最终 r1 包 11 个文件哈希及最终验收统计，均与原证据匹配；未重跑历史验收。新增[真实 App 配置及验收手册](../../current/b02-github-live-acceptance.md)与[接手核对记录](../../development/2026-09-12-b02-external-preparation/README.md)。当前进程未设置认证配置或数据库连接，约定配置位置未发现文件；没有读取秘密正文，也没有启动真实服务。B02.6 仍 BLOCKED，B02 整体 IN_PROGRESS，B03 TODO。后续规划、编排及函数接口设计使用 GPT-6 Astra，实现使用 gpt-5.6 sol。

后续从 B02.6 再次接手的结果另存[本轮核对记录](../../development/2026-09-12-b026-resume-01/README.md)。当前工具进程仍无认证或数据库环境配置，两个约定 auth.json 路径不存在。69 项源码、11 个包文件和旧 70 个证据文件与各自清单匹配；未运行真实验收或历史测试。只向部署操作者索取非秘密 HTTPS origin 与配置绝对路径，其他位置配置状态未知。上一轮四份已修改文档没有终态哈希，本轮不据旧基线断言它们此后未再改动。B02.6、B02、B03 状态保持。

用户再次确认“尚未配置”后，进一步委托代理按手册实际配置，并提供 Vercel 域名 bohanxu.me，选择只在本机 Windows 浏览器验收。最新[专用配置记录](../../development/2026-09-12-b026-configuration-01/README.md)已建立。独立持久 PostgreSQL 已运行，r1 迁移和核查为 current=3、target=3、pending=0；私密包装根与配置草稿已创建。Windows 将 repomesh.bohanxu.me 解析到 127.0.0.1，目标 origin 为 https://repomesh.bohanxu.me:8443。用户授权代理处理 Vercel 后，官方 CLI 登录成功；代理添加专用 TXT 并确认权威 DNS，原 DNS-01 申请已签发生产证书，证书链及密钥匹配检查通过，TLS 路径已写入草稿。实际 HTTPS 尚未启动验证。用户报告已创建 repomesh-bohan-b026 App，公开查询未取得参数；真实 App 凭据及最终 auth.json 仍未配置，Web 与 coordinator 未启动。真实登录验收仍未完成，B02.6 BLOCKED、B02 IN_PROGRESS、B03 TODO。

此前 WSL 登录故障已完成[桌面真实请求验收](../../development/2026-09-12-wsl-ssh/DESKTOP-AUTH-VERIFIED.md)。[WSL 环境验收](../../development/2026-09-12-wsl/README.md)保存 Windows／Linux B01、Linux 发布包、Windows 浏览器及 Vite 更新结果；本次接手核验后继续 B02 开发，没有重跑这些历史验收。Windows 原工程保留，无需全局切换 Agent／终端。旧[暂停记录](../../development/2026-09-12-batch-01/PAUSE.md)保留为历史，当前主目录和进度以本节为准。未提交、未推送本次修改。

## 历史接续顺序（已由顶部最新授权替代）

最新运行状态：用户完成本机 GitHub App 导入后，已生成 `/home/xubohan/.config/repomesh/auth.json` 并启动 r1 Web（PID 177022）和 coordinator（PID 177023）。数据库启动核查为 3/3/0；Windows 正常校验证书访问固定 HTTPS 登录页返回 200。06:51:48 PDT 的只读快照中尚无授权尝试、会话或连接。新建[真实验收 01](../../development/2026-09-12-b026-live-01/README.md)逐项保存后续观察；前文未配置、未启动均为之前阶段记录。完整真实验收仍未通过，B02 整体 IN_PROGRESS，B03 TODO。

真实验收 01 随后发现[非 UTC 时间输出缺陷](../../development/2026-09-12-b026-live-01/utc-failure.md)：r1 的真实 attempt observedAt 返回 `-07:00`，前端按 UTC 契约拒绝。两次登录发起均为 201，但尚未进入 GitHub 授权或创建会话。数据库 timestamptz 读取现已统一 UTC，[回归、完整检查及独立复核](../../development/2026-09-12-b026-utc-fix-01/README.md)完成。07:08 PDT 已切换到 `dist/repomesh-0.2.0-b026-utc-20260912-r2/`，Web PID 192940、coordinator PID 192941；原 r1、失败记录、运行环境备份和数据库期限保留。最新[真实验收 02](../../development/2026-09-12-b026-live-02/README.md)正在继续，专用 Chromium 已恢复为单个主页面并等待账号持有人登录 GitHub。完整授权、取消、安装、重连和自然刷新尚未完成，B02.6 仍未通过。

08:26 PDT 的最新实测已通过 Windows 浏览器 HTTPS 入口与真实 GitHub 取消授权。实际 callback 303、原 attempt cancelled/USER_CANCELLED、结果 URL 无 code/state 残留；只读数据库为零会话、零连接。证据见[真实验收 02](../../development/2026-09-12-b026-live-02/README.md)。用户已在 Windows 专用 profile 登录 GitHub；解决该浏览器访问本机域名误走系统代理后，当前浏览器主 PID 29320、CDP 127.0.0.1:9230、固定 target CBEA86CD7C41D085994D442CEB5B60A4，白名单监控 Node PID 46132。完整授权同意、安装权限、重连和自然刷新仍未完成，尚无真实 token 到期计时。前文等待登录及取消未执行为此前状态。

08:44 PDT 真实登录、08:48 PDT 同账号重连均已通过。用户先前同意在旧尝试 10 分钟到期后才返回，过期证据保留；显式新建尝试后 GitHub 沿用已给予的同意，真实 callback 303、confirmed、session 200。GitHub 当前账号 catbobyman 的数字 ID 137759882 与本地账号对应，显示名 catmem；身份和登录证据的第二次独立复核通过。同账号重连后 user.id 不变、连接 revision 更新、旧会话撤销。首次发现 13 项均读权 allowed、App 能力 denied/APP_INSTALLATION_MISSING、coverage partial。专用 App 尚待安装，第二测试账号及专用私仓正在准备。当前连接的刷新窗口为 2026-09-12 16:48:07.487276 PDT，这由重连产生，不是自然刷新证据；最终刷新基线尚未封存。详见[真实验收 02](../../development/2026-09-12-b026-live-02/README.md)，B02 整体仍 IN_PROGRESS，B03 TODO。

09:09 至 09:18 PDT 的实际验收进一步确认安装 161172403 只覆盖专用私仓 1367444901，该私仓真实读权与 App 能力均 allowed，LIVE-06 已通过独立复核。浏览器正常 API 游标请求取完三页、14 项且无重复，末页 partial；未将其冒充页面默认 50 项的分页按钮操作。新增安装外的专用私仓 1367467551，在 GitHub 当前账号下可读，RepoMesh 搜索为空且 partial。临时将专用 App 的 Pull requests 降为 read 后，同一安装内私仓仍读权 allowed，而 App 能力为 denied/APP_PERMISSION_MISSING。App 请求配置已恢复 write，但安装授予权限仍是 read，GitHub 正等待账号持有人在 `/settings/installations/161172403/permissions/update` 点击 Accept new permissions；之后还须实际重新发现确认 allowed。复核与各项证据均在[真实验收 02](../../development/2026-09-12-b026-live-02/README.md)，LIVE-07/08 仍 IN_PROGRESS。

第二测试账号仍未提供，错误账号重连和读权撤回尚未执行。Windows Node PID 51412 已启动正常会话维持，每 10 分钟 GET /api/session，首次 200 已记录；只维护现有浏览器活跃度，截止原会话绝对期限，不登录、不重连、不刷新令牌。主监控仍为 PID 46132，固定 main target 不变；权限批准页面 target 为 BB9E3828289E2C77B28BF717352CF7F8。最终自然刷新基线尚未封存，B02 整体 IN_PROGRESS，B03 TODO。

09:44 PDT 已实测权限恢复。同一专用私仓的用户读取资格与 App 能力均为 allowed，后续独立复核通过。用户明确没有第二个测试账号，错误账号重连及撤回用户读权两项保持 BLOCKED，不再重复索取第二账号。09:45 PDT 建立新刷新基线时发现真实回调竞争。数据库 confirmed、generation 3 会话有效，浏览器却没有收到新 Cookie，随后 session 401。旧会话维持进程于 09:48 PDT 因 401 停止。原失败保存于[刷新准备 01](../../development/2026-09-12-b026-refresh-01/PLAN.md)。

10:01 PDT 最新状态如下。[Cookie 竞争修复](../../development/2026-09-12-b026-cookie-fix-01/README.md)已完成可控回归、完整工程验证、race 和独立源码复核，新的 r3 配套包已构建。原 r1、r2 各 11 项包文件、旧 batch-02 的 70 项证据及 .codex/config.toml 保持原字节。已切换 Web PID 287037 和 coordinator PID 287038，09:58 的真实登录与 09:59 的同账号重连均收到新 Cookie，session 200、attempt confirmed、旧会话撤销，独立真实回归复核通过。见[真实验收 03](../../development/2026-09-12-b026-live-03/README.md)。

当前最终基线为 reconnect cbfce23e-631b-4d97-a66a-97b25e0d60bb、epoch 5、generation 5。自然刷新窗口于 2026-09-12 17:58:45.991534 PDT 开始，access token 于 17:59:15.991534 PDT 到期。Windows 会话维持 PID 52940 首次正常 GET 返回 200，数据库 last_active_at 实际前进。

观察 01 的 Linux PID 292515 在首条样本后因 INVALID_TIMESTAMP 退出。Python 3.10 旧解析器无法接受 PostgreSQL JSON 裁去尾零后的部分小数秒位数，实际格式核对和工具回归已复现。原失败、基线及旧脚本保留在[观察 01](../../development/2026-09-12-b026-refresh-01/README.md)。新工具修复通过独立复核后，10:07 PDT 启动[自然刷新观察 02](../../development/2026-09-12-b026-refresh-02/README.md)，Linux PID 293161，沿用 Windows keeper PID 52940。01 到 02 之间存在采样间隔；只读连续性快照确认同一连接版本、令牌期限、generation 5 有效且没有新尝试，02 基线与 01 原字节一致，没有重新登录或重连。

原 Windows Chrome target 保持在工作区，观察期间不要登录、重连或手动注销。完成或失败会写入观察 02 的新输出；只有自然刷新确认后才自动执行后续私仓发现、移动视口、注销和浏览器往返脚本。目前未出现自然刷新通过结果。

10:37 PDT，用户报告已创建第二账号 `bohanxu111`，公开 GitHub 资料确认账号存在；10:41 PDT 用户确认邮箱验证完成。LIVE-05 错误账号重连和 LIVE-08 用户读权撤回不再因账号或邮箱验证缺失而 BLOCKED，改为 READY_PENDING。为保护当前 epoch 5 自然刷新基线，在观察和 LIVE-10 完成前不把该账号加入专用 Chrome、不改 App 可见性或安装范围、不发送私仓邀请，也不发起新登录或重连。执行时仍需实际观察账号登录、App 授权及邀请接受。第二账号的分阶段执行门槛、状态转换和恢复顺序已写入[第二账号验收 01](../../development/2026-09-12-b026-second-account-01/README.md)；该目录当前仅为 PREPARED_NOT_RUN，不是通过证据。

1. 先检查观察器的 observations.jsonl、outcome.json、confirmed-refresh.json 和 browser-result.json 是否存在及实际内容，核对运行进程，避免重复启动或重连污染当前基线。只有真实前后快照和浏览器结果完整后才进行独立结果复核。随后配置第二账号并执行错误账号重连与用户读权撤回；两项通过前不能将 B02 整批标为 VERIFIED。模型、预算、P9 等后续候选保持各自采用范围。
2. B00 基线及 B01 数据库基础已通过运行检查。B02 完整验收后按[施工表](../../plan/IMPLEMENTATION-PLAN.md)进入 B03 项目管理，再实现模型配置、Issue 原子创建及查询／恢复。每组独立验证，未完成的真实集成不标通过。
3. 再完成 G1—G2 的真实 Manager 往返、G3—G4 的单仓执行，最后完成 G5 的两轮与跨仓恢复。

前端／接口的 F01—F15 去向见[页面交接](../../current/HANDOFF-PAGE-API-DESIGN.md)；后端 B01—B08 去向见[后端交接](../../current/HANDOFF-BACKEND-DESIGN.md)。归档与契约审查之后已开始分批开发，首先交付数据库基础。认证采用范围以 B02 记录为准，模型及 P9 候选的采用状态保持。

历史依据：[设计采用记录](../../current/design-delegation.md)、[整理前交接全文](../2026-09-12-development-preparation/docs/current/HANDOFF.md)、[原始审查](../../reviews/2026-09-12-design-readiness/README.md)。历史角色、任务 ID 和技术轮次用于溯源，不自动召回旧协作者。
