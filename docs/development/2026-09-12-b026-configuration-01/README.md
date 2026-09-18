# B02.6 专用环境配置

用户在完成接手核对后，明确要求按手册帮助配置真实登录验收。用户提供 Vercel 域名 `bohanxu.me`，并选择仅在这台 Windows 电脑的浏览器验收。本轮开始实际配置，不沿用旧轮次“不启动服务”的限制。完整真实 GitHub 验收仍未执行。

## 已完成

- `https://bohanxu.me/` 的 HTTPS 校验和 HTTP 200 正常，当前是 Vercel 博客。认证 callback 路径返回 404。DNS authority 为 ns1.vercel-dns.com 与 ns2.vercel-dns.com。主域和原网站配置未改。
- 固定本机验收 origin 为 `https://repomesh.bohanxu.me:8443`，callback 为 `https://repomesh.bohanxu.me:8443/api/auth/github/callback`。Windows hosts 已新增 `127.0.0.1 repomesh.bohanxu.me`，Windows DNS API 实查返回 127.0.0.1。该子域不作为公网 RepoMesh 服务。
- `/home/xubohan/.config/repomesh` 已创建为 0700。`root-1.key` 为 UID 1000 所有、非符号链接的 0600 普通文件，原始 32 字节。代理依本次部署授权生成，没有读取、输出或哈希其正文。
- `auth.pending.json` 为非秘密配置草稿，已填 origin、callback 与实际 TLS 文件路径。App ID、Client ID 仍待导入。没有创建冒充真实凭据的 client secret 或 PEM，也没有创建最终 auth.json。
- 独立 PostgreSQL 17.11 已初始化并保持运行。数据目录为 `/home/xubohan/.local/share/repomesh/b026-postgres`，数据库为 `repomesh_b026`，socket 为 `/home/xubohan/.local/state/repomesh-b026`。目录均为 0700，本地 peer 认证，listen_addresses 为空，port 设置为 55432，无 TCP 监听。
- 最终 r1 配套 Web 二进制执行 `db migrate` 与 `db check` 均成功，实际结果为 `current=3 target=3 pending=0`。私有 runtime.env 已保存受控启动环境，正文未进入记录。

详细本机结果见[配置执行记录](local-setup.txt)，首次阶段的[原记录](local-setup-initial.txt)保留。[Windows 解析证据](windows-hosts-result.txt)与[实际执行脚本](enable-local-host.ps1)只证明本机解析，不能代替 HTTPS 验收。脚本先保存原 hosts 原字节到新的 Windows TEMP 文件，再追加专用映射；已有冲突会拒绝改写。

## 当前等待

Let's Encrypt 生产 DNS-01 申请已完成，工具为已按官方校验和验证的 lego v5.4.1。账户和证书材料保存在私有 `/home/xubohan/.local/share/repomesh-b026-acme`。2026-09-12 06:36:08 PDT 原申请返回证书，未另建申请。证书 SAN 为 `repomesh.bohanxu.me`，有效期为 2026-09-12 12:37:36 UTC 至 2026-12-11 12:37:35 UTC；证书链验证通过，公私钥匹配。证书与私钥位于 `state/certificates/repomesh.bohanxu.me.crt` 和同名 `.key`，均为 0600，父目录为 0700。TLS 路径已写入 pending 配置，原文件已保存唯一 0600 备份。早期等待状态保留于[DNS-01 原记录](acme-dns01-status.md)，不作为当前未签发结论。

用户随后授权代理处理 Vercel 配置。官方 Vercel CLI 设备登录已成功，代理确认域名所在团队后新增下列 TXT，两个权威 DNS 均返回精确值后才继续原申请。只新增此记录，未更改博客的 A、CNAME 或 nameserver。用户随后看到验证码过时，此时 CLI 已完成授权，无需重复登录。

| 字段 | 值 |
| --- | --- |
| Type | TXT |
| Name | `_acme-challenge.repomesh` |
| Value | `7XpLmTX7KIDTzmnDU9yKVuD9BQnMOsiIR4RSOBIp5TU` |
| TTL | 默认 |

该值为需公开发布的 DNS 验证值，不是 App secret、token 或私钥。添加后，先查询权威 DNS 确认精确值，再向已有 session 发送回车。若进程失效，先查看私有 STATUS.txt 和原日志，记录新轮次后再恢复，不假称原申请通过。Vercel CLI 的安装和登录材料位于私有 `/home/xubohan/.local/share/repomesh-b026-vercel`，认证正文不进入仓库或证据。

签发后已仅按本次新增的 record ID 删除挑战 TXT，Vercel 列表与两个权威 DNS 均确认清除。本次实际结果见[Vercel 与证书收尾记录](vercel-acme-complete.md)；上表保留申请过程，不需要再次添加。

[TLS 独立复核](tls-independent-review.md)确认域名、有效期、静态证书链、文件权限与配置路径，无开放 P0/P1/P2。该复核不代表浏览器 HTTPS 已通过。

[GitHub App 预填创建链接](https://github.com/settings/apps/new?name=RepoMesh-Bohan-B026&url=https%3A%2F%2Frepomesh.bohanxu.me%3A8443&callback_urls%5B%5D=https%3A%2F%2Frepomesh.bohanxu.me%3A8443%2Fapi%2Fauth%2Fgithub%2Fcallback&request_oauth_on_install=false&public=false&webhook_active=false&metadata=read&contents=write&pull_requests=write)使用固定origin、callback与三项仓库权限。用户在GitHub页面创建App，保持令牌过期开启、callback通配匹配关闭，安装测试仓库并安全放置client secret和PEM。创建链接本身不代表App已创建。

用户随后报告已点击 Create GitHub App，当前设置入口为 `https://github.com/settings/apps/repomesh-bohan-b026`。公开 `/apps/repomesh-bohan-b026` 查询返回 404，未从公开接口取得 App ID 和 Client ID。真实秘密文件尚未落地，本次创建进度不是 RepoMesh OAuth 验收结果。

## 本机导入 GitHub 材料

在 App 设置页获取 App ID、Client ID，生成 client secret，生成并下载 PEM。保持用户令牌过期开启。秘密不要粘贴到对话中。在 Windows Terminal 执行：

```powershell
wsl -d Ubuntu-22.04 -u xubohan -- python3 /home/xubohan/projects/Repomesh_Go_ver/docs/development/2026-09-12-b026-configuration-01/import-github-app.py
```

按提示在终端输入两项 ID、隐藏输入 client secret，并输入下载的 PEM 绝对路径（支持 Windows 路径）。脚本仅更新配置草稿并排他创建 0600 秘密文件，不启动服务。遇到部分失败按[恢复说明](recovery.md)处理，不重复覆盖已有秘密。

导入工具通过临时 HOME 与 PTY 的成功导入、已有文件拒绝、非 RSA 拒绝、路径不匹配拒绝及中途写入失败检查，见[最新测试结果](import-verification-03.json)。[独立初审](import-independent-review.md)发现的路径绑定和失败恢复问题已在[闭环复核](import-review-closure.md)关闭，开放 P0/P1/P2 为零。保留前两次测试记录；`verify-import-helper.py` 是验证脚本副本，若需要新验证应复制到新的临时目录执行，不能在证据目录重跑覆盖记录。

## 接续与状态

用户随后完成本机导入并回复“OK了”。已核对实际文件权限，排他创建 auth.json，并于 2026-09-12 06:51 PDT 启动 r1 Web（PID 177022）和 coordinator（PID 177023），数据库核查为 3/3/0。Windows HTTPS 探测和专用可见 Chromium 登录页通过；新的[真实验收 01](../2026-09-12-b026-live-01/README.md)记录实际结果。前文“尚未落地”“未启动”等是配置过程的早期记录。

实际浏览器验收按[既有手册](../../current/b02-github-live-acceptance.md)执行，每轮复制[模板](../2026-09-12-b02-external-preparation/live-acceptance-template.md)到新目录。自然刷新保持原连接，不改数据库期限、不模拟时钟、不手动重放令牌，也不用重新登录代替刷新。

B02.1 至 B02.5 仍为 LOCAL_VERIFIED。B02.6 已开始真实入口验收，HTTPS 登录入口 LIVE-01 通过，其余场景见新记录；整批真实验收尚未通过。B02 整体 IN_PROGRESS，B03 TODO。没有运行旧 verify-record.py、重跑历史测试、修改产品源码或 r1 包，也未提交推送。规划使用 GPT-6 Astra，实际配置与脚本使用 gpt-5.6 sol。

## 本次依据

- [DNS-01 支持未公开的服务域名](https://letsencrypt.org/docs/challenge-types/#dns-01-challenge)。本机服务通过DNS验证签证书，未放宽Cookie和证书校验。
- [GitHub OAuth callback 经用户浏览器重定向](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/about-the-user-authorization-callback-url)。本机返回仍需实际浏览器验证。
- [Vercel 添加 DNS 记录](https://vercel.com/docs/domains/managing-dns-records)。当前只需要新增专用 TXT。
- [GitHub App 预填参数](https://docs.github.com/en/apps/sharing-github-apps/registering-a-github-app-using-url-parameters)。未通过链接填写秘密。

执行安排见[计划](PLAN.md)，选择和修正见[决策记录](decisions.tsv)。gpt-5.6 terra 已[独立复核](independent-review.md)已完成的本机配置，发现的计划勾选问题已关闭。该结论不覆盖证书、HTTPS、GitHub App 或真实 OAuth。
