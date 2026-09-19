# RepoMesh

多仓库协作产品，采用 React／TypeScript／Vite 前端和同一 Go module 中的三个进程。已实现 PostgreSQL 迁移、GitHub App 认证与仓库发现、项目管理，以及 B04 模型供应商保存和执行来源导入。B03、B04 当前为 `INTEGRATED_LOCAL_VERIFIED`，非整批业务 VERIFIED；Issue、运行和 AgentTeams 集成尚未实现。

当前完成度、真实 GitHub 验收的暂停状态及恢复条件见[当前交接](docs/current/HANDOFF.md)。首次接手可按[全局阅读指南](docs/current/AGENT-READING-GUIDE.md)查阅；实施顺序见[计划导航](docs/plan/README.md)，专题和历史证据见[文档导航](docs/README.md)。

## 开发环境

当前主开发环境为 WSL2 Linux，主副本位于 `/home/xubohan/projects/Repomesh_Go_ver`。以下命令默认在 Linux／WSL 的 Bash 中、从仓库根目录执行；其他机器使用自己的克隆路径。Windows 上进行认证开发时进入 WSL，当前认证秘密文件读取仅支持 Linux。既有环境配置和 SSH 接入过程见[WSL 记录](docs/development/2026-09-12-wsl/README.md)和[SSH 接入记录](docs/development/2026-09-12-wsl-ssh/README.md)。

| 工具或条件 | 用途 |
| --- | --- |
| Go 1.26 或以上 | 构建、运行三个 Go 入口及测试。版本要求见 `go.mod`。 |
| Node.js 22.12 或以上、npm | 安装前端依赖、开发、测试及构建。版本要求见 `web/package.json`。 |
| PowerShell 7，命令为 `pwsh` | 配套打包和数据库批次验证；普通源码启动不需要。 |
| PostgreSQL 17 | 当前数据库验证基线；启用认证及持久业务时需要可连接的开发数据库。只查看未配置页面不需要数据库。 |
| GCC 等 C 编译器、启用 cgo | 运行 `go test -race` 及包含 race 的 B02／B03 批次验证。 |
| 专用 GitHub App、HTTPS、秘密文件 | 登录和后续持久业务的前置配置，见下方认证启动步骤。 |

检查当前终端使用的工具，WSL 中应使用 Linux 工具：

```bash
command -v go node npm
go version
node --version
npm --version
# 使用打包或数据库验证脚本前再检查
pwsh -NoProfile -Command '$PSVersionTable.PSVersion.ToString()'
/usr/lib/postgresql/17/bin/postgres --version
go env CGO_ENABLED
```

PostgreSQL 工具路径按本机安装位置调整。前端按已提交的 `web/package-lock.json` 安装；Go 依赖和校验和保存在根 `go.mod`、`go.sum`。`validation/go.mod` 和 `third_party/go.mod` 隔离历史实验及独立上游，根目录 Go 检查不进入它们。当前页面和认证启动不需要 Docker、Python 或启动 AgentTeams。

### 云端代理的 Install／Start 配置

截图所示环境设置继续使用仓库中的两个脚本：

| 设置项 | 内容 |
| --- | --- |
| Install Script | `bash .cursor/install.sh` |
| Start Script | `bash .cursor/start.sh` |

安装脚本要求环境预先提供符合上表版本的 Go、Node.js、npm 和 OpenSSL。它检查工具、按需安装 PostgreSQL 17、初始化专用开发集群、下载 Go 依赖并构建前端；安装 PostgreSQL 的分支使用 Debian／Ubuntu 的 `apt` 和 `sudo`。PowerShell 或 race 工具缺失时会提示，不影响普通源码启动。

启动脚本只启动该开发集群，创建 `repomesh_dev`，显式应用当前源码的迁移并核查。默认数据目录为 `$HOME/repomesh-pg`，端口为 5432，连接串保存在权限为 `0600` 的 `connection-url.txt`。初始化前可通过 `REPOMESH_DEV_PG_ROOT` 和 `REPOMESH_DEV_PG_PORT` 指定独立目录及端口，后续使用相同设置；已有目录不重建。

[环境配置](.cursor/environment.json)中的 Web 终端读取该连接文件后单独启动 Web。Start Script 内的环境变量不会自动传到其他终端；执行数据库命令和集成测试前，在各自终端加载：

```bash
export REPOMESH_DATABASE_URL="$(cat "${REPOMESH_DEV_PG_ROOT:-$HOME/repomesh-pg}/connection-url.txt")"
export REPOMESH_TEST_DATABASE_URL="$REPOMESH_DATABASE_URL"
```

这些连接只用于脚本创建的开发库。认证材料和 HTTPS 仍按下方步骤配置，coordinator 需另行启动；脚本不会自动恢复真实 GitHub 验收。网络访问沿用环境设置，安装依赖需要能访问相应的软件包源。

## 首次启动：查看页面

此方式启动未配置认证的 Web，用于查看页面和进程诊断。

```bash
npm --prefix web ci
npm --prefix web run build
go run ./cmd/repomesh-web --addr 127.0.0.1:8080 --assets ./web/dist --auth-config=
```

打开 [本地 Web](http://127.0.0.1:8080)。显式空的 `--auth-config=` 覆盖当前终端可能已有的认证配置；Web 不连接数据库，认证和项目 API 返回 503；模型 API 此时未注册，返回 404。页面不能完成登录或保存业务数据。Ctrl+C 停止 Web。

另开终端可检查：

```bash
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
```

`/healthz` 返回 200，只表示进程存活；`/readyz` 仍返回 503，`businessReady=false`。即使完成认证配置，当前完整业务就绪条件也尚未满足。

## 项目、仓库和 Issue 的操作顺序

登录后从左上角「管理 / 新建项目」进入项目管理：先保存项目名称和用途，再进入该项目的仓库页，明确勾选并接入仓库。项目可以暂时没有仓库；仓库和执行配置未就绪时不能创建 Issue。

新建 Issue 时再次勾选本次工作的仓库范围。候选只来自当前项目，项目后续增仓不会扩张已有 Issue。控制台不会自动取第一个项目、默认全选仓库或为了创建 Issue 隐式建项目。项目选择按账号保存，切换项目会重新读取列表和工作台。

迁移 `0042_task_repository_scope.sql` 为任务补充可约束的项目／Issue／仓库绑定；历史越界或缺少 Issue 的任务保留，但调度不再消费。升级后可用只读查询 `SELECT * FROM public.task_repository_scope_violations` 查看待核实记录，不能用自动增仓来消除这些记录。关联计划或任务的 Issue 可归档，清除会返回冲突。实施与本地验证见[范围修复记录](docs/development/2026-09-19-project-scope/README.md)。

## 启用认证与持久业务

### 准备配置

1. 准备自己拥有的 PostgreSQL 开发数据库及连接串。迁移账号需要在该库创建 schema、表并访问迁移对象的权限；程序不会创建数据库本身。
2. 按[认证开发说明](docs/current/authentication-development.md)和[GitHub App 操作手册](docs/current/b02-github-live-acceptance.md)准备专用 App、App ID、Client ID、client secret 和 RSA 私钥。登录授权与 App 安装分别配置。
3. 将 [auth.example.json](configs/auth.example.json)复制到仓库及发布目录之外的部署目录，填写真实值。`origin` 必须是固定 HTTPS origin，`callbackUrl` 必须严格等于 `<origin>/api/auth/github/callback`。不要把填好的 `auth.json`、client secret、PEM、包装根、数据库连接串文件或验证技能的 `config.yaml` 放进仓库；根 `.gitignore` 会忽略误拷进工作树的这些文件，以及 `.env`、HAR 和 Cookie 导出。
4. client secret、App 私钥和包装根分别使用绝对路径的普通文件，由运行 Web 和 coordinator 的 Linux 用户拥有，权限严格为 `0600`，不得是符号链接。包装根是密码学随机的原始 32 字节。已有数据库使用原配套根文件，不能重新生成同名根替换。
5. 配置 HTTPS。使用受控反向代理时，`tlsCertificateFile` 和 `tlsKeyFile` 留空，代理转发到 Web 的回环地址；直接由 Web 提供 TLS 时同时填写证书和私钥路径，并调整监听地址。浏览器必须通过配置的 HTTPS origin 访问。

代理必须保留浏览器 Origin，并避免在访问日志中记录 callback 查询串。详细秘密要求、根备份及轮换步骤见[认证开发说明](docs/current/authentication-development.md)。配置示例中的占位值不能直接用于启动。

### 首次迁移并启动 Web

先把下列占位值替换为自己的连接串和配置文件路径，再执行。程序不会自动加载 `.env`；[环境变量示例](configs/repomesh.env.example)只是说明文件。

```bash
export REPOMESH_DATABASE_URL='<自己的 PostgreSQL 开发库连接串>'
export REPOMESH_AUTH_CONFIG='/绝对路径/到/auth.json'

go run ./cmd/repomesh-web db check
go run ./cmd/repomesh-web db migrate --timeout 30s
go run ./cmd/repomesh-web db check

npm --prefix web ci
npm --prefix web run build
# 下例用于 HTTPS 反向代理转发到本机 8080
go run ./cmd/repomesh-web --addr 127.0.0.1:8080 --assets ./web/dist
```

空库第一次 `db check` 报缺少迁移并退出 1 是预期结果；其他错误按[数据库开发说明](docs/current/database-development.md)处理。当前源码含 42 条迁移，迁移后应输出 `schema status=current current=42 target=42 pending=0`。以后新增迁移时，以所用源码或配套二进制的 `target` 为准。Web 和 coordinator 只核查迁移，不自动迁移；配置、秘密或迁移不匹配会使启动失败。

数据库子命令不需要前端资源，也不启动 HTTP。迁移仅支持前进；待应用 SQL 与历史记录在同一事务提交。优先通过环境变量提供连接串，避免将凭据放进 `--database-url` 命令参数。

### 启动 coordinator 并访问页面

另开一个 Bash 终端，进入同一仓库根目录，设置与 Web 相同的数据库和认证配置。新终端不会继承上一个终端的 `export`。

```bash
export REPOMESH_DATABASE_URL='<与 Web 相同的 PostgreSQL 连接串>'
export REPOMESH_AUTH_CONFIG='/与 Web 相同的绝对路径/auth.json'
go run ./cmd/repomesh-coordinator
```

通过配置的 HTTPS origin 打开页面。coordinator 负责账号核实、令牌刷新、仓库发现续扫和秘密维护；未配置认证时退出 1。两个进程使用相同的部署配置与根清单。停止时分别在两个终端按 Ctrl+C。

`repomesh-host-executor` 已实现受限执行循环（B10）：带 `--database` 和 `--worker` 注册后运行，负责心跳、轮询自己名下的停止请求、撤销资源写能力并释放。未注册主机不执行任何动作；容器／卷／网络的主机拆除在当前构建中显式拒绝（仅目录清理可用），正式状态推进由 coordinator 负责。真实 GitHub 验收仍处于暂停状态，已有环境记录不证明服务现在运行或整批验收完成；恢复入口见[当前交接](docs/current/HANDOFF.md#b02-外部暂停与恢复责任)。

执行来源由部署身份通过 `repomesh-web sources import --file PATH` 导入，通过 `sources result --import-id UUID` 查询原结果，不走浏览器。采用范围见[B04 说明](docs/current/b04-model-sources-adoption.md)。

## 日常开发与检查

已有依赖和构建产物时，可直接用上述命令启动 Web 和 coordinator。前端源码变更后重新执行 `npm --prefix web run build`；锁文件变更后先执行 `npm --prefix web ci`。Go 源码变更后重启对应的 `go run` 进程。升级源码若包含新迁移，先核查并显式迁移，再启动配套进程。

只开发前端页面时使用 Vite：

```bash
npm --prefix web run dev
```

打开 [Vite 开发页面](http://127.0.0.1:5173)。Vite 未配置 API 代理；认证集成使用 Go 同源服务提供的构建资源。

按改动影响选择检查；完整工程检查为：

```bash
npm --prefix web ci
npm --prefix web run typecheck
npm --prefix web test
npm --prefix web run build
go build ./...
go test ./...
go vet ./...
```

普通 `go test ./...` 未设置 `REPOMESH_TEST_DATABASE_URL` 时会跳过 PostgreSQL 集成用例，不能据此认定数据库验收通过。需要独立数据库验证时，可按范围运行：

```bash
pwsh -NoProfile -File scripts/verify-batch.ps1 -Batch B02 -PostgresBin /usr/lib/postgresql/17/bin
pwsh -NoProfile -File scripts/verify-batch.ps1 -Batch B03 -PostgresBin /usr/lib/postgresql/17/bin
```

这些脚本会创建临时 PostgreSQL 实例、执行检查并清理自己的实例，使用普通 Linux 用户运行。B02 包含认证 race 和前端 API 检查；B03 是后端批次检查，不安装或构建前端，应先准备 `web/dist`，前端完整测试及浏览器验收另行执行。脚本目前支持 B00、B01、B02、B03，没有 B04 模式。每次生成新的证据目录并拒绝覆盖；本地通过不等于真实 GitHub、模型请求或整批业务验收通过。

## 配置与配套构建

| 环境变量 | 命令行覆盖 | 默认值或用途 |
| --- | --- | --- |
| `REPOMESH_WEB_ADDR` | Web `--addr` | `127.0.0.1:8080`。 |
| `REPOMESH_WEB_ASSETS` | Web `--assets` | `web/dist`，相对于启动工作目录。 |
| `REPOMESH_AUTH_CONFIG` | Web／coordinator `--auth-config` | 默认不配置认证；值为部署 JSON 路径。 |
| `REPOMESH_DATABASE_URL` | `db check`／`db migrate` 的 `--database-url` | 数据库子命令、配置认证后的进程及 `sources` 子命令使用的连接串。 |

命令行参数优先于对应环境变量。PowerShell 中环境变量写法为 `$env:REPOMESH_DATABASE_URL = '<连接串>'`，Bash 中使用 `export`。两个 shell 都不会使程序自动读取 `.env`。

下面在 Bash 中生成带时间戳的新版本标签，构建成功后从该配套目录启动 Linux Web：

```bash
repomesh_version="local-$(date -u +%Y%m%dT%H%M%S)-$$"
pwsh -NoProfile -File scripts/build.ps1 -Version "$repomesh_version" &&
  (cd "dist/repomesh-$repomesh_version" && ./bin/repomesh-web --assets ./web/dist)
```

构建脚本按锁文件安装前端、检查类型、构建资源并输出三个同版本 Go 二进制。产物在 `dist/repomesh-<Version>/`，包含配置示例、启动说明和带 SHA-256 的 `release.json`。同名目录已存在时脚本拒绝覆盖，应使用新的标签；失败留下的目录不视为完整包。脚本使用当前 `GOOS`／`GOARCH`，不部署或启动服务，也不执行完整测试。

启用认证的配套启动需要在启动终端设置前述环境变量，并用该包的 `bin/repomesh-web db check`／`db migrate` 核对数据库。另开终端进入同一配套目录、设置相同环境变量后运行 `./bin/repomesh-coordinator`。真实配置和秘密保留在包外，使用绝对路径。Windows 构建的二进制带 `.exe`，认证仍须使用 Linux 构建运行。

从配套目录检查版本：

```bash
./bin/repomesh-web --version
./bin/repomesh-coordinator --version
./bin/repomesh-host-executor --version
```

发布时交付完整目录，保持三个入口与前端资源同版本。旧 B02／B03 包及对应验收只代表当时的源码，当前实现与证据从[交接](docs/current/HANDOFF.md)追溯。

## 常见启动问题

| 现象 | 检查或处理 |
| --- | --- |
| 找不到前端 `index.html` | 从仓库根目录重新构建前端，核对 `--assets` 相对于工作目录的路径。 |
| 8080 端口已占用 | 使用空闲端口，例如 `--addr 127.0.0.1:8081`；启用 HTTPS 代理时同步修改上游端口。 |
| 页面能打开，但认证或业务 API 返回 503 | 检查启动终端是否设置 `REPOMESH_AUTH_CONFIG`。未配置时查看页面是预期行为。 |
| 认证启动报告 schema pending | 使用相同源码或配套二进制显式迁移目标开发库，再核查。 |
| 认证配置或秘密不可用 | 检查 Linux 平台、绝对路径、运行用户、`0600` 权限和原配套根文件，详见认证说明。 |
| 登录后没有会话 | 通过配置的 HTTPS origin 访问；真实登录 Cookie 带 Secure，不能用 HTTP 地址替代。 |
| 仓库发现或认证恢复停滞 | 检查 coordinator 是否运行，以及它是否使用与 Web 相同的配置和数据库。 |
| `/readyz` 返回 503 | 当前实现的固定行为，不作为本地进程启动失败的判断。 |
| Git `dubious ownership` 或 Go `error obtaining VCS status` | 按[开发说明](docs/current/development-scaffold.md#本地启动与配置)设置当前会话的仓库信任。 |

AgentTeams 保留在被父仓库忽略的独立克隆中；源码位置、固定版本及重建方式见[上游说明](third_party/README.md)。

## Codex 项目插件

本仓库通过项目级配置启用 pstack 0.9.28，包含 54 个开发辅助技能。按任务需要选择相关技能；安装范围、固定来源及 Windows 限制见[插件说明](.agents/plugins/README.md)。这是开发工具配置，不改变产品 Skill 模块暂缓的决定。

## 实施边界

PostgreSQL、认证、受限 GitHub 发现适配、项目管理及 B04 模型来源与秘密保存已实现。项目可保存为待配置状态，默认配置为空时不伪造有效值。模型测试、专用应用、预算编辑、Issue、业务计划、调度、AgentTeams、运行待办和对象存储仍未接入。Graph 后续仍是后台进程内模块；ADR-0020 的 Python 仓库分析仅记录受控扩展边界；Skill 工程继续暂缓。后续业务目录、接口、数据库表及恢复算法按各批采用契约增加。

`validation/` 保存独立的历史实验和证据，不是产品代码，也不随产品发布。骨架构建通过不代表旧验证全部通过，更不等于业务或 AgentTeams 集成验收完成。
