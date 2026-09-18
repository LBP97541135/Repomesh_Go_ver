# 独立复核：GitHub App 本地秘密导入辅助脚本

复核对象：`import-github-app.py`、`verify.py` 与 `verification-02.json`。未读取真实配置或秘密，未运行真实导入，未改动产品文件。

## P0

无。

## P1

1. **未将 pending 配置中的秘密文件路径绑定到实际写入目标。**
   脚本固定创建 `~/.config/repomesh/github-client-secret` 和 `github-app.pem`，但仅校验 pending 的 `origin` 与 `callbackUrl`，不会校验 `clientSecretFile` 与 `privateKeyFile`。随后产品运行时会按 JSON 中这两个字段读取凭据。因此一个权限和格式都合格、但路径指向其他位置的 `auth.pending.json` 会让脚本报告成功，而服务无法使用刚导入的文件（或读取意外的已有文件）。在写入前要求两个 JSON 字段恰好等于这两个绝对目标路径；不匹配则拒绝，是必要的闭环。

## P2

1. **多文件写入不是失败可恢复的事务。**
   `github-client-secret` 在 PEM、备份和 pending 替换之前创建。如果随后 `github-app.pem` 的排他创建、备份写入或文件系统写入失败，脚本保留已创建的 client secret；下一次运行会因目标已存在而拒绝。该文件仍是 0600，且错误输出不含秘密，风险主要是操作中断后的人工恢复。应在失败处理里尽力删除本次创建的文件（仅限仍可确认由本次调用创建的路径），或在开始前清楚提供安全的恢复步骤；并增加一个“第二个目标在首个写入后被占用/写入失败”的测试。

## 已确认的正向证据

- 脚本要求 stdin/stdout TTY；client secret 使用隐藏输入，错误和成功输出均不打印秘密或 PEM。
- 配置目录与 pending 文件均检查为当前用户拥有的真实对象，且分别严格为 0700/0600；目标以 `O_CREAT|O_EXCL` 与 0600 创建。
- PEM 来源拒绝符号链接，读取时核对 inode，并以 OpenSSL 仅接受未加密 RSA 私钥且模数至少 2048 位。
- 已重新执行 `python3 -m py_compile ...` 和 `python3 verify.py`。临时 HOME＋PTY 验证重现：成功导入的两个文件均为 0600、原 pending 有精确备份、输出不含测试 secret；预存目标拒绝且未改 pending；EC 私钥拒绝且不创建目标。

## 结论

没有发现秘密回显、覆盖现有目标、0600 创建或 RSA 位数验证方面的 P0 问题。修复 P1 后，这个辅助脚本的核心导入契约才与后续产品读取路径一致；P2 是建议补足的失败恢复能力。
