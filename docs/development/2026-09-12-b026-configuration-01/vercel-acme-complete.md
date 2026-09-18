# B02.6 Vercel DNS 与 ACME 签发结果

- Vercel CLI：`59.16.0`，Node.js `22.22.1`。CLI 安装与认证状态保存在 `/home/xubohan/.local/share/repomesh-b026-vercel`，目录权限 `0700`。没有读取或输出认证令牌。
- Vercel 登录：设备授权成功。实际账号为 `catbobyman`，目标域名 `bohanxu.me` 位于唯一团队 `catbobymans-projects`。Vercel CLI 显示该域名的 registrar 和 nameservers 均为 Vercel。
- 写入前检查：`bohanxu.me` 没有 `_acme-challenge.repomesh` TXT。现有 CAA 和 ALIAS 记录未修改。
- 新增记录：TXT `_acme-challenge.repomesh.bohanxu.me`，值 `7XpLmTX7KIDTzmnDU9yKVuD9BQnMOsiIR4RSOBIp5TU`。Vercel record ID 为 `rec_c27f725789c0d751f05fe52c`。
- 写入验证：`ns1.vercel-dns.com` 与 `ns2.vercel-dns.com` 都返回上述精确 TXT 值，随后才继续原 lego PTY session `15538`。没有发起第二个 ACME order。
- ACME 结果：Let's Encrypt 在 2026-09-12 06:36:06 PDT 验证 DNS-01，并在 06:36:08 PDT 返回证书。
- 证书名称：subject CN 为 `repomesh.bohanxu.me`，SAN 仅含 `DNS:repomesh.bohanxu.me`。
- 签发者：`C = US, O = Let's Encrypt, CN = YE2`。
- 序列号：`06BAFE6FBA013416B219D1A63196293B3D17`。
- 有效期：`2026-09-12 12:37:36 UTC` 至 `2026-12-11 12:37:35 UTC`。
- OpenSSL 验证：使用保存的 issuer 文件验证 leaf 证书结果为 `OK`。证书公钥与私钥导出公钥的 SHA-256 相等，结果为 `certificate_private_key_match=true`。报告不包含该摘要值或私钥正文。
- 证书文件：`/home/xubohan/.local/share/repomesh-b026-acme/state/certificates/repomesh.bohanxu.me.crt`，权限 `0600`。
- issuer 文件：`/home/xubohan/.local/share/repomesh-b026-acme/state/certificates/repomesh.bohanxu.me.issuer.crt`，权限 `0600`。
- 私钥文件：`/home/xubohan/.local/share/repomesh-b026-acme/state/certificates/repomesh.bohanxu.me.key`，权限 `0600`。私钥正文未读取或输出。
- 证书目录：`/home/xubohan/.local/share/repomesh-b026-acme/state/certificates`，权限 `0700`。
- 待完成配置：`/home/xubohan/.config/repomesh/auth.pending.json` 在确认目录 `0700`、文件 `0600` 且不是符号链接后，原子更新了 `tlsCertificateFile` 和 `tlsKeyFile` 两个字段。其他 JSON 字段由解析后原样保留。文件正文未输出。
- 配置备份：`/home/xubohan/.config/repomesh/auth.pending.json.b026-tls-20260912T063659-0700.bak`，权限 `0600`。本任务没有生成最终 `auth.json`，也没有启动 Web 或 coordinator。
- TXT 清理：证书签发成功后，仅按本次新增的 record ID `rec_c27f725789c0d751f05fe52c` 删除挑战记录。Vercel CLI 报告删除成功。随后 `ns1.vercel-dns.com` 与 `ns2.vercel-dns.com` 均未再返回本次挑战值，Vercel DNS 列表也不再包含 `_acme-challenge.repomesh`。

记录用语说明：匹配检查由 OpenSSL 读取私钥并导出公钥。上文“私钥正文未读取或输出”指没有作为文本检查或进入工具输出；并非密码学操作从未读取私钥。
