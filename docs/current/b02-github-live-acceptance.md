# 配置 GitHub App 并验收 B02.6

本手册用于现有 Linux B02 实现。完成配置后，按文末顺序执行真实验收。配置步骤本身不构成验收通过。当前状态见[本轮接手记录](../development/2026-09-12-b02-external-preparation/README.md)，秘密轮换和恢复仍以[认证开发说明](authentication-development.md)为准。

## 固定验收环境

1. 准备专用验收 PostgreSQL 数据库。不要复用旧测试脚本留下的临时数据库。
2. 选择一个稳定的 HTTPS origin。下文以 `https://repomesh.example.com` 为占位示例，执行前替换为自己控制的域名。
3. 将域名接到能到达本机 Web 的 HTTPS 入口。浏览器必须信任证书，证书名称必须覆盖域名。
4. 保证该入口在令牌刷新验收期间持续可用。临时随机域名变化后，原回调与 Cookie 不能沿用。
5. 允许 Web 和 coordinator 直接访问 `github.com:443` 与 `api.github.com:443`。当前 GitHub 客户端不使用 `HTTP_PROXY` 或 `HTTPS_PROXY`，不支持自定义 GitHub Enterprise 主机。

仅有桌面 SSH 项目连接不会建立浏览器 HTTPS 入口。WSL 的入站转发、宿主防火墙和 DNS 要由实际入口接通；无需改 Codex SSH 配置。

## 创建专用 GitHub App

在 GitHub 的 **Settings → Developer settings → GitHub Apps → New GitHub App** 创建 App。组织所有的 App 从组织设置进入。按以下值填写，字段位置可参考 [GitHub 注册说明](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/registering-a-github-app)。

| GitHub 设置 | 本批填写值 |
| --- | --- |
| Homepage URL | 自己的固定 HTTPS origin |
| Callback URL | `https://repomesh.example.com/api/auth/github/callback`，替换域名后与服务端逐字一致 |
| Expire user authorization tokens | 保持开启；已有 App 在 Optional Features 确认 User-to-server token expiration 已启用 |
| Request user authorization (OAuth) during installation | 关闭；安装完成后从 RepoMesh 登录页发起授权 |
| Enable Device Flow | 关闭，当前实现仅接 Web 授权流程 |
| Setup URL | 留空，安装完手动回到 RepoMesh |
| Webhook Active | 关闭，B02 没有 webhook 接收端点 |
| Repository permissions / Metadata | Read-only |
| Repository permissions / Contents | Read and write |
| Repository permissions / Pull requests | Read and write |
| 其他权限 | 保持 No access |
| Where can this GitHub App be installed? | 测试仓库都在 App 所有账号内时选 Only on this account；需安装到其他账号或组织时选 Any account |

这些权限对应当前 `AppCapability` 的工作能力核查。B02 不会因此执行提交或创建 PR。用户自己的私仓读取资格仍需单独成立。

安装时触发的 OAuth 流程可能缺少 RepoMesh 创建的 attempt、浏览器 binding 和 state。当前必须从 RepoMesh 发起登录，以便 callback 找到原尝试。GitHub 对[安装期间用户授权的说明](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app#generating-a-user-access-token-when-a-user-installs-your-app)与安装入口分开；不要把安装完成页面当作登录成功。

创建后完成以下操作。

1. 记录 **App ID** 和 **Client ID**。它们不同，在 `auth.json` 中都填写为字符串。
2. 在 **Client secrets** 生成 OAuth client secret，直接存到服务器私密文件。
3. 在 **Private keys** 生成并下载该 App 的 RSA PEM 私钥，安全传到服务器。
4. 在 **Install App** 选择测试账号或组织，再选 **Only select repositories** 并勾选专用私仓。至少准备一个当前用户有读权的已安装私仓。
5. 若要验证安装范围外缺席，再准备一个当前用户有读权但未选入安装的私仓。不要要求其必定出现在 RepoMesh，当前发现覆盖为 `partial`。

安装范围与 App 可安装账号的区别见 [GitHub 安装说明](https://docs.github.com/en/apps/using-github-apps/installing-your-own-github-app)。组织审批或 SSO 尚未完成时，记录实际阻塞；使用 SAML 的组织需先建立组织 SSO 会话，再授权 App。

不要在对话、仓库、发布包或验收 JSON 中粘贴 client secret、PEM、包装根、数据库密码或 token。后续接续只需要非秘密的 HTTPS origin 和服务端配置文件绝对路径。

## 放置服务端配置

以下以 Linux 当前账号运行两个进程为例。正式服务账号部署时，让两个进程使用相同的 effective UID，并由该 UID 拥有全部 App 和根文件。

```bash
umask 077
install -d -m 700 "$HOME/.config/repomesh"
```

在该目录中放置以下文件。秘密文件本身不得为符号链接，父目录由部署管理员控制。

- `github-client-secret` 保存 client secret，普通文件、严格 `0600`，不得超过 64 KiB。
- `github-app.pem` 保存该 App 的一个 RSA PEM 私钥，普通文件、严格 `0600`，不得超过 64 KiB。支持 PKCS#1 或 PKCS#8，至少 2048 位。
- `root-1.key` 保存原始 32 字节随机包装根，普通文件、严格 `0600`。不能填 64 字符十六进制文本。
- `auth.json` 保存非秘密配置及秘密文件路径。

首次部署的包装根由操作者在服务器生成。以下命令拒绝覆盖同名文件；重复部署复用原根。根轮换必须走认证开发说明中的流程。

```bash
(umask 077; set -o noclobber; openssl rand 32 > "$HOME/.config/repomesh/root-1.key")
```

从[配置示例](../../configs/auth.example.json)复制到尚不存在的 `auth.json`，在本地编辑器填写。示例中的 `/home/xubohan` 适用于当前账号，其他服务账号使用自己的绝对路径。JSON 内不展开 `$HOME` 或 `~`。

```json
{
  "origin": "https://repomesh.example.com",
  "appId": "替换为 App ID 数字",
  "clientId": "替换为 Client ID",
  "callbackUrl": "https://repomesh.example.com/api/auth/github/callback",
  "clientSecretFile": "/home/xubohan/.config/repomesh/github-client-secret",
  "privateKeyFile": "/home/xubohan/.config/repomesh/github-app.pem",
  "activeRootId": "root-1",
  "roots": [{"id": "root-1", "path": "/home/xubohan/.config/repomesh/root-1.key"}],
  "tlsCertificateFile": "",
  "tlsKeyFile": ""
}
```

`origin` 不带尾部 `/`、路径、查询串或 fragment。若使用非默认端口，origin 和 callback 都写同一端口。`appId` 必须为正整数的字符串。未知或重复 JSON 字段会被拒绝。

只检查文件元数据，不显示正文。

```bash
stat -c '%a %U %F %s %n' "$HOME/.config/repomesh/github-client-secret" "$HOME/.config/repomesh/github-app.pem" "$HOME/.config/repomesh/root-1.key"
```

启动成功能证明配置读取和本地自检通过。RSA 格式及私钥与 App 的匹配还要通过真实安装能力查询确认，不能只凭启动成功判定。

## 接通固定 HTTPS 入口

选择现有反向代理终止 TLS 时，将 JSON 中两个 TLS 文件字段留空，Web 监听 `127.0.0.1:8080`。代理在同一网络命名空间访问此回环地址。若代理在另一台机器，先建立受控内网连接，并按实际地址配置 upstream。

下面是已有 Nginx 的独立验收虚拟主机片段。先替换域名和已有证书绝对路径。代理原样转发 Origin、Cookie、Set-Cookie 与 Location，不缓存认证响应。配置语义见 [Nginx 代理模块](https://nginx.org/en/docs/http/ngx_http_proxy_module.html)。

```nginx
server {
    listen 443 ssl;
    server_name repomesh.example.com;
    ssl_certificate /absolute/path/fullchain.pem;
    ssl_certificate_key /absolute/path/privkey.pem;
    access_log off;
    error_log /dev/null crit;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $http_host;
        proxy_set_header Origin $http_origin;
        proxy_cache off;
        proxy_redirect off;
    }
}
```

对这个专用验收入口关闭访问日志和请求级错误日志，避免 callback 的 code、state 或请求正文进入日志。若上层还有代理、WAF 或 APM，同样关闭认证 URL 查询串、请求正文和认证头采集。调试使用 RepoMesh 的非秘密错误码。将片段加入自己的 Nginx 配置后执行 `nginx -t`，成功后由部署操作者重载该实例。本轮没有安装或改写代理配置。

若直接由 Go 提供 TLS，填写证书和私钥绝对路径，并用 `--addr 0.0.0.0:8443` 启动 Web。此时固定 origin 必须改为 `https://自己的域名:8443`，GitHub callback 同步带端口。证书需可被进程读取且被浏览器信任。不要用 `curl -k`、浏览器忽略证书或去掉 Secure Cookie 绕过验证。

## 迁移数据库并启动配套产物

使用当前 r3 包，其中包含真实验收发现的 UTC 时间输出和同步 callback Cookie 竞争修复；原 r1、r2 包保留作历史证据。已有配套资源无需重建。程序不自动加载 `.env`。从仓库根目录打开 Web 终端，以隐藏输入读取专用验收库连接串。本机已启动的专用服务见[真实验收 03](../development/2026-09-12-b026-live-03/README.md)，接手时先检查现有进程，避免重复启动。

```bash
cd /home/xubohan/projects/Repomesh_Go_ver
export REPOMESH_AUTH_CONFIG="$HOME/.config/repomesh/auth.json"
read -r -s -p '专用验收库连接串: ' REPOMESH_DATABASE_URL
echo
export REPOMESH_DATABASE_URL
export REPOMESH_RELEASE="$PWD/dist/repomesh-0.2.0-b026-cookie-20260912-r3"
"$REPOMESH_RELEASE/bin/repomesh-web" db migrate --timeout 30s
"$REPOMESH_RELEASE/bin/repomesh-web" db check
```

仅在两条数据库命令均成功，且 check 报告 `current=3 target=3 pending=0` 后启动 Web。

```bash
"$REPOMESH_RELEASE/bin/repomesh-web" --addr 127.0.0.1:8080 --assets "$REPOMESH_RELEASE/web/dist"
```

另开 coordinator 终端，使用相同的 Linux 账号、配置和数据库。

```bash
cd /home/xubohan/projects/Repomesh_Go_ver
export REPOMESH_AUTH_CONFIG="$HOME/.config/repomesh/auth.json"
read -r -s -p '同一专用验收库连接串: ' REPOMESH_DATABASE_URL
echo
export REPOMESH_DATABASE_URL
./dist/repomesh-0.2.0-b026-cookie-20260912-r3/bin/repomesh-coordinator
```

浏览器从固定 HTTPS origin 打开 `/login`。不要使用 Vite 5173 端口进行认证验收。先确认 `/healthz` 为 200，`/readyz` 为 503，未登录的 `/api/session` 为 401。若认证 API 返回 `AUTH_NOT_CONFIGURED` 503，说明当前 Web 没加载配置，不能继续记真实成功。

## 依次执行真实验收

为这次运行新建证据目录，不覆盖旧 `verification-02` 或 `browser-06`。复制[真实验收记录模板](../development/2026-09-12-b02-external-preparation/live-acceptance-template.md)，逐项填写实际时间、操作、预期与观察、脱敏证据位置和结果。所有项初始为 NOT_RUN。

1. 先执行 HTTPS 往返和取消。使用尚未授权该 App 的测试账号，在真正 GitHub 授权页点取消，检查原 attempt 为 `cancelled/USER_CANCELLED`，没有生成登录会话。若 GitHub 自动授权而未出现取消页，记录该项尚未覆盖，不能手工拼 `error=access_denied`。
2. 从 RepoMesh 发起新登录，在 GitHub 完成授权。确认返回固定 callback 后以 303 进入 `/auth/result/{attemptId}`，地址不残留 code 或 state。原尝试 `confirmed` 且当前 `/api/session` 有该账号，才算登录通过。
3. 执行真实仓库发现，完成所有游标页。核对已安装私仓的稳定 ID、`userParticipation.status` 和 `appCapability.status`；末页保持 `coverage.status=partial`。首次 503 时按页面重读原查询，不能改接替身。
4. 执行同账号重连，核对账号 ID 不变、原重连尝试 confirmed、连接 revision 更新，以及本浏览器旧 session 被撤销。另用测试账号验证错误账号重连被拒绝，原账号连接不得被覆盖。
5. 执行安装权限和失权样本，只改变专用测试 App 及测试仓库。每次变更后刷新发现，按安装设置与当前 API 观察逐项记录。安装范围外私仓缺席是合法的 partial 结果；不能因此声称验证了 `denied` 状态。需要显式 denied 样本时，使用用户仍可读取的专用公开测试仓库，实际观察 `APP_INSTALLATION_MISSING` 或 `APP_PERMISSION_MISSING`，不要假造安装响应。测试用户失去读权时，等待原读权观察超过 60 秒，再查询以触发重新核查；不得把有效缓存窗口内的观察当作新核权。完成后恢复测试配置，再重连建立最终刷新基线。
6. 保留这次连接，按下一节完成自然到期刷新。此期间不再进行该账号的登录、重连或安装权限变更。
7. 刷新成功后使用同一有效会话发起新仓库发现，最后注销并确认 `/api/session` 为 401、页面清除仓库结果。记录桌面和移动视口的实际浏览器往返。

浏览器 HAR、trace、原始网络导出可能包含 Cookie、code、state、csrfToken 和 authorizationUrl，不能原样纳入证据。仅保存允许字段与裁剪后的截图。仓库响应字段以[首批浏览器契约 §3](first-batch-browser-api-contract.md)及[发现实现](../../internal/access/discovery.go)为准。

## 等待并证明真实刷新

启用过期功能后，GitHub 的用户 token 通常有效 8 小时，refresh token 有效 6 个月。修改设置前生成的长期 token 不会自动变为过期 token。以新授权得到的期限为准。[GitHub 令牌刷新说明](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/refreshing-user-access-tokens)

当前 coordinator 在 `access_expires_at <= now() + 30 秒` 且 refresh token 未过期时领取刷新。因此首次授权后通常需等待约 7 小时 59 分 30 秒，实际按数据库期限和后台调度取证。保留 Web、coordinator 与数据库，不修改数据库期限，不模拟时间，不解密 token 后手动调用刷新接口。

在专用验收库的 psql 中，用登录后 `/api/session` 的 `user.id` 设置 `actor_id`。该 UUID 不是 GitHub 数字账号 ID。连接密码由本机受控方式提供，不写到命令行参数或证据。

```sql
\set actor_id '替换为本地 user.id'
BEGIN READ ONLY;
SELECT now() AS sampled_at, actor, revision, access_epoch, status, refresh_state,
       credential_committed_at, observed_at, access_expires_at, refresh_expires_at,
       refresh_ref <> '' AS has_refresh_token,
       access_expires_at - interval '30 seconds' AS refresh_due_at
FROM repomesh_access.connections
WHERE actor = :'actor_id';
SELECT actor, generation, created_at, last_active_at, expires_at, revoked
FROM repomesh_access.sessions
WHERE actor = :'actor_id'
ORDER BY created_at;
COMMIT;
```

先保存刷新前快照，再于到期窗口后保存同样快照。要求 `has_refresh_token=true`，两个 expires_at 均非空。成功后同 actor 的 revision 改变、access_epoch 增一、credential_committed_at 前进，两项期限前进，连接回到 `connected/idle`。期间没有其他登录或重连，才能把变化归属为刷新。随后新的真实仓库查询证明新凭据可用；不要求捕获短暂的 claimed 状态。

浏览器会话空闲期限是 30 分钟，绝对期限是 12 小时。保持同一会话时，在空闲截止前通过正常页面或同源 `GET /api/session` 请求维持活跃。页面可见不保证有请求，需观察 last_active_at。后台刷新不依赖浏览器活跃；若会话已失效，先保存刷新后的数据库证据再登录，不能把新登录的连接变化计作刷新。

`/api/session` 会把超过 60 秒的连接观察显示为 unknown。只看到 UI unknown 不能判断刷新失败。若数据库为 `unknown` 或遗留 `claimed`，记录实际状态和非秘密错误码，停止该项成功判定。不要手工重置责任位或重发旧 refresh token。后续修复和新一轮验收另留证据。

## 完成 B02 后接续 B03

只有真实验收表必需项全部有实际证据、失败已处理并独立复核后，才把 B02.6 和 B02 整体标为 VERIFIED。若因配置、GitHub 操作或自然刷新等待而未完成，继续保留 B02 IN_PROGRESS 与 B03 TODO。

B03 开工时重新对照[施工表](../plan/IMPLEMENTATION-PLAN.md)的项目创建、列表、资料编辑、明确增仓与原操作恢复范围，读取[首批浏览器契约](first-batch-browser-api-contract.md)、[首批持久化](backend-first-batch-persistence.md)及[项目配置专题](project-configuration-design.md)的已采用章节。先落实待配置项目，验收同键 20 并发、异输入冲突、丢回执和重启恢复、增仓全回滚及受限仓库下资料修复。C05/C06 留 B05，模型候选、预算和 P9 不随 B02 自动采用。
