# B02.6 再次接手核对

本轮从既有真实验收准备接续。运行环境为 Linux，工作目录为 `/home/xubohan/projects/Repomesh_Go_ver`，分支为 main，HEAD 为 `ad9a49b2fe3b5fe743a65c838ded6cb022e79363`。桌面 SSH 和项目绑定沿用用户已核验状态。

已完整读取用户指定的 18 份文件，包含当前开发入口、B02 采用与认证说明、真实验收手册、最终本地结果及上一轮准备记录。另读最终认证和文档复核。规划与接续判断由 GPT-6 Astra 承担，核对脚本由 gpt-5.6 sol 实现。

## 配置阻塞

本轮当前工具进程中 `REPOMESH_AUTH_CONFIG` 和 `REPOMESH_DATABASE_URL` 均未设置。`/etc/repomesh/auth.json` 和 `/home/xubohan/.config/repomesh/auth.json` 不存在。只读取存在性及必要元数据，没有搜索或输出秘密正文，没有扫描其他机器或部署路径。

用户随后明确回复“尚未配置”，要求提供手册路径及双方分工，并将真实登录验收暂时保持未完成。本轮据此只收尾接手记录。

按[既有手册](../../current/b02-github-live-acceptance.md)还需落实以下操作。

1. 接通稳定 HTTPS origin，使浏览器信任证书，callback 严格为 `<origin>/api/auth/github/callback`。
2. 完成专用 GitHub App 设置，启用用户令牌过期，配置 Metadata 读、Contents 写和 Pull requests 写权限，安装到专用测试仓库。授权和组织审批由账号持有人完成。
3. 在仓库和发布目录之外放置 auth.json 及其引用的 client secret、PEM、原始 32 字节包装根。秘密文件由运行进程 UID 所有，权限为 0600。
4. 将专用验收数据库连接通过服务器受控方式提供给运行进程。程序不自动加载 .env。

接续输入只需非秘密 HTTPS origin 和 auth.json 绝对路径。不要在对话中发送 client secret、PEM、包装根、数据库密码或 token。真实运行时使用最终 r1 配套 Web 和 coordinator，并依手册核查迁移。

## 操作分工

| 工作 | 账号或部署操作者 | 开发代理 |
| --- | --- | --- |
| HTTPS 入口 | 选择自己控制的域名，处理所需 DNS、路由或外部平台权限 | 检查现有入口，在可访问的专用环境中配置 TLS 或反向代理并验证证书和回调 |
| GitHub App | 创建 App、生成 client secret 和 PEM、安全放到服务器、安装测试仓库及完成组织审批 | 提供逐项设置，检查非秘密配置与安装能力的实际观察 |
| 服务器配置 | 按手册在服务器生成包装根，提供配置绝对路径。已有专用数据库的连接留在服务器受控位置 | 协助填写 auth.json，检查权限，准备专用验收库，迁移并启动 r1 配套 Web 和 coordinator |
| 真实验收 | 完成账号登录、授权、取消和账号切换。处理 GitHub 页面上的安装或权限变更 | 驱动 RepoMesh 页面，记录回调、会话、发现、自然刷新及注销证据，再安排独立复核 |

本表说明配置就绪后的分工，不表示本轮已经部署或执行。当前按用户要求保留真实验收未完成。

## 验收边界

B02.1 至 B02.5 保持 LOCAL_VERIFIED。本轮只做接手核对，准备成果继续沿用。B02.6 保持 BLOCKED，真实 LIVE-01 至 LIVE-10 均 NOT_RUN。B02 整体 IN_PROGRESS，B03 TODO。

配置就绪后，每次真实运行建立新证据目录，使用[已有模板](../2026-09-12-b02-external-preparation/live-acceptance-template.md)。刷新按实际 access_expires_at 前 30 秒的窗口观察，保存前后只读数据库快照。期间不重连、不修改期限、不模拟时间、不手动重放 token，并留意浏览器会话 30 分钟空闲期限。

本轮未启动真实服务、迁移数据库、运行 GitHub 授权，也未重跑 Go、npm、PostgreSQL、浏览器或上游历史实验。未提交、未推送。源码清单和包内哈希匹配只能证明文件与各自记录相符，不能证明可复现构建或源码到二进制的密码学来源。

## 文件核对结果

69 项源码、r1 清单中的 11 个文件和旧 batch-02 的 70 个证据文件均无缺失、新增或哈希变化。早期失败记录保持原字节。本轮没有运行旧 verify-record.py。

上一轮 workspace-baseline 早于四份合法文档修改，final-checks.json 也未保存这四份文档的终态哈希。因此只能确认它们相对早期基线已变化，无法判定准备终态以后是否再次修改。本轮另存当前基线，以便核对本轮编辑。

最终只读核对见[audit.json](audit.json)，脚本见[audit.py](audit.py)。脚本只接受新的输出目录。可使用 `python3 audit.py --repo /home/xubohan/projects/Repomesh_Go_ver --output /tmp/repomesh-b026-audit-new` 重复同类核对，执行前确保输出路径不存在。

[初次记录](audit-initial.json)保留原样。主代理发现初版进程名匹配会漏掉 Linux comm 截断后的长名称，执行者已修正并另存最终记录。进程检查只覆盖当时可见的 /proc comm，不证明其他 namespace 或改名进程不存在。认证配置候选在产品文件哈希前排除，发布和历史目录中的额外文件只列路径。

执行安排见[计划](PLAN.md)，关键选择见[决策记录](decisions.tsv)。gpt-5.6 terra 的[独立复核](independent-review.md)对本轮接手记录判定 PASS，独立复算 69、11、70 三组哈希，并检查脚本、文档链接及差异空白。复核者无法读取父任务完整逐字记录，没有据此声称审计了全部对话。
