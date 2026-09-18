# 基础工程验收记录

日期：2026-09-10 UTC（本机 America/Los_Angeles 为 2026-09-09）。范围仅为当前基础工程；不修改或重新运行历史 AgentTeams 验证。

## 阅读与分工

先读取五个规定入口并列出[阅读前文件清单](../archive/2026-09-12-development-preparation/docs/current/scaffold-source-inventory.md)。主 agent 负责产品代码、工程配置、根 README、开发说明及总交接；三位本轮 agent 分别承担 ADR/领域、AgentTeams 证据、页面/接口/后端接手文件。文件编辑归属在分派时明确，没有联系旧协作者或恢复旧任务。

- [ADR 阅读与独立复核](../archive/2026-09-12-development-preparation/docs/current/scaffold-adr-review.md)：全部 20 份 ADR（0001—0020）、领域词表及架构/技术/执行/交付专题，逐文件记账。
- [AgentTeams 证据追溯](scaffold-agentteams-evidence.md)：两份调研、当前验收清单、两轮 README 及全部 61 份报告（41＋20），重点未完成条件和恢复后受阻审计；关键结论追至原始 evidence 和 scripts，标明阅读粒度。
- [页面/接口/后端审计](../archive/2026-09-12-development-preparation/docs/current/scaffold-page-backend-review.md)：页面、接口、后端、接手、消息、仓库分析、旧项目调查、文档评审与通信历史，共 24 份。两份原 41 项接手阅读清单保留。

材料中的设计采用、锁定提交、有限实测与未验证能力分别记录，没有据上游变化替换架构，也没有把历史缺口写成已解决。

## 构建与启动检查

实际环境：Go 1.26.4 windows/amd64、Node.js 22.22.1、npm 11.13.0、PowerShell 7。前端锁定 React/React DOM 19.3.0、TypeScript 7.0.2、Vite 8.2.2；精确依赖与传递依赖见 `web/package-lock.json`。Go 仅标准库，无外部运行依赖。

| 检查 | 结果与范围 |
| --- | --- |
| `go build ./...` | 通过；三入口都编译。Git ownership 前提通过当前命令进程的 `GIT_CONFIG_*` 信任条目解决，未改变全局配置。 |
| `go test ./...` | 通过；检查存活/业务就绪、未实现 API、静态资源边界、缺失前端和在途请求关闭等待。 |
| `go vet ./...` | 通过。 |
| `go list ./...` | 仅 5 个产品包，历史 validation 不进入产品构建。 |
| `npm --prefix web ci` | 按锁文件成功安装。 |
| `npm --prefix web run typecheck` | 通过。 |
| `npm --prefix web run build` | 类型检查与 Vite 生产构建通过。 |
| 配套构建脚本 | 完整执行锁文件安装、前端构建及三个 Go 二进制构建；没有部署或启动外部服务。 |
| 三个 `--version` | 相同发布版本，退出码 0。 |
| 协调/执行默认启动 | 明确未实现，退出码 1；无工作循环、执行监听或主机操作。 |
| Web 缺失资源 | 启动前明确报错，退出码 1。 |
| 本地 HTTP 实测 | 首页、JS、CSS 200；healthz 200 且 businessReady=false；readyz 503；业务 API、未知页面和资源目录 404；POST 首页 405。 |
| 浏览器 | Playwright 实际渲染桌面和 390px 移动视口，无业务假数据；最终页面 console 0 errors/0 warnings。 |
| 配套清单 | 同包三个版本匹配，`release.json` 内 10 个文件 SHA-256 对应实际产物；前端包含在同一清单。 |

独立复核发现并关闭 HTTP 退出时序缺陷：原先 `Serve` 在 listener 关闭后返回，可能使主进程早于 `Shutdown` 请求收尾退出；现已同步等待 `Shutdown`，加入真实本地在途请求回归测试，独立复测通过。初次 Go 测试的未使用导入和浏览器 favicon 404 也已修正。未将首次失败隐去或以成功摘要代替最终检查。

最终交付包：`dist/repomesh-0.1.0-scaffold-final/`，根 README 与包内启动说明均使用这个包。构建前产生的 `0.1.0-scaffold`、`0.1.0-scaffold.1` 是本轮中间检查产物，未覆盖或删除；最终验收仅指 `scaffold-final`。

独立复核另发现 README 一度指向修复前中间包，现已在最终构建前统一标签和启动路径。复核者核对了最终包散列、三个版本、README 字节一致性及交接链接，结论为本轮范围内无未关闭问题；未参与主要代码实现，完整记录见上述独立复核报告。

浏览器截图保留在被忽略的 `output/playwright/`，用于本轮本地复核，不写入历史 validation。仅启动过本任务的本地静态 HTTP/浏览器检查，结束时关闭本任务检查进程；没有启动 AgentTeams、数据库、容器或模型服务。

## 文档与原工作树

同步了 HANDOFF、总导航、current 索引、技术/架构阶段说明、页面/接口/后端专题及两份接手 Prompt。新开发说明集中记录目录职责、依赖方向、配置、启动、配套发布与扩展边界。未新建产品 ADR 或改写既有产品决定。

对可枚举且非运行目录的原材料建立 SHA-256 基线，最终只有本轮归属内 11 份既有文档和根 `.gitignore` 发生变化；未发现其他原文件删除或字节变更。既有 CONTEXT、ADR、调研、原始通信日志、归档及两轮验证报告/脚本/evidence 保持。忽略的 runtime/private、外部服务状态不属于此次散列核对范围。`validation/go.mod` 为新增编译隔离标记，不是旧证据修改。

`git diff --check` 通过；另对新建/未跟踪文档执行本地相对链接和文本检查，因为 Git diff 不覆盖全部既有未跟踪文件。锁文件、配置示例未被忽略，依赖、构建产物、临时浏览器输出被忽略。未提交、重置、清理或恢复用户原工作树。

## 剩余范围与下一阶段

本轮骨架验收不包括 Issue、计划、调度、权限、GitHub、AgentTeams、PostgreSQL/队列/对象存储；Graph 未来仍在后台进程内，Skill 全部暂缓，Python 分析只有 ADR-0020 的扩展说明。数据库表、完整 MCP Schema、消息协议及恢复算法未冻结。

下一阶段应选定一个完整业务场景，先落实身份/权限、持久化原子范围及失败语义，再形成相应接口和测试。AgentTeams 接入需专门处理两轮报告中的控制旁路、结果未知、身份撤销、资源限额、消息恢复和原生行为差距，不能因可构建入口而跳过这些前提。

**骨架完成不等于业务或 AgentTeams 集成验收完成；旧验证的受阻及未完成项保持原义。**
