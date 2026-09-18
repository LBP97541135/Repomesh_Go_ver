# 配置与验证 B02 认证

B02 提供 GitHub App 登录、同账号重连、服务端会话、授权结果恢复和仓库发现。采用边界见[采用记录](b02-authentication-adoption.md)，本地验证及真实验收缺口见[本批结果](../development/2026-09-12-batch-02/README.md)。项目管理和 B04 模型来源保存已接入，当前完成度见[交接](HANDOFF.md)；Issue 与运行接入仍未实现，`/readyz` 保持 503。

逐项 App 设置、HTTPS 接线、配套包启动命令及自然到期刷新验收见[真实 GitHub 验收手册](b02-github-live-acceptance.md)。[非 UTC 环境时间输出](../development/2026-09-12-b026-utc-fix-01/README.md)和[同步 callback Cookie 竞争](../development/2026-09-12-b026-cookie-fix-01/README.md)已完成本地修复与独立源码复核。原 r1、r2 和本地证据保留。配置、修复和本地验证完成不代表完整真实验收通过。

## 配置 Linux 开发部署

准备 RepoMesh 专用 GitHub App、固定 HTTPS 域名和 PostgreSQL 数据库。App 的 callback 必须严格等于 `<origin>/api/auth/github/callback`，例如 `https://repomesh.example.com/api/auth/github/callback`。登录授权与 App 安装分开；工作能力核查要求当前安装未暂停，且具有 Metadata 读取、Contents 写入、Pull requests 写入权限。当前只实现核查，不执行仓库写入。

复制[部署配置示例](../../configs/auth.example.json)到自己的部署目录，填写 App ID、Client ID、固定 origin、callback 及秘密文件路径。文件不接受未知字段或重复字段。这里没有模型、任意出站 URL 或命令配置入口。

OAuth client secret、App RSA 私钥和包装根分别保存在仓库及发布目录之外，填写绝对路径，使用两个进程 effective UID 所有的普通文件，权限严格为 `0600`。秘密文件本身不得为符号链接，父目录由部署管理员控制。App 秘密文件各不超过 64 KiB；RSA 私钥支持 PKCS#1／PKCS#8，至少 2048 位。包装根必须是密码学随机的原始 32 字节，不是十六进制字符串；生产根由部署管理员生成，程序不自动创建。配置、环境变量和命令行只引用文件路径，不放秘密正文。

使用与当前程序配套的迁移，再启动两个进程。程序不自动加载 `.env`，也不自动迁移。

```bash
export REPOMESH_DATABASE_URL='<自己的 PostgreSQL 连接串>'
export REPOMESH_AUTH_CONFIG='/etc/repomesh/auth.json'
go run ./cmd/repomesh-web db migrate
npm --prefix web ci
npm --prefix web run build
go run ./cmd/repomesh-web --assets ./web/dist
```

另开终端，设置相同的两个环境变量后运行后台协调进程。

```bash
go run ./cmd/repomesh-coordinator
```

也可用 `--auth-config /etc/repomesh/auth.json` 覆盖 `REPOMESH_AUTH_CONFIG`。未提供配置时，Web 可提供页面和诊断，认证 API 返回 503；coordinator 明确报告未配置并退出 1。配置存在但无效、秘密不可读、根自检失败或迁移不匹配时，进程启动失败，不退回伪认证。

HTTPS 有两种接线方式。直接服务时填写 `tlsCertificateFile` 和 `tlsKeyFile`。反向代理终止 TLS 时两项留空，Web 仅监听受控的回环／内部地址，浏览器通过配置的 HTTPS origin 访问。代理须保留浏览器 Origin，禁止访问日志记录 callback 查询串，禁止抓取认证请求正文。不要直接通过 HTTP 地址测试真实登录，`__Host-` Cookie 始终带 Secure、HttpOnly 和 SameSite=Lax。

前端独立 `npm --prefix web run dev` 未配置 API 代理。认证集成使用 Go 同源服务的构建资源，避免通过跨源放宽或不安全 Cookie 绕过部署约束。

## 持久状态与后台恢复

迁移 `0002_auth_secrets.sql` 建立秘密、自检与根注册，`0003_authentication.sql` 建立账号、浏览器绑定、会话、授权尝试、连接和发现批次。GitHub 稳定账号 ID 绑定本地账号；会话 Cookie、浏览器绑定和 state 在查找时只保存摘要。state／PKCE 及令牌正文独立加密，AAD 绑定权威 owner、purpose 和版本。

会话绝对期限 12 小时、空闲期限 30 分钟，浏览器绑定绝对期限 7 天，授权尝试 10 分钟，非秘密结果保留 24 小时后返回 410。最小尝试标识继续保留，旧键不能复活。创建尝试每浏览器每分钟最多 5 次、每账号最多 10 次；同输入重放不增加尝试数量。

交换 code 和刷新令牌在外发前领取唯一责任。响应未知时不会重新发送旧凭据。可恢复的账号查询、发现扫描和根重包由 coordinator 推进。网页登录回执与当前会话分别读取；确认回执不能补发丢失的 Cookie。登录与重连会轮换本浏览器身份，常规刷新只更新连接版本。跨浏览器旧连接结果同样不能覆盖新连接。

同步 callback 最长运行 15 秒，调用方更早的截止时间仍有效。保存交换结果时，身份核实工作保留给该 callback，coordinator 在截止前不能抢先确认。同步路径结束却仍待身份核实时可提前释放；请求中断或超时后按保留期限恢复。后台恢复始终不签发会话 Cookie。

发现按稳定仓库 ID 升序输出。每次后台工作最多读取两个上游页，预算 5 秒；批次最长 10 分钟。初始 503 后即使后台已完成，未交付的原批仍供重读；首次成功交付后，下一次无 cursor 首页可新建批。空页可以携带继续游标。扫描不是跨页固定快照，较小的新 ID 通过刷新读取。末页的 `coverage` 仍为 `partial`，单一 App 安装范围外的私仓可能缺席。

已确认用户读取资格的仓库才能披露名称。读权观察最多复用 60 秒，之后按上游仓库名称查询并核稳定 ID；改名、转移或未知时隐藏旧名称。App 工作能力单独核查，失败显示 unknown，不把用户读权当成 App 写权。项目和 Issue 真正写入时还须落实其所属批次的动作授权。

终态尝试的可用交换材料由 coordinator 永久销毁。未成功关联的秘密在明确 CAS 失败时清理；提交回执未知和进程崩溃留下的无引用版本，超过 15 分钟后按当前权威引用清理。此窗口大于当前请求、启动及尝试最长写入预算。销毁保留版本 tombstone，不允许重新启用。数据库不可用时不会虚报清理完成；后台恢复后继续执行。

## 轮换与恢复包装根

保存数据库和全部仍被引用的根文件备份。根 ID 与正文指纹不可换绑，同一正文也不能另起 ID 来重置预算。每个根最多包装 1,000,000 次，包括失败写入、自检和重包；计数先在独立持久事务扣除，不因后续失败退款。

轮换时，生成新的随机根文件，配置 `roots` 同时保留旧根与新根，将 `activeRootId` 指向新 ID。重启 Web 和 coordinator，确认新配置的自检通过。旧根转为仅解包，旧配置不能重新取得包装资格。coordinator 每次最多重包 20 个版本，SecretVersion ID 不变。确认所有未销毁版本（包括已禁用版本）及自检均不再引用旧根之后，才从配置移除旧文件；tombstone 不再要求旧根。历史备份仍需保留对应的解密根。

若原进程仍使用旧 active root，其新包装会被数据库拒绝，应按同一部署配置重启全部认证进程。并发轮换的引用预检与旧根写入使用共同的事务屏障，引用预检失败不能先退役有效根。根切换成功后，其他 App 文件或数据库操作失败不会回滚已经提交的切根；部署前先核完整配置，并使所有进程使用相同根清单。

恢复数据库旧快照可能回退包装计数，不能沿该快照中的旧 active root 继续包装。恢复时保留解密需要的旧根，同时生成从未使用的新根作为 active，再重包。丢失仍被引用的根会导致对应秘密无法恢复，不能通过重建同名文件修复。

当前认证启动仅支持 Linux，其他平台明确拒绝读取认证秘密文件。Windows 构建通过不代表 Windows ACL 或真实登录已经验收。Go 的清零操作不保证运行时已产生的所有明文副本都物理清除；禁用请求体日志和调试转储，限制进程与数据库访问权限。

## 验证与尚缺条件

```bash
pwsh -NoProfile -File scripts/verify-batch.ps1 -Batch B02 -PostgresBin /usr/lib/postgresql/17/bin
```

race 检查还要求启用 cgo 并有可用的 C 编译器，例如 GCC。脚本创建自己的 PostgreSQL，运行完整工程检查、真实数据库及 HTTP 用例、race 检查、二进制和迁移重启检查，再清理实例。通过仅输出 `LOCAL_VERIFIED`。没有 `REPOMESH_TEST_DATABASE_URL` 的普通 Go 测试会跳过数据库集成，不等价于本批验收。

浏览器脚本及可复跑方式见[本批结果](../development/2026-09-12-batch-02/README.md)。其中 API 替身用于故障交互，不代表真实 GitHub 成功。真实 App、HTTPS 和秘密配置已有历史记录，但完整外部验收仍暂停，恢复条件见[当前交接](HANDOFF.md#b02-外部暂停与恢复责任)。用户已解除 B03 等待 B02 整批 VERIFIED 的旧顺序条件；跨账号用例按交接保留为暂缓，不据本页恢复旧实验。
