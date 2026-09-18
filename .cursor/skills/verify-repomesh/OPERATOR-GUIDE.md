# RepoMesh 本机验证操作指导

给操作员看。代理读 `.cursor/skills/verify-repomesh/SKILL.md`。本页说明你要填什么、文件放哪、怎么开跑。

默认在本机 `http://127.0.0.1:18080` 驱动页面。不要把公开域名填进 `config.yaml` 的 `origin`。不要准备模型 API Key。B02 要的是 GitHub App 登录，不是模型请求。

## 先选范围

| `run_scope` | 做什么 | 你最少要准备 |
| --- | --- | --- |
| `unconfigured-only` | 未配置认证的登录壳、探针、本机页面 | 复制示例 YAML 即可 |
| `account-a-live` | 账号 A 的真实 GitHub 登录与发现 | YAML 加 live 字段，Linux 上放 `auth.json` 和秘密文件 |
| `restore-leftovers` | 清理上次第二账号实验留下的 GitHub 状态 | 上一行全部，外加 restore 布尔项 |

当前交接规定以后真实验证只用账号 A。不要准备账号 B。LIVE-05 和 LIVE-08-USER-READ 仍是 `DEFERRED_BY_USER`。

## 第一步：复制 YAML

在仓库根目录执行：

```bash
cp .cursor/skills/verify-repomesh/config.example.yaml .cursor/skills/verify-repomesh/config.yaml
```

只改 `config.yaml`。不要提交它。不要在 YAML 里写秘密正文，只写路径、ID、布尔值。

改完后检查：

```bash
python3 .cursor/skills/verify-repomesh/helpers/load-config.py --check
```

## 第二步：按范围填字段

### 三种范围都有

| 字段 | 填什么 | 默认 |
| --- | --- | --- |
| `listen.host` | 本机回环地址 | `127.0.0.1` |
| `listen.port` | 空闲端口。不要用环境里已有的 `8080` | `18080` |
| `origin` | 留空。空表示 `http://<host>:<port>` | `""` |
| `run_scope` | 上表三选一 | `unconfigured-only` |

`origin` 只能是 loopback HTTP。填公开域名会被拒绝。

### 仅 `unconfigured-only`

其它字段可保持示例空值或 `false`。不必填 GitHub 账号，不必放 `auth.json`。

### `account-a-live` 必填

| 字段 | 填什么 |
| --- | --- |
| `auth_config_path` | Linux 上 `auth.json` 的绝对路径。只要路径 |
| `reuse_existing_postgres_and_wrap_root` | 库里已经有认证行时必须 `true`。不要另造同名包装根 |
| `github_account_a_login` | 主账号 A 的 GitHub 用户名 |
| `installed_private_repo_id` | A 能读、且已装进 App 的私仓稳定数字 ID |
| `out_of_install_repo_id` | 可选。A 能读、未装进 App 的仓 ID，给 LIVE-08 分页或缺席用 |
| `confirmations.holder_will_click_github_ui` | 必须 `true`。GitHub 授权、取消、安装、改权限由你本人点 |
| `database.url_env_var` | 例如 `REPOMESH_DATABASE_URL` |
| `database.connection_url_file` | 或填一个只含连接串的文件路径。二选一。不要把密码写进 YAML |

### `restore-leftovers` 再加这些

先在 GitHub 上核过，再把对应项改成 `true`。

| 字段 | 含义 |
| --- | --- |
| `app_visibility_private_again` | App 已改回 private |
| `installation_161284386_gone` | 安装 `161284386` 已卸 |
| `collaborator_and_invite_gone` | 协作者和待处理邀请已无 |
| `b_oauth_grant_revoked` | B 的 OAuth grant 已撤 |
| `a_installation_161172403_selects_only_1367444901` | A 安装仍只选仓库 `1367444901` |
| `account_a_cannot_see_1368000734` | A 看不到仓库 `1368000734` |

## 第三步：秘密放在 Linux 上，不要发给代理

YAML 和对话里都不放正文。这些文件由运行 Web、coordinator 的同一 Linux 用户拥有，普通文件，权限 `0600`，不能是符号链接。

| 文件 | 作用 |
| --- | --- |
| `auth.json` | App ID、Client ID、**HTTPS** origin、callback、秘密文件路径。从 `configs/auth.example.json` 复制 |
| GitHub App client secret | OAuth client secret，≤64 KiB |
| App RSA PEM | PKCS#1 或 PKCS#8，至少 2048 位 |
| 包装根 | 原始 32 字节随机数，不是十六进制文本 |
| TLS 证书或反向代理 | 浏览器信任的 HTTPS。产品登录 cookie 带 `Secure` |

`auth.json` 的 `callbackUrl` 必须等于 `<HTTPS origin>/api/auth/github/callback`。产品要求这里是 HTTPS。技能默认驱动地址仍是 YAML 里的本机 HTTP。

不要提供：模型供应商 Key、账单 Key、账号 B、Cookie、OAuth `code`／`state`、PEM 正文、包装根字节、带密码的连接串。

## 第四步：开跑

仓库根目录，Linux：

```bash
python3 .cursor/skills/verify-repomesh/helpers/load-config.py --check

RUN_ID="$(date -u +%Y%m%dT%H%M%S)-$$"
export REPOMESH_VERIFY_RUN="$RUN_ID"
export REPOMESH_VERIFY_STATE="/tmp/repomesh-verify-$RUN_ID"
export REPOMESH_VERIFY_EVIDENCE="$PWD/.cursor/skills/verify-repomesh/evidence/$RUN_ID"

bash .cursor/skills/verify-repomesh/helpers/launch.sh
bash .cursor/skills/verify-repomesh/helpers/doctor.sh
```

未配置模式再跑：

```bash
bash .cursor/skills/verify-repomesh/helpers/drive-unconfigured-login.sh
bash .cursor/skills/verify-repomesh/helpers/drive-local-gates.sh
```

浏览器打开 YAML 里的本机 origin，例如 `http://127.0.0.1:18080/login`。不要用 Vite `5173`。

结束时只清本轮进程：

```bash
bash .cursor/skills/verify-repomesh/helpers/cleanup.sh
```

证据目录会留下。不要按进程名 `pkill`。

`launch.sh` 通过后应看到 `ready origin=http://127.0.0.1:18080`。未配置 doctor 为 `healthz=200`、`readyz=503`、`/api/session` 503 `AUTH_NOT_CONFIGURED`。live doctor 在本机 HTTP 上可为 `/api/session` 401 `AUTHENTICATION_REQUIRED`。

## 本机 HTTP 能证明什么

能证明：进程起来、未配置登录壳、`/` `/projects` `/settings/models` 的未登录外壳、对应 API 的 503／404。

不能证明：浏览器收下 `__Host-repomesh-session`、真实 GitHub 授权往返、已登录仓库列表、已登录项目保存、真实模型请求。那些要 `auth.json` 的 HTTPS origin，并由你在 GitHub 页面点击。未配置时 `/api/model-providers` 现在是 404 `not_implemented`，不是 503。

## 已跑通的 HTTPS 回环路径

Cloud VM 账号 A 的成功路径（不要改端口去迁就地址栏）：

1. 技能 Web 听 `http://127.0.0.1:18080`，coordinator 共用同一份 `auth.json` 和数据库。
2. 本机 TLS 反代听 `https://127.0.0.1:18443`，转到 18080。解开上游分块后必须写 `Content-Length` 并关闭连接，否则 Chrome 会一直等到超时。反代脚本不要进 git。
3. `auth.json` 的 `origin` 必须恰好是 `https://127.0.0.1:18443`，`callbackUrl` 为该 origin 加 `/api/auth/github/callback`。
4. 浏览器只打开 `https://127.0.0.1:18443/`。不要用地址栏打开 `:18080` 或环境默认 `:8080` 做 cookie 证明。
5. 自签证书会显示 Chrome **Not secure**。这只说明 LIVE-01（公有 CA）做不到，不自动等于 cookie 失败。继续授权后 `__Host-` cookie 仍可被收下。
6. 证据只记 cookie 标志：`Secure`、`HttpOnly`、`SameSite=Lax`、`Path=/`。不要写 cookie 值、OAuth `code`、PEM、数据库密码、HAR。
7. 会话空闲 30 分钟。等 LIVE-09 自然刷新时，用 HTTPS origin 上的 `GET /api/session` 保活。不要改库里的过期时间，不要点「退出登录」。
8. 连接观察超过 60 秒会显示「GitHub 连接待确认」，这不是列表失败。安装内测试仓允许时的药丸是「App 能力已核实」。
9. 匿名 HTTPS 探针：`bash .cursor/skills/verify-repomesh/helpers/probe-https-origin.sh https://127.0.0.1:18443`。快照：`helpers/live-snapshot.sh`。

## 踩坑

- 不要按进程名 `pkill`。只清本轮 `$REPOMESH_VERIFY_STATE` 里的 PID。LIVE-09 等待中的 18080 实例不要当垃圾清掉。
- 未配置证明若 18080 已被 live 占用，另写一份 yaml，端口改成空闲端口（例如 18082），用 `REPOMESH_VERIFY_CONFIG` 启动。
- GitHub 对已授权账号可能自动同意，没有 Cancel 页，LIVE-02 会缺。不要为了制造 Cancel 而弄坏当前登录。
- 改 App 权限可能卡在 GitHub sudo／2FA 的 Confirm access。持有人点不了就把 LIVE-07 记成 BLOCKED，不要改权限，也不要用「安装里没有某仓」冒充 `APP_PERMISSION_MISSING`。
- GUI 代理常会编造中文文案。以截图为准。
- 库里已有认证行时，`reuse_existing_postgres_and_wrap_root` 必须是 `true`，不要另造同名包装根。
- 旧 origin `https://repomesh.bohanxu.me:8443` 的 PASS 不能继承到新 App／新 origin。
- 会话空闲 30 分钟后，已配置 origin 回到「开始使用 RepoMesh」和「使用 GitHub 登录」，不是未配置的「暂时无法确认登录状态」。
- 不要把带私仓名的完整仓库列表截图提交进 git 或 PR。只记稳定 ID 和药丸文案。

## 填好之后

把 `config.yaml` 放在上述路径。在对话里只说范围和路径已经填好。不要粘贴文件正文。代理按 `SKILL.md` 和功能图继续跑。

## 本轮 LIVE 进度

当前 Cloud VM 账号 A 清单是 [CHECKLIST.md](../../../docs/development/2026-09-14-b026-cloud-live-01/CHECKLIST.md)。每做完一项就改那一页。新 origin 或新 App 另开目录。
