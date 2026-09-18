# B02.6 专用配置执行

用户已要求代理按手册配置真实登录验收环境。本轮从此前未配置状态继续，旧轮次的“不启动服务”描述是历史事实，不限制本次明确授权。

- [x] 确认 Linux、工作目录及 Git 状态，保留全部既有修改。
- [x] 检查专用配置路径、PostgreSQL 工具和现有监听。
- [x] 检查用户提供的 bohanxu.me。主域 HTTPS 可访问，现为 Vercel 博客，RepoMesh callback 返回 404。
- [x] 创建私有配置目录、包装根及非秘密配置草稿。
- [x] 创建专用持久 PostgreSQL，完成 r1 配套迁移和核查。
- [x] 确定独立子域名与本机浏览器访问方式，Windows hosts 已生效。
- [x] 完成 DNS-01 证书签发并填写 TLS 配置路径。代理在授权后添加 TXT，权威 DNS 验证通过，原申请签发成功，证书链和密钥匹配检查通过。
- [ ] 实际启动后验证受信任 HTTPS 和 Windows 浏览器访问。
- [x] 用户报告已创建 GitHub App，设置入口为 repomesh-bohan-b026。公开接口尚未取得参数。
- [ ] 导入真实 App 参数与秘密文件，完成测试安装。账号持有人完成 GitHub 操作。
- [ ] 启动配套 Web 与 coordinator，按模板记录真实验收。
- [x] 独立复核已完成的本机配置，更新当前交接。证书与真实验收待后续分别复核。

## 吞吐检查点

- Blocking first steps. 本机数据库和私密目录不依赖域名。HTTPS 接线依赖用户选择本机浏览器或公网访问，App 注册依赖固定 callback。
- Independent workstreams. GPT-6 Astra 规划和核对域名。gpt-5.6 sol 实际配置专属目录和数据库。独立复核只读取脱敏产物及必要元数据。
- Shared mutable state. 服务器配置执行者仅写新 ~/.config/repomesh、~/.local/share/repomesh/b026-postgres、~/.local/state/repomesh-b026 及其临时证据。主代理集成仓内新记录及 HANDOFF，不改产品代码、旧证据、r1 包和 .codex/config.toml。
- Smallest safe decomposition. 先完成专用数据库及秘密文件元数据，再接通HTTPS和真实App。每步按实际结果记录，不将配置或迁移通过算作OAuth通过。
