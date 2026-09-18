# B02 认证与仓库发现

2026-09-12，本地实施单元 B02.1—B02.5 已 LOCAL_VERIFIED；整批保持 IN_PROGRESS。B02.6 真实 GitHub 验收受 App 尚未配置阻塞，B03 未开始。用户已确认本批最小实施包，采用范围见[采用记录](../../current/b02-authentication-adoption.md)，单元及状态见 [PLAN](PLAN.md)。

## 实际交付

| 单元 | 实现与验证 |
| --- | --- |
| B02.1 秘密基础 | 迁移 0002、AES-256-GCM 信封加密、权威 AAD、包装预算持久预扣、根自检／轮换、不可复活的销毁墓碑；真实 PostgreSQL 并发和失败回归通过。 |
| B02.2 认证 | 迁移 0003、稳定账号、浏览器绑定、会话、登录／同账号重连、幂等尝试、结果恢复与刷新；未知外部 POST 不重领，跨浏览器迟到提交不能覆盖新连接。 |
| B02.3 发现 | 持久批次与游标、稳定 ID 排序、有限续扫、连接和 claim 版本隔离、用户读权与 App 工作能力分开核查；完成但未交付的原批可重读。 |
| B02.4 页面与组装 | 登录、恢复结果、仓库首页及注销；严格 HTTP／JSON 边界、Linux 私密文件配置、Web 与 coordinator 启动、前后端配套发布。 |
| B02.5 本地验收 | 完整工程检查、真实数据库、race、浏览器故障交互、发布清单和非主要实现者复核通过。 |

GitHub 协议适配调用固定 HTTPS 主机并限制超时、响应大小和重定向；本轮使用受控替身验证，没有调用真实 GitHub。项目、Issue、会话和模型原操作的目标授权尚未实现，结果页不会凭目标形状产生已授权跳转。`/readyz` 仍返回 503，主机执行入口仍未实现。

设计比较见 [design.md](design.md)、[候选 A](candidate-a.md)、[候选 B](candidate-b.md)，取舍过程见 [decisions.tsv](decisions.tsv)。按 poteto-mode 的 Sequence Verifiable Units 与 Prove It Works 原则，将本地可验证单元和外部验收分别记录。

## 最终证据

| 证据 | 结果与覆盖 |
| --- | --- |
| [标准验收 verification-02](verification-02/checks.json) | LOCAL_VERIFIED，44 项检查。Go build/test/vet、npm ci/typecheck/build、13 项前端 API 测试、认证 race、三个入口、迁移重复执行／数据库重启／历史漂移和 HTTP 检查通过；脚本实例已停止并清理。 |
| 同上 Go JSON 输出 | 81 个顶层测试通过，含 42 组 PostgreSQL 测试；计入子测试共 63 个 PostgreSQL PASS 事件，数据库测试跳过数为 0。根文件异主测试因无 chown 权限跳过，未计为通过。 |
| [最终发布包浏览器 browser-06](browser-06/checks.json) | Chromium 153.0.8010.12，9 个场景通过。使用最终 r1 二进制和同包前端资源；未配置场景访问真实 Go API，其余场景用浏览器 API 替身及受控时钟。 |
| [发布核验](release-verification-r1.json) | Linux/amd64，三个入口版本一致，11 个清单文件哈希匹配，认证配置已入包。产物为 `dist/repomesh-0.2.0-b02-local-20260912-r1/`。 |
| [独立认证复核](auth-review.md)、[最终文档复核](documentation-final-review.md)及[问题关闭记录](review-resolution.md) | 最终 PASS，无开放的 P0/P1/P2。包含认证事务、秘密、部署配置和 UI 代次增量复核，区分复核者实跑与读取主任务证据；文档复核另重新计算最终包全部哈希。 |
| [专属数据库停止记录](test-instance-cleanup.json) | 手工测试实例已停止，未触及共享服务。专属数据目录仍在 `/tmp` 保留以便追溯；标准脚本创建的实例另由脚本清理。 |
| [最终结构核对](final-checks.json) | 文档链接、差异空白、既有修改原字节备份及未授权改动核查；41 个原文件备份匹配，35 个现文件保持原字节，6 个按本次任务继续更新。源码清单见 [final-source.json](final-source.json)，可用 [verify-record.py](verify-record.py)重新核对。 |

浏览器覆盖未配置状态、未知提交刷新后不自动重发、显式同键重试、回执确认但会话缺失、当前连接不替代重连回执、继续前重核会话、空页游标和末页 partial、移动视口及键盘、429 冷却与隐藏页面暂停，以及注销后丢弃迟到仓库响应。[桌面截图](browser-06/workspace-desktop.png)和[移动截图](browser-06/workspace-mobile.png)保留中文字体正常的最终结果。

## 复跑

从仓库根目录运行。需要 Linux、Go 1.26、Node.js 22.12+、PowerShell 7、PostgreSQL 17；race 要求启用 cgo 并有 GCC 或相应 C 编译器。数据库脚本会创建自己的临时实例，不读取共享开发库。

```bash
pwsh -NoProfile -File scripts/verify-batch.ps1 -Batch B02 -PostgresBin /usr/lib/postgresql/17/bin
```

浏览器脚本使用独立目录安装的 Playwright。本轮版本为 1.63.0；Chromium 及 Linux 动态库、中文字体也放在 `/tmp/repomesh-b02-browser`，没有修改产品依赖或全局系统安装。该临时工具目录可能被系统清理，复跑前应准备可启动的 Chromium 和中文字体。下列路径对应本机本轮安装，`browser-07` 必须不存在；脚本拒绝覆盖证据目录并在结束时关闭自己启动的 Web 和浏览器。

```bash
export REPOMESH_PLAYWRIGHT_DIR=/tmp/repomesh-b02-browser/node_modules/playwright
export PLAYWRIGHT_BROWSERS_PATH=/tmp/repomesh-b02-browser/browsers
export FONTCONFIG_FILE=/tmp/repomesh-b02-browser/fonts.conf
export LD_LIBRARY_PATH=/tmp/repomesh-b02-browser/libraries/usr/lib/x86_64-linux-gnu
export REPOMESH_WEB_BINARY="$PWD/dist/repomesh-0.2.0-b02-local-20260912-r1/bin/repomesh-web"
export REPOMESH_WEB_ASSETS="$PWD/dist/repomesh-0.2.0-b02-local-20260912-r1/web/dist"
export REPOMESH_BROWSER_EVIDENCE=browser-07
node docs/development/2026-09-12-batch-02/browser-check.mjs
```

## 未完成的外部验收

用户已明确回复 GitHub App“尚未配置”。按[认证配置说明](../../current/authentication-development.md)准备专用 App、固定 HTTPS callback、服务端凭据和根文件后，验收真实授权／取消、同账号重连、刷新、App 安装权限、私仓发现及浏览器跨站往返，才能将 B02 整体标为 VERIFIED。当前不创建真实 App，不生成生产根，不进入 B03。

本轮未执行真实数据库进程 kill、网络 commit-ack 丢失、时钟回拨或长期 15 分钟清理窗口实验；相关路径的证据是事务源码审查与受控数据库测试。当前认证启动仅支持 Linux，Windows 编译及历史 B01 验收不证明 Windows 认证可用。

前置环境、桌面 SSH 绑定和 41 个既有修改文件的基线见[接手记录](../2026-09-12-b02-preflight/README.md)。原字节备份保留，未重跑历史 WSL／AgentTeams 实验，未提交或推送。早期失败和中间产物保留，按[关闭记录](review-resolution.md)解释，不改写为全部通过。
