# TLS 配置独立复核

复核范围仅为指定证书链、私钥元数据及 `auth.pending.json` 的 TLS 路径/字段存在性。未读取或输出私钥、包装根、runtime 配置或其他秘密正文；未启动 Web。公私钥匹配沿用配置者已提供的 OpenSSL 验证，未重复执行。

## 结果

- 叶证书 subject/SAN 为 `repomesh.bohanxu.me`；pending origin 仅以布尔比较确认其为预期的 `https` 主机和 `8443` 端口，SAN/Origin 匹配为真，callback 与该 origin 的关系也为真。
- 叶证书当前有效：`notBefore` 为 2026-09-12 12:37:36 UTC，`notAfter` 为 2026-12-11 12:37:35 UTC；`openssl x509 -checkend 0` 成功。
- `openssl verify -CAfile <issuer> <leaf>` 成功。叶证书 issuer 是 Let's Encrypt YE2。
- 叶证书、issuer 证书和 key 均为当前用户拥有的普通文件，模式均为 0600；ACME 根目录、state 与 certificates 目录均为当前用户拥有、模式 0700。key 仅执行 `stat` 元数据检查。
- pending JSON 是对象；`origin`、`appId`、`clientId`、`callbackUrl`、`clientSecretFile`、`privateKeyFile`、`activeRootId`、`roots`、`tlsCertificateFile`、`tlsKeyFile` 字段都存在。TLS 证书和私钥路径与指定路径完全匹配，且 TLS 配置成对存在。

## P0 / P1 / P2

无开放项。

这证明静态证书材料、链与 pending TLS 路径满足本次检查范围；没有启动 Web，因此这不是 HTTPS 服务可用性或端到端 TLS 通过的结论。
