# 本轮启动前后检查

- 2026-09-12 06:50 PDT：实际工具环境为 Linux，工作目录为 `/home/xubohan/projects/Repomesh_Go_ver`。既有未提交修改保留。
- 本机导入后，client secret 与 GitHub App PEM 都是当前用户拥有的普通 0600 文件；只核对元数据，未显示正文。
- 69 项已记录源码与 11 个 r1 发布文件均与各自清单匹配。第一次检查因 jq 未安装而未执行，随后 Python 标准库核对成功。
- 配置者报告 r1 Web PID 177022、coordinator PID 177023，数据库核查为 `current=3 target=3 pending=0`；完整启动结果见后续 startup.md。
- Windows `curl.exe` 直接访问 `https://repomesh.bohanxu.me:8443/login`，未禁用证书校验，结果 `http=200 tls_verify=0`。这证明 Windows 的域名解析、连接与证书校验可用，实际浏览器渲染和 OAuth 另记。
- PostgreSQL 只读事务于 `2026-09-12 06:51:48.744987-07` 观察 attempts=0、sessions=0、connections=0。尚未开始真实登录或刷新计时。

查询仅访问三个表的计数，未读取认证材料、Cookie、数据库连接串或任何令牌。
