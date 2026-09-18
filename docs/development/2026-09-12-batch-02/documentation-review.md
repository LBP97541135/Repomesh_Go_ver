独立交付说明复核，2026-09-12。

按要求完整阅读根目录 AGENTS.md、README.md、authentication-development.md、database-development.md、development-scaffold.md、build.ps1 和 verify-batch.ps1。另以 CLI 入口、部署配置解析、HTTP 路由、秘密根实现、后台工作、配置示例和现有发布清单核对命令及行为。没有修改主树、启动服务、调用 GitHub 或重跑验收。HANDOFF 和 B02 批次 README 的收尾段落不在本次判定范围。

发现四项明确偏差和一项验证前置遗漏。未发现 P0/P1。

1. [P2] 当前 Windows 能力与脚本支持范围写得过宽。

   [README.md:77](/home/xubohan/projects/Repomesh_Go_ver/README.md:77) 说明当前数据库批次脚本已适配 Windows 与 Linux，并提供当前源码的 Windows B01 命令。[database-development.md:38](/home/xubohan/projects/Repomesh_Go_ver/docs/current/database-development.md:38) 同样将 Windows 和 Linux 都列为当前脚本的可用运行环境。但脚本对 B01/B02 均运行 `go test -json -count=1 ./...`，同时配置真实测试数据库。[internal/access/postgres_test.go:70](/home/xubohan/projects/Repomesh_Go_ver/internal/access/postgres_test.go:70) 的无平台标签 fixture 会调用 `secrets.New`；[root_other.go:5](/home/xubohan/projects/Repomesh_Go_ver/internal/secrets/root_other.go:5) 在 Windows 上无条件拒绝。因此本轮源码上的完整 B01/B02 脚本无法因历史 Windows B01 通过而视为可用。

   [authentication-development.md:60](/home/xubohan/projects/Repomesh_Go_ver/docs/current/authentication-development.md:60) 的“其他平台的认证启动可能明确拒绝”也弱化了已确定的限制。[private_file_other.go:7](/home/xubohan/projects/Repomesh_Go_ver/internal/access/private_file_other.go:7) 对所有非 Linux 平台无条件返回 unsupported 错误。当前应明确写“认证启动仅支持 Linux，Windows 仅已核编译，不能启用认证”。将历史 Windows B01 命令及结果标明对应旧源码；当前完整认证数据库验收应指定 Linux。若要继续支持当前 Windows B01 验证，需要先明确脚本批次覆盖范围，不能靠跳过认证测试宣称 B02 已验收。

2. [P2] README 把历史骨架包写成当前验收产物，当前工作目录也没有该包。

   [README.md:96](/home/xubohan/projects/Repomesh_Go_ver/README.md:96) 使用 `0.1.0-scaffold-final` 作为构建版本，[README.md:99](/home/xubohan/projects/Repomesh_Go_ver/README.md:99) 声称 `dist/repomesh-0.1.0-scaffold-final/` 是当前验收包，并排除其他包。[README.md:104](/home/xubohan/projects/Repomesh_Go_ver/README.md:104) 随后让读者运行这个目录里的 Windows 二进制。

   本次只读枚举 `rg --files dist -g release.json -g README.txt` 只发现 `dist/repomesh-0.1.0-b01-wsl-20260912/`。这是旧 B01 包，不能证明本轮认证产物已打包。新 `build.ps1` 确实会打入认证配置，并将清单 stage 写为 `authentication-local`，但源码存在不能替代一次实际构建。

   建议改用新的 B02 构建标签，并在本轮构建实际通过后填写它的路径、目标平台和清单位置。若本轮没有打包，则明确写“当前尚未产出 B02 发布包”，将已有目录称为历史 B01 产物。WSL 默认生成 Linux 文件，不应在同一默认构建流程后直接给出 `.exe` 启动命令，Windows 命令须单独标注目标平台及认证不支持限制。

3. [P2] “失败配置不能先退役有效根”超出了目前实现保证。

   [authentication-development.md:56](/home/xubohan/projects/Repomesh_Go_ver/docs/current/authentication-development.md:56) 将并发引用检查修复概括为“失败配置不能先退役有效根”。当前根包确实已经在引用检查前锁住 active root，这与先前 P2 修复相符。但 [deployment.go:81](/home/xubohan/projects/Repomesh_Go_ver/internal/access/deployment.go:81) 会先调用 `secrets.New` 完成根激活及自检，然后才在第 86、90 行读取 App 秘密文件，第 95 行创建 GitHub 适配器。

   具体反例是保留有效 A 根、配置有效新 B 根并把 activeRootId 改为 B，同时误填不存在的 privateKeyFile。根初始化会先激活 B 并停用 A 的包装资格，然后 App 私钥读取使整个认证启动失败。旧 A 进程的新包装被拒绝。缺失 App 文件的配置因此仍可能改变全局包装根。

   建议把承诺收窄为“并发引用检查完成前持有旧根写入屏障；缺失当前引用根等根预检错误不会切换包装根”。部署说明还应提醒先验证本次全部秘密路径和 App 参数，再实施根轮换，或由实现补齐完整配置预检。不要将本地事务屏障表述成整个进程启动失败时自动回滚根切换。该反例是源码路径推导，本次未执行。

4. [P2] 未配置认证的返回码不是统一 503。

   [AGENTS.md:101](/home/xubohan/projects/Repomesh_Go_ver/AGENTS.md:101)、[README.md:88](/home/xubohan/projects/Repomesh_Go_ver/README.md:88)、[authentication-development.md:30](/home/xubohan/projects/Repomesh_Go_ver/docs/current/authentication-development.md:30) 和 development-scaffold 的场景表均把未配置认证 API 统称为 503。

   [auth.go:32](/home/xubohan/projects/Repomesh_Go_ver/internal/web/auth.go:32) 先对 POST 校验 Origin，再在第 36 行检查 Service。未配置时 Origin 为空，因此登录、重连、注销 POST 都先返回 `403 ORIGIN_REJECTED`。GET session、attempt、repositories 等才返回 `503 AUTH_NOT_CONFIGURED`。未知 API 仍返回 404。

   建议按实际响应写成“未配置时认证读取接口返回 503；写接口仍先执行 Origin 检查，可能返回 403”。若产品希望未配置的 POST 也统一返回 503，则需要调整和验证路由顺序，不能只改说明。可用未配置的 Web 对 `GET /api/session` 和携 Origin 的 `POST /api/auth/github/login` 各发一次请求确认，本次未启动服务。

5. [P3] B02 race 检查增加了未列出的 Linux 工具前置。

   [verify-batch.ps1:200](/home/xubohan/projects/Repomesh_Go_ver/scripts/verify-batch.ps1:200) 调用 `go test -race`。Linux 上 race 构建要求启用 cgo 并提供支持的 C 编译器。README 的工具要求只有 Go、Node、npm、PowerShell 和 PostgreSQL；仅安装这些工具的最小 Linux 环境会在该检查失败。

   建议在 B02 验证前置中列出 `CGO_ENABLED=1` 和可用的 gcc 或 clang，并说明构建环境必须支持当前 Go race 目标。可在脚本开始阶段读取 `go env CGO_ENABLED CC` 给出明确诊断，避免完成前面的检查后才报告 race 无法构建。这是验证前置遗漏，不是当前已配置 WSL 环境的测试失败。

其余核对结论如下。

- 配置 JSON 字段与 `Deployment` 和 `auth.example.json` 一致，未知键、重复键、精确 HTTPS origin/callback 的说明与边界解析一致。两个 TLS 字段必须成对，Web 支持直接 TLS；代理终止 TLS 时 HTTP 后端需由部署限制访问，说明没有声称程序自动证明代理隔离。
- `--auth-config` 覆盖环境变量，db 子命令不要求认证配置或前端资源，当前三条迁移及退出码与 CLI 实现一致。程序不自动迁移、不自动读取 `.env` 的说明正确。
- App 安装与用户读权分开，当前需要 Metadata read、Contents write、Pull requests write，发现覆盖始终 partial，实际仓库写入及后续业务未实现的描述一致。
- 包装预算的独立持久预扣、新根重包、旧根解包、恢复旧数据库时必须换从未使用的新包装根的说明，与包内实现和运维责任边界一致。删除旧根前建议使用“所有未销毁版本及自检”这个精确条件，避免将“活跃密文”理解为仅 enabled 版本。
- build.ps1 的前端 build 会间接执行 typecheck，三入口版本注入、目标平台后缀、配置及锁文件复制、最终 SHA-256 清单和已有目录拒绝覆盖均与说明一致。构建脚本不运行测试、不部署、不迁移。B02 验证脚本输出 LOCAL_VERIFIED，并保留真实 GitHub 未执行的标记，未混写为完整 VERIFIED。
- 本次检查的五份 Markdown 引用文件均可定位。路径提取对 PowerShell 类型转换产生的一条伪链接已排除，没有将它当作断链报告。

以上定位按审查时文件内容记录。主代理若继续编辑，需以最新内容复核受影响段落；本报告没有把仍在收尾的 HANDOFF 或批次结果旧段落当作缺陷。
