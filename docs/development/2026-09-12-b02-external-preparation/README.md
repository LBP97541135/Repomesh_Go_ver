# B02.6 接手核对与真实验收准备

2026-09-12 接手当前未提交工作树。已确认 Linux，工作目录为 `/home/xubohan/projects/Repomesh_Go_ver`，分支 main，HEAD 为 `ad9a49b2fe3b5fe743a65c838ded6cb022e79363`。桌面 SSH 和项目绑定沿用用户已核验状态，本轮没有重配或再次查询绑定。

本轮交付[真实 GitHub 配置及验收手册](../../current/b02-github-live-acceptance.md)、[待执行验收表](live-acceptance-template.md)和本地证据核对。B02.1 至 B02.5 仍为 LOCAL_VERIFIED，B02.6 因真实配置未就绪保持 BLOCKED，B02 整体 IN_PROGRESS，B03 TODO。

## 已核对的原始材料

完整读取 AGENTS.md、根 README、HANDOFF、DEVELOPMENT-START、IMPLEMENTATION-PLAN、B02 采用记录、认证开发说明，以及旧 batch-02 的 README 与 review-resolution。另读现行索引、领域语言、认证部署与 GitHub 刷新及发现源码、最终认证复核和最终文档复核。

gpt-5.6 sol 在专属临时目录解析原始 JSON 并复算哈希，主代理读取实际 JSON 和关键源码后集成到本目录。[审计 JSON](evidence-audit.json)仅记录本轮读取和比对结果，不声称重跑旧检查。

| 原始证据 | 本轮直接核对结果 |
| --- | --- |
| [verification-02](../2026-09-12-batch-02/verification-02/checks.json) | 44 项；37 条退出码契约、6 条 HTTP 状态契约和 PostgreSQL gate 均匹配。LOCAL_VERIFIED，failure 为 null，cleanupPassed 为 true。 |
| 同上 go-test JSON 事件 | 81 个顶层 Go PASS，42 个顶层 PostgreSQL PASS，含子测试共 63 个 PostgreSQL PASS 事件，PostgreSQL skip 为 0。TestRootFileOwner 仍单列为跳过。 |
| [browser-06](../2026-09-12-batch-02/browser-06/checks.json) | 9 个场景均通过，externalGitHub 明确为 NOT_RUN。 |
| [release-verification-r1](../2026-09-12-batch-02/release-verification-r1.json)及实际包 | 11 个清单文件均存在且哈希匹配，无额外包文件，release.json 本身除外。三个入口版本一致的结论来自原验证记录。 |
| [final-source](../2026-09-12-batch-02/final-source.json)与当前源码 | 同口径 69 个文件，无缺失、额外文件或哈希差异。 |
| [auth-review](../2026-09-12-batch-02/auth-review.md)、[documentation-final-review](../2026-09-12-batch-02/documentation-final-review.md)、[review-resolution](../2026-09-12-batch-02/review-resolution.md) | 最终 PASS、无开放 P0/P1/P2 与原始发现的关闭追加记录一致；未把早期失败改为通过。 |

包内两个配置示例、package-lock 和三个前端构建文件与当前对应文件相同。`SOURCE-README.md` 是构建时快照，与接手时根 README 有两处文字差异，分别是本地验收进度和 cgo 前置说明。本轮保留该快照，不改包或清单。现有哈希证明当前文件与记录相符；没有执行可复现构建，也不将它表述为源码到二进制的密码学来源证明。

旧 [verify-record.py](../2026-09-12-batch-02/verify-record.py)会重写 final-source.json 和 final-checks.json。本轮只读其源码，没有运行它。早期 verification-01、browser-01 至 browser-05、失败 JSONL 及无 r1 的核验均保留原字节。

## 配置就绪度

本轮只查看环境变量是否存在、约定文件是否存在，以及进程名。`REPOMESH_AUTH_CONFIG`、`REPOMESH_DATABASE_URL`、`REPOMESH_WEB_ADDR`、`REPOMESH_WEB_ASSETS` 在当前工具进程均未设置。`/etc/repomesh/auth.json` 和 `/home/xubohan/.config/repomesh/auth.json` 不存在。进程名筛选未发现 repomesh、postgres、nginx、caddy 或 cloudflared。见[配置存在性记录](configuration-presence.json)。

这项检查不扫描其他部署位置，不证明用户没有在其他机器配置 App。结合用户已明确的“尚未配置”，当前没有可用于真实验收的部署入口。没有请求用户发送秘密，也没有创建 App、生成根、迁移真实数据库或启动服务。

手册按当前源码补齐以下准备。

- GitHub App 具体选项、安装范围和三项仓库权限。安装期间自动 OAuth 关闭，由 RepoMesh 创建原尝试。
- 固定 HTTPS callback、反向代理或直接 TLS、Linux 文件权限及同 UID 运行。
- 使用最终 r1 配套包迁移并启动 Web 与 coordinator，避免重建已验收产物。
- 真实授权、取消、同账号及错误账号重连、私仓发现和 App 权限样本。
- 启用用户 token 过期，保留连接约 8 小时到自然刷新窗口，使用非秘密只读数据库快照证明刷新。真实刷新期间不重新登录或重连。

GitHub 设置与 token 期限已查阅当前官方文档，引用保留在手册相应步骤。手册中的域名、证书路径和 App 标识仍为占位值；未连接实际 HTTPS 部署验证。

## 修改保护与验证边界

[工作区基线](workspace-baseline.json)保存接手时 562 个 tracked 或非 ignored untracked 文件的哈希，不含本轮新文件。[旧 batch-02 基线](batch-02-baseline.json)保存该目录全部 70 个文件哈希。主代理是仓库唯一写入者，只继续编辑本轮相关文档；产品源码、.codex/config.toml、旧证据与发布包均保留。

规划和配置分析使用 GPT-6 Astra，证据执行使用 gpt-5.6 sol。新文档由 gpt-5.6 terra [独立复核](independent-review.md)，最终 PASS，无开放问题。复核期间同步了 PLAN 的完成勾选，主代理另要求纠正报告中两处源码行号，技术事实未改变。本轮没有新增产品函数，后续函数接口设计仍由 GPT-6 Astra 承担，实现交给 gpt-5.6 sol。[计划](PLAN.md)记录吞吐检查点，[决策记录](decisions.tsv)保留实际选择和证据。

[结构检查](structural-checks.json)记录文档链接、Bash 语法和受保护文件核对；[收尾检查](final-checks.json)覆盖复核收尾后的文档。它们不包含 HTTPS 部署、Nginx 配置执行或真实 GitHub 验收。

本轮仅需校验新增文档的链接、命令与源码事实，以及受保护文件哈希；未重跑 Go、npm、PostgreSQL、浏览器或历史 AgentTeams 实验。配置步骤是待执行说明，不能记作启动或外部验收已通过。未提交、未推送。

## 下一步

部署操作者按手册配置专用 App、固定 HTTPS 和服务器私密文件。配置就绪后，仅提供非秘密的 origin 与 auth.json 绝对路径，代理即可检查启动条件并接续真实验收。GitHub 登录、授权同意或组织安装审批由账号持有人完成；验收结果按实际浏览器和服务端观察记录。

B02.6 全部真实必需项通过且完成独立复核后，再更新 B02 整体状态并核对 B03 采用章节。B03 的完整设计开工核对未在本轮提前完成；模型、预算、P9 采用状态不变。
