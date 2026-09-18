# B02.6 ACME DNS-01 状态

- 记录时间：2026-09-12 06:16 PDT 后。
- 工具：lego v5.4.1 linux/amd64。
- 官方资产：https://github.com/go-acme/lego/releases/download/v5.4.1/lego_v5.4.1_linux_amd64.tar.gz
- 官方校验清单：https://github.com/go-acme/lego/releases/download/v5.4.1/lego_5.4.1_checksums.txt
- 资产 SHA-256：`ebb33f1bead5a7c99dd46f1c5734b44cf1eab5b5c12faf397cd14d50a5916419`。实际值与官方清单一致。
- ACME 环境：Let's Encrypt 生产环境。
- 证书名称：仅 `repomesh.bohanxu.me`。
- 挑战方式：lego `manual` DNS-01。
- 当前状态：生产证书请求已发起，正在等待 DNS TXT 记录。TLS 证书尚未签发。
- PTY session：`15538`。
- TXT 完整名称：`_acme-challenge.repomesh.bohanxu.me`。
- TXT 值：`7XpLmTX7KIDTzmnDU9yKVuD9BQnMOsiIR4RSOBIp5TU`。
- 私有工具及状态根目录：`/home/xubohan/.local/share/repomesh-b026-acme`，权限 `0700`。
- ACME 账户和证书状态目录：`/home/xubohan/.local/share/repomesh-b026-acme/state`，权限 `0700`。其中私钥和账户文件不得复制到证据目录或输出。
- 可读交互日志：`/home/xubohan/.local/share/repomesh-b026-acme/logs/production-dns01-session.log`，权限 `0600`。
- 恢复状态：`/home/xubohan/.local/share/repomesh-b026-acme/STATUS.txt`，权限 `0600`。
- 恢复步骤：等待用户确认已在 Vercel 添加 TXT。随后通过公共权威 DNS 查询确认完整名称返回精确值。确认后只向 PTY session `15538` 发送一个换行，并等待签发结果。不要启动第二次申请。
