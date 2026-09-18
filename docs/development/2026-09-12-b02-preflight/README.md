# Linux 接手与 B02 前置核对

日期为 2026-09-12。用户在本任务明确要求恢复 RepoMesh 开发，并在配置核对时回复 GitHub App“尚未配置”。此前为配置 WSL 而暂停施工的指令已由本次接手要求替代。

当前实际运行在 WSL2 Linux，工作目录为 `/home/xubohan/projects/Repomesh_Go_ver`。桌面项目绑定 `remote-ssh-discovered:repomesh-wsl`，项目路径一致。依据为本次命令的[环境与工作区记录](baseline.json)及新读取的[桌面项目记录](desktop-project.json)。没有更改全局 Agent 设置或登录凭据。

B00、B01 继续沿用原 VERIFIED 结果。B02 已完成前置核对，业务实现尚未开始。认证候选仍缺明确采用，因此 B02 记为 BLOCKED。采用范围确认后即可先实现和验证本地单元；真实 GitHub App 尚未配置，只阻止真实 OAuth 与页面集成验收。本轮接手核对和文档验证单独记录，不作为 B02 验收。

## 已核实的开发环境

| 项目 | 本次观察 |
| --- | --- |
| 内核 | `Linux`，`5.15.167.4-microsoft-standard-WSL2` |
| Git | `main`，HEAD 为 `ad9a49b2fe3b5fe743a65c838ded6cb022e79363` |
| 工具 | Go 1.26.4、Node 22.22.1、npm 10.9.4、PowerShell 7.6.6、PostgreSQL 17.11、Codex 0.153.4 |
| 命令位置 | Go、Node、npm、Codex 位于 `/usr/local/bin`，PowerShell 位于 `/usr/bin` |
| 现有工作 | 已记录 41 个修改或未跟踪文件的 SHA-256，并在本次临时目录保存其原字节；初始清单遗漏、本任务未改动的既有配置文件另见[补测记录](baseline-supplement.json) |
| RepoMesh 配置 | 当前进程无 `REPOMESH_` 环境变量；根目录与 `configs` 仅发现配置示例；用户确认 GitHub App 尚未配置 |

Codex 版本命令退出 0，同时报告只读文件系统阻止创建 PATH aliases。现有命令已正常执行，本轮没有改权限或据此重装工具。

## B02 的采用范围

已采用部分包括稳定外部账号身份、用户读权与 App 工作能力分开、原操作恢复，以及[首批浏览器契约](../../current/first-batch-browser-api-contract.md)的 session 和仓库列表字段。[F01](../../current/login-recovery-page-design.md)采用的三项是问题优先、重连未知可查询或稍后处理、确认后显式继续且不重提业务。

新认证流程、HTTP 字段、Cookie 与多标签策略、发现扫描和时间参数仍是候选。唯一字段来源为 [RM-FIRST-ACCESS-r3](../../current/authentication-browser-api-draft.md)，汇总范围为[完整候选包 §4](../../current/first-batch-complete-review.md#4-③认证发现统一裁决范围)。用户的恢复开发要求解除旧施工暂停，但没有点选上述产品取舍，本轮不改写其采用历史。

建议采用以下 B02 最小实施包。表内全部仍为待确认范围。

| 范围 | 具体方案与影响 |
| --- | --- |
| A1 接入来源 | 首批仅 github.com 的单一 GitHub App 用户授权。安装范围外的受邀私仓可能不显示，列表如实返回 partial，J1 完整发现目标保留。 |
| A2、A3 认证与恢复 | 同源 HTTPS 服务端会话，Origin 和 CSRF；回调以 state、PKCE、浏览器绑定核验。登录与重连各有原 attempt 查询；固定 Destination 生成站内路径，不接受 returnUrl；错误账号不合并。 |
| A4 并发登录与注销 | 新尝试使旧未完成尝试失效。注销只撤销请求所带会话及其绑定代次，不发送可能删除新登录 Cookie 的迟到删除头。旧标签页可能需要重新登录。 |
| A5 期限与流控 | binding 绝对 7 天，会话绝对 12 小时且空闲 30 分钟，attempt 10 分钟，非秘密结果最多 24 小时。墓碑保留到相关有效期结束。发起限流每浏览器 5 次每分钟、每账号 10 次每分钟；查询按 2、5、10、30 秒退避，隐藏页面暂停。 |
| A6、A7 发现与观察 | 每次扫描最多 2 页且预算 5 秒，批次最长 10 分钟；coordinator 以持久有限待办续扫。读观察最多 60 秒，写入重新核实且距提交最多 15 秒，本地代次改变立即失效。这些窗口不构成外部撤权时限承诺。安装不足联系部署管理员。 |
| 认证所需 S01、S02 | 按[来源稿 §2](../../current/backend-first-batch-sources-draft.md#2-s01s02秘密保存权限与恢复)采用仓库外根文件及 PostgreSQL 信封加密，覆盖 App 秘密、用户令牌、刷新令牌和短期交换材料。每条秘密独立数据密钥，绑定用途及归属，包装次数持久预扣，上限一百万次。包含文件权限、恢复、自检和根轮换约束。模型秘密的业务入口仍在 B04。 |
| B02 所需 S06 | 只纳入登录身份、仓库元数据发现和用户读权与 App 能力分开的观察。Issue 建项及实际写入的动作矩阵留到对应批次。本包不采用模型预算、测试、出站或 P9。 |

HTTP 的精确字段、错误和数据保留规则继续由链接的唯一专题维护，本表不另建协议。后续 `conversation_message` Destination 继续拒绝；已有只读会话目标沿 r3。项目、Issue、会话和模型操作页的真实返回验收随所属业务批次补齐，B02 的无权、未知及路径逃逸用例需先验证，不能伪造目标有权。

## 施工表需要补足的依赖

认证交换和令牌刷新在 B02 就需要秘密基础，不能等到 B04 模型保存时才保护凭据。应先实现上述认证所需 S01、S02，再由 B04 接入模型业务。这里调整的是实施顺序，采用状态仍待确认。

认证候选 §7 要求发现任务不依赖 HTTP 请求存活。因此 B02 还包含 coordinator 的有限发现处理器、持久领取及旧 claim 隔离。当前 [coordinator](../../../cmd/repomesh-coordinator/main.go) 仅报告未实现。新增处理器不启动 AgentTeams 或受限宿主执行，也不把整个产品的 `/readyz` 改成就绪。

## 真实配置清单

用户已确认未配置 GitHub App。以下配置在真实 OAuth 与页面集成验收前准备。采用范围确认后，本地数据库、会话和发现任务的实现及替身验证可以先行；替身结果必须与真实外部证据分开。秘密值不进入对话、仓库、日志或发布包。

| 配置 | 用途与检查 |
| --- | --- |
| GitHub App ID、Client ID | 确定 RepoMesh 专用 App；不把开发用 Git 凭据或 Codex 账号当成产品 App |
| 站点 HTTPS Origin 与固定 callback | callback 为站点的 `/api/auth/github/callback`，须与 App 配置一致；浏览器实际信任证书 |
| Client secret、App 私钥的受控路径 | 服务端读取并保护；OAuth 交换与 App 安装能力查询分开 |
| 根文件绝对路径与 rootKeyId | Linux 文件权限及独立备份；不自动生成生产根 |
| 独立 PostgreSQL 开发实例 | 保存认证、秘密及发现待办；普通 Web 启动的配置变更须同步说明 |
| 测试账号与专用仓库 | 覆盖成功、同账号重连、错误账号、安装覆盖不足及撤权场景；不得使用共享业务数据制造故障 |

尚未选择站点域名、HTTPS 接入方式或实际配置文件格式。本轮未创建 App、生成根密钥、修改 GitHub 权限、启动外部服务或读取秘密正文。

## 采用后按单元施工

1. 比较 B02 的数据形状与接口草图，先落实认证秘密、稳定账号、浏览器绑定、会话、尝试及发现批次的持久关系。业务源码依据实际用例新增，验证使用独立 PostgreSQL。
2. 实现登录、重连、退出和尝试查询。覆盖同键并发、交换未知、错误账号、旧回调、旧注销、Cookie 丢失、Origin、CSRF 和返回目标核权。
3. 实现凭据刷新唯一责任及发现续扫。验证刷新结果未知不重发，进程重启后继续原批次，旧连接和旧 claim 无法回写，partial 与空页游标准确。
4. 接入已采用的登录页面布局及确定的认证行为。真实浏览器完成 GitHub 往返，并单独记录替身测试与真实证据。
5. 运行相关 Go 和前端完整检查，独立复核代码、运行证据及清理结果。只有 B02 验收完整才标为 VERIFIED，再按施工表进入 B03。

## 本轮验证

本次只更新文档和接手证据。检查本地链接、引用章节、Git diff 格式及 41 项既有文件保留情况，结果见[最终文档检查](document-checks-final.json)。gpt-5.6-sol 独立复核指出证据补记、文字歧义和过度阻塞说明，主代理已修正，见[独立复核](review.md)。本轮未运行 Go、npm、数据库、OAuth 或登录页面测试，B00、B01 的原始验收保持。

本轮工作步骤见 [PLAN](PLAN.md)，决策见 [decisions.tsv](decisions.tsv)。Prove It Works 促使本轮直接核对 Linux 命令和桌面项目；Sequence Work into Verifiable Units 促使本轮分开环境验收与 B02 业务验收。
